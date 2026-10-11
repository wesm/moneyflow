import { describe, expect, it } from 'vitest'

import { cursorMove, cursorTarget } from './cursor-keys'

function key(name: string, init: KeyboardEventInit = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: name, ...init })
}

describe('table cursor keys', () => {
  it('reads the TUI cursor keys', () => {
    const moves = [
      ['ArrowUp', 'up'],
      ['k', 'up'],
      ['ArrowDown', 'down'],
      ['j', 'down'],
      ['Home', 'home'],
      ['T', 'home'],
      ['End', 'end'],
      ['B', 'end'],
      ['PageUp', 'page-up'],
      ['PageDown', 'page-down'],
    ] as const
    for (const [name, move] of moves) {
      expect(
        cursorMove(key(name, { shiftKey: name.length === 1 && name !== name.toLowerCase() })),
      ).toBe(move)
    }
  })

  it('leaves t, b, other keys, and modified keys to other handlers', () => {
    for (const name of ['t', 'b', 'g', 'Enter', 'ArrowLeft'])
      expect(cursorMove(key(name))).toBe(undefined)
    expect(cursorMove(key('Home', { ctrlKey: true }))).toBe(undefined)
    expect(cursorMove(key('j', { metaKey: true }))).toBe(undefined)
    expect(cursorMove(key('PageDown', { altKey: true }))).toBe(undefined)
  })

  it('moves by one row, one page, or to either end, inside the result', () => {
    expect(cursorTarget('down', 4, 100, 20)).toBe(5)
    expect(cursorTarget('up', 4, 100, 20)).toBe(3)
    expect(cursorTarget('up', 0, 100, 20)).toBe(0)
    expect(cursorTarget('down', 99, 100, 20)).toBe(99)
    expect(cursorTarget('page-down', 4, 100, 20)).toBe(24)
    expect(cursorTarget('page-down', 90, 100, 20)).toBe(99)
    expect(cursorTarget('page-up', 30, 100, 20)).toBe(10)
    expect(cursorTarget('page-up', 3, 100, 20)).toBe(0)
    expect(cursorTarget('home', 57, 100, 20)).toBe(0)
    expect(cursorTarget('end', 57, 100, 20)).toBe(99)
  })

  it('pages by at least one row and stays at row 0 in an empty result', () => {
    expect(cursorTarget('page-down', 4, 100, 0)).toBe(5)
    expect(cursorTarget('end', 0, 0, 20)).toBe(0)
    expect(cursorTarget('page-down', 0, 0, 20)).toBe(0)
  })
})
