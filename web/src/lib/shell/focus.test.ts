import { afterEach, describe, expect, it } from 'vitest'

import { restoreFocus, rowIndex, trapEveryTab } from './focus'

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

describe('trapEveryTab', () => {
  afterEach(() => document.body.replaceChildren())

  /** A surface whose controls are laid out; jsdom draws nothing, so each reports a box. */
  function surface(html: string): HTMLElement {
    const element = document.createElement('div')
    element.tabIndex = -1
    element.innerHTML = html
    document.body.append(element)
    for (const control of element.querySelectorAll<HTMLElement>('*')) {
      if (!control.hidden) control.getClientRects = () => [new DOMRect(0, 0, 10, 10)] as never
    }
    return element
  }

  function tab(shift = false): KeyboardEvent {
    const event = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: shift,
      bubbles: true,
      cancelable: true,
    })
    document.activeElement?.dispatchEvent(event)
    return event
  }

  const focused = () => document.activeElement?.textContent

  it('moves every Tab itself, so a browser that skips buttons cannot leave the surface', () => {
    const root = surface(
      '<button>One</button><input aria-label="Two" /><button disabled>Off</button>' +
        '<button tabindex="-1">Roving</button><button hidden>Hidden</button><a href="#">Three</a>',
    )
    const release = trapEveryTab(root)
    expect(document.activeElement).toBe(root)
    expect(tab().defaultPrevented).toBe(true)
    expect(focused()).toBe('One')
    tab()
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Two')
    tab()
    expect(focused()).toBe('Three')
    tab()
    expect(focused()).toBe('One')
    tab(true)
    expect(focused()).toBe('Three')
    release()
  })

  it('skips controls a browser would not reach: inert, in a disabled fieldset, or invisible', () => {
    const root = surface(
      '<button>One</button><div inert><button>Inert</button></div>' +
        '<fieldset disabled><input aria-label="Fieldset" /></fieldset>' +
        '<button style="visibility: hidden">Invisible</button><button>Two</button>',
    )
    const release = trapEveryTab(root)
    tab()
    expect(focused()).toBe('One')
    tab()
    expect(focused()).toBe('Two')
    tab()
    expect(focused()).toBe('One')
    release()
  })

  it('moves Shift+Tab from a middle control to the one before it', () => {
    const root = surface('<button>One</button><button>Two</button><button>Three</button>')
    const release = trapEveryTab(root)
    root.querySelectorAll('button')[1]!.focus()
    expect(tab(true).defaultPrevented).toBe(true)
    expect(focused()).toBe('One')
    release()
  })

  it('keeps focus on a surface with no controls', () => {
    const root = surface('<p>Nothing to focus</p>')
    const release = trapEveryTab(root)
    expect(tab().defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(root)
    expect(tab(true).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(root)
    release()
  })

  it('goes to the last control on Shift+Tab from the surface itself', () => {
    const root = surface('<button>One</button><button>Two</button>')
    const release = trapEveryTab(root)
    tab(true)
    expect(focused()).toBe('Two')
    release()
  })

  it('leaves a Tab that an inner control claimed', () => {
    const root = surface('<button>One</button><button>Two</button>')
    const release = trapEveryTab(root)
    const first = root.querySelector('button')!
    first.focus()
    first.addEventListener('keydown', (event) => event.preventDefault())
    tab()
    expect(focused()).toBe('One')
    release()
  })
})
