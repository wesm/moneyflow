import { afterEach, describe, expect, it } from 'vitest'

import { restoreFocus, rowIndex } from './focus'

/** A grid of rows, each with a ⋯ button in its actions cell. */
function grid(indexes: number[]): HTMLElement {
  const element = document.createElement('div')
  element.setAttribute('role', 'grid')
  for (const index of indexes) {
    const row = document.createElement('div')
    row.setAttribute('role', 'row')
    row.dataset['index'] = String(index)
    row.innerHTML = `<div data-column="actions"><button>Actions ${index}</button></div>`
    element.append(row)
  }
  document.body.append(element)
  return element
}

const button = (root: HTMLElement, index: number) =>
  root.querySelector<HTMLElement>(`[data-index="${index}"] button`)

describe('restoreFocus', () => {
  afterEach(() => document.body.replaceChildren())

  it('focuses the opener while it is on the page', () => {
    const root = grid([0, 1])
    const opener = button(root, 1)!
    expect(restoreFocus(opener, 1, root)).toBe(true)
    expect(document.activeElement).toBe(opener)
  })

  it('focuses the same control in the row now at the opener index', () => {
    const root = grid([0, 1, 2])
    const opener = button(root, 1)!
    const index = rowIndex(opener)
    opener.closest('[role="row"]')!.remove()
    const replacement = document.createElement('div')
    replacement.setAttribute('role', 'row')
    replacement.dataset['index'] = '1'
    replacement.innerHTML = '<div data-column="actions"><button>Actions new</button></div>'
    root.insertBefore(replacement, root.lastChild)
    expect(restoreFocus(opener, index, root)).toBe(true)
    expect(document.activeElement?.textContent).toBe('Actions new')
  })

  it('clamps to the last row when the opener row was the last', () => {
    const root = grid([0, 1, 2])
    const opener = button(root, 2)!
    opener.closest('[role="row"]')!.remove()
    expect(restoreFocus(opener, 2, root)).toBe(true)
    expect(document.activeElement).toBe(button(root, 1))
  })

  it('skips placeholder rows, which have no controls', () => {
    const root = grid([0])
    const placeholder = document.createElement('div')
    placeholder.dataset['index'] = '1'
    root.append(placeholder)
    const cell = document.createElement('div')
    cell.dataset['column'] = 'actions'
    const opener = document.createElement('button')
    cell.append(opener)
    expect(restoreFocus(opener, 4, root)).toBe(true)
    expect(document.activeElement).toBe(button(root, 0))
  })

  it('leaves the grid fallback to the caller when no row control can take focus', () => {
    const root = grid([])
    const outside = document.createElement('button')
    expect(restoreFocus(outside, 0, root)).toBe(false)
    expect(restoreFocus(undefined, undefined, root)).toBe(false)
    expect(restoreFocus(outside, undefined, undefined)).toBe(false)
  })

  it('does not focus a different control than the one that opened', () => {
    const root = grid([0])
    const cell = document.createElement('div')
    cell.dataset['column'] = 'merchant'
    const opener = document.createElement('button')
    cell.append(opener)
    expect(restoreFocus(opener, 0, root)).toBe(false)
  })
})

describe('rowIndex', () => {
  afterEach(() => document.body.replaceChildren())

  it('reads the index of the row holding an element', () => {
    const root = grid([3])
    expect(rowIndex(button(root, 3)!)).toBe(3)
    expect(rowIndex(root)).toBeUndefined()
    expect(rowIndex(undefined)).toBeUndefined()
  })
})
