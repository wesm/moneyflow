import { describe, expect, it, vi } from 'vitest'

import { createMoneyflowShortcuts, handleMoneyflowKeydown, validateCapabilities } from './shortcuts'

const capabilities = [
  ['cursor.up', '↑/k'],
  ['cursor.down', '↓/j'],
  ['cursor.home', 'home'],
  ['view.cycle-grouping', 'g'],
  ['view.show-detail', 'd'],
  ['view.switch-accounts', 'A'],
  ['view.drill', 'enter'],
  ['view.back', 'esc'],
  ['time.toggle-granularity', 't'],
  ['time.clear-period', 'a'],
  ['time.previous-period', '←'],
  ['time.next-period', '→'],
  ['sort.cycle', 's'],
  ['sort.reverse', 'v'],
  ['selection.toggle', 'space'],
  ['selection.toggle-all', 'ctrl+a'],
  ['overlay.filters', 'f'],
  ['overlay.search', '/'],
  ['overlay.help', '?'],
].map(([id, key_display]) => ({
  id: id!,
  key_display: key_display!,
  description: id!,
  category: '',
  available: true,
}))

describe('Moneyflow browser shortcuts', () => {
  it('matches the TUI read-only keys without lifecycle or End bindings', () => {
    expect(() => validateCapabilities(capabilities)).not.toThrow()
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)

    for (const key of [
      'ArrowUp',
      'k',
      'ArrowDown',
      'j',
      'Home',
      'g',
      'd',
      'Enter',
      'Escape',
      't',
      'a',
      'ArrowLeft',
      'ArrowRight',
      's',
      'v',
      ' ',
      'f',
      '/',
      '?',
    ]) {
      expect(shortcuts.manager.handleKeydown(keyboard(key))).toBe(true)
    }
    expect(shortcuts.manager.handleKeydown(keyboard('A', { shiftKey: true }))).toBe(true)
    expect(shortcuts.manager.handleKeydown(keyboard('a', { ctrlKey: true }))).toBe(true)
    for (const key of ['End', 'q'])
      expect(shortcuts.manager.handleKeydown(keyboard(key))).toBe(false)
    expect(shortcuts.manager.handleKeydown(keyboard('c', { ctrlKey: true }))).toBe(false)
    shortcuts.destroy()
  })

  it('rejects conflicting server action metadata', () => {
    expect(() =>
      validateCapabilities([
        { id: 'cursor.up', key_display: 'j', description: 'wrong', category: '', available: true },
      ]),
    ).toThrow('conflicts')
  })

  it('suspends table letter keys while an overlay scope is active', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    const pop = shortcuts.manager.pushScope('search')
    expect(shortcuts.manager.handleKeydown(keyboard('j'))).toBe(false)
    pop()
    expect(shortcuts.manager.handleKeydown(keyboard('j'))).toBe(true)
  })

  it('does not intercept native editing controls', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    const event = keyboard('a', { ctrlKey: true })
    Object.defineProperty(event, 'target', { value: document.createElement('input') })
    expect(handleMoneyflowKeydown(shortcuts.manager, event)).toBe(false)
    expect(handlers.apply).not.toHaveBeenCalled()
  })
})

function keyboard(key: string, init: KeyboardEventInit = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key, bubbles: true, ...init })
}
