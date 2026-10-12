import { describe, expect, it, vi } from 'vitest'

import { status } from './shell/status.svelte'
import { createMoneyflowShortcuts, handleMoneyflowKeydown, validateCapabilities } from './shortcuts'

const capabilities = [
  ['cursor.up', '↑/k'],
  ['cursor.down', '↓/j'],
  ['cursor.home', 'home'],
  ['view.cycle-grouping', 'g'],
  ['view.set-grouping', '1-5'],
  ['view.show-detail', 'd'],
  ['view.find-duplicates', 'D'],
  ['view.switch-accounts', 'A'],
  ['view.drill', 'enter'],
  ['view.back', 'esc'],
  ['time.clear-period', 'a'],
  ['time.previous-period', '←'],
  ['time.next-period', '→'],
  ['time.choose', 't'],
  ['sort.cycle', 's'],
  ['sort.reverse', 'v'],
  ['selection.toggle', 'space'],
  ['selection.toggle-all', 'ctrl+a'],
  ['transaction.show-info', 'i'],
  ['overlay.filters', 'f'],
  ['overlay.search', '/'],
  ['overlay.help', '?'],
  ['transaction.edit-merchant', 'm'],
  ['transaction.edit-category', 'c'],
  ['category.manage', 'C'],
  ['category-group.manage', 'G'],
  ['transaction.toggle-hidden', 'h'],
  ['transaction.delete', 'x'],
  ['changes.undo', 'u'],
  ['changes.redo', 'U'],
  ['transactions.export', 'E'],
  ['changes.review', 'w'],
  ['provider.refresh', 'r'],
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
      'a',
      'ArrowLeft',
      'ArrowRight',
      't',
      's',
      'v',
      ' ',
      'f',
      '/',
      '?',
      'm',
      'c',
      'h',
      'x',
      'u',
      'w',
      'r',
    ]) {
      expect(shortcuts.manager.handleKeydown(keyboard(key))).toBe(true)
    }
    expect(shortcuts.manager.handleKeydown(keyboard('A', { shiftKey: true }))).toBe(true)
    expect(shortcuts.manager.handleKeydown(keyboard('D', { shiftKey: true }))).toBe(true)
    expect(shortcuts.manager.handleKeydown(keyboard('C', { shiftKey: true }))).toBe(true)
    expect(shortcuts.manager.handleKeydown(keyboard('G', { shiftKey: true }))).toBe(true)
    expect(shortcuts.manager.handleKeydown(keyboard('U', { shiftKey: true }))).toBe(true)
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

  it('groups by the dimension that the pressed number selects', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    const dimensions = ['merchant', 'category', 'group', 'account', 'time']

    for (const [index, dimension] of dimensions.entries()) {
      expect(shortcuts.manager.handleKeydown(keyboard(String(index + 1)))).toBe(true)
      expect(handlers.apply).toHaveBeenLastCalledWith('view.set-grouping', dimension)
    }
    expect(shortcuts.manager.handleKeydown(keyboard('6'))).toBe(false)
    shortcuts.destroy()
  })

  it('rejects a server key with no browser binding', () => {
    expect(() =>
      validateCapabilities([
        ...capabilities,
        { id: 'view.new-key', key_display: 'z', description: 'new', category: '', available: true },
      ]),
    ).toThrow('view.new-key')
    expect(() => validateCapabilities(capabilities)).not.toThrow()
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

  it('lets a focused button take Enter and Space but keeps letter keys global', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    const button = document.createElement('button')
    for (const key of ['Enter', ' ']) {
      const event = keyboard(key)
      Object.defineProperty(event, 'target', { value: button })
      expect(handleMoneyflowKeydown(shortcuts.manager, event)).toBe(false)
    }
    expect(handlers.apply).not.toHaveBeenCalled()
    const letter = keyboard('g')
    Object.defineProperty(letter, 'target', { value: button })
    expect(handleMoneyflowKeydown(shortcuts.manager, letter)).toBe(true)
    expect(handlers.apply).toHaveBeenCalledWith('view.cycle-grouping')
  })

  it('opens the period chooser locally with t', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    expect(shortcuts.manager.handleKeydown(keyboard('t'))).toBe(true)
    expect(handlers.local).toHaveBeenCalledWith('time.choose')
    expect(shortcuts.manager.handleKeydown(keyboard('t', { ctrlKey: true }))).toBe(false)
    shortcuts.destroy()
  })

  it('leaves a key to the control that already handled it', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    for (const key of ['ArrowLeft', 'ArrowRight', 'Escape']) {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      event.preventDefault()
      expect(handleMoneyflowKeydown(shortcuts.manager, event)).toBe(false)
    }
    expect(handlers.apply).not.toHaveBeenCalled()
    expect(handleMoneyflowKeydown(shortcuts.manager, keyboard('ArrowLeft'))).toBe(true)
    expect(handlers.apply).toHaveBeenCalledWith('time.previous-period')
    shortcuts.destroy()
  })

  it('ignores a held Enter but keeps other keys repeating', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    expect(handleMoneyflowKeydown(shortcuts.manager, keyboard('Enter', { repeat: true }))).toBe(
      false,
    )
    expect(handlers.apply).not.toHaveBeenCalled()
    expect(handleMoneyflowKeydown(shortcuts.manager, keyboard('j', { repeat: true }))).toBe(true)
    expect(handlers.local).toHaveBeenCalledWith('cursor.down')
    expect(handleMoneyflowKeydown(shortcuts.manager, keyboard('Enter'))).toBe(true)
    expect(handlers.apply).toHaveBeenCalledWith('view.drill')
    shortcuts.destroy()
  })

  it('dispatches editing keys only when the server capability is available', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(
      capabilities.filter((capability) => capability.id === 'transaction.toggle-hidden'),
      handlers,
    )
    expect(shortcuts.manager.handleKeydown(keyboard('h'))).toBe(true)
    expect(handlers.local).toHaveBeenCalledWith('edit.hide')
    expect(shortcuts.manager.handleKeydown(keyboard('m'))).toBe(false)
  })

  it('explains an unavailable delete with x and clears the notice on the next shortcut', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const reason = 'Open the group to delete its transactions.'
    const shortcuts = createMoneyflowShortcuts(
      capabilities.map((capability) =>
        capability.id === 'transaction.delete' || capability.id === 'transaction.toggle-hidden'
          ? { ...capability, available: false, reason }
          : capability,
      ),
      handlers,
    )
    try {
      expect(shortcuts.manager.handleKeydown(keyboard('x'))).toBe(true)
      expect(status.notice).toBe(reason)
      expect(handlers.local).not.toHaveBeenCalled()
      expect(shortcuts.manager.handleKeydown(keyboard('h'))).toBe(false)
      expect(status.notice).toBe(reason)
      expect(shortcuts.manager.handleKeydown(keyboard('j'))).toBe(true)
      expect(status.notice).toBe('')
    } finally {
      status.clear()
      shortcuts.destroy()
    }
  })

  it('confirms deletion with x while delete is available', () => {
    const handlers = { local: vi.fn(), apply: vi.fn() }
    const shortcuts = createMoneyflowShortcuts(capabilities, handlers)
    expect(shortcuts.manager.handleKeydown(keyboard('x'))).toBe(true)
    expect(handlers.local).toHaveBeenCalledWith('edit.delete')
    expect(status.notice).toBe('')
    shortcuts.destroy()
  })
})

function keyboard(key: string, init: KeyboardEventInit = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key, bubbles: true, ...init })
}
