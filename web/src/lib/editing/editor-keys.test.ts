import { describe, expect, it } from 'vitest'

import { controlKey, editorKey, type EditorFocus, type KeyLike } from './editor-keys'

const open = { listOpen: true, valid: true, focus: 'combobox' } as const
const closed = { listOpen: false, valid: true, focus: 'combobox' } as const
const ctrl = { ctrlKey: true } as const
const meta = { metaKey: true } as const

function key(name: string, modifiers: Omit<KeyLike, 'key'> = {}): KeyLike {
  return { key: name, ...modifiers }
}

describe('editorKey with the list open', () => {
  it('leaves typing to the input, which filters the list', () => {
    expect(editorKey(key('a'), open)).toBe('none')
    expect(editorKey(key('Backspace'), open)).toBe('none')
  })

  it('moves the active option with the arrow keys', () => {
    expect(editorKey(key('ArrowDown'), open)).toBe('move-next')
    expect(editorKey(key('ArrowUp'), open)).toBe('move-prev')
  })

  it('accepts the active option on Enter', () => {
    expect(editorKey(key('Enter'), open)).toBe('accept')
  })

  it('accepts the active option even when the current value is invalid', () => {
    expect(editorKey(key('Enter'), { ...open, valid: false })).toBe('accept')
  })

  it('accepts and stages on Ctrl+Enter and Cmd+Enter', () => {
    expect(editorKey(key('Enter', ctrl), open)).toBe('accept-and-stage')
    expect(editorKey(key('Enter', meta), open)).toBe('accept-and-stage')
  })

  it('closes the list on Escape instead of cancelling, keeping the text', () => {
    expect(editorKey(key('Escape'), open)).toBe('close-list')
  })
})

describe('editorKey with the list closed', () => {
  it('opens the list when typing', () => {
    expect(editorKey(key('a'), closed)).toBe('open')
    expect(editorKey(key(' '), closed)).toBe('open')
    expect(editorKey(key('é', { altKey: true }), closed)).toBe('open')
    expect(editorKey(key('Backspace'), closed)).toBe('open')
    expect(editorKey(key('Delete'), closed)).toBe('open')
  })

  it('does not treat shortcuts, lone modifiers, or caret keys as typing', () => {
    expect(editorKey(key('a', ctrl), closed)).toBe('none')
    expect(editorKey(key('v', meta), closed)).toBe('none')
    expect(editorKey(key('Shift', { shiftKey: true }), closed)).toBe('none')
    expect(editorKey(key('ArrowLeft'), closed)).toBe('none')
    expect(editorKey(key('Home'), closed)).toBe('none')
  })

  it('opens the list with the arrow keys', () => {
    expect(editorKey(key('ArrowDown'), closed)).toBe('open')
    expect(editorKey(key('ArrowUp'), closed)).toBe('open')
  })

  it('stages on Enter when the value is valid', () => {
    expect(editorKey(key('Enter'), closed)).toBe('stage')
  })

  it('does nothing on Enter when the value is invalid', () => {
    expect(editorKey(key('Enter'), { ...closed, valid: false })).toBe('none')
  })

  it('stages on Ctrl+Enter and Cmd+Enter', () => {
    expect(editorKey(key('Enter', ctrl), closed)).toBe('stage')
    expect(editorKey(key('Enter', meta), closed)).toBe('stage')
  })

  it('does not stage on Ctrl+Enter when the value is invalid', () => {
    expect(editorKey(key('Enter', ctrl), { ...closed, valid: false })).toBe('none')
  })

  it('cancels the editor on Escape', () => {
    expect(editorKey(key('Escape'), closed)).toBe('cancel')
  })
})

describe('editorKey outside the combobox', () => {
  const focuses: EditorFocus[] = ['scope', 'stage']

  it('leaves Tab and Shift+Tab to the normal focus order, never changing scope', () => {
    const outside = focuses.map((focus) => ({ listOpen: false, valid: true, focus }))
    for (const state of [open, closed, ...outside]) {
      expect(editorKey(key('Tab'), state)).toBe('none')
      expect(editorKey(key('Tab', { shiftKey: true }), state)).toBe('none')
    }
  })

  it('leaves the arrow keys to the scope radio group and the buttons', () => {
    for (const focus of focuses) {
      const state = { listOpen: false, valid: true, focus }
      expect(editorKey(key('ArrowDown'), state)).toBe('none')
      expect(editorKey(key('ArrowUp'), state)).toBe('none')
      expect(editorKey(key('a'), state)).toBe('none')
    }
  })

  it('cancels on Escape from any control', () => {
    for (const focus of focuses) {
      expect(editorKey(key('Escape'), { listOpen: false, valid: true, focus })).toBe('cancel')
    }
  })

  it('stages on Ctrl+Enter and Cmd+Enter from any control', () => {
    for (const focus of focuses) {
      const state = { listOpen: false, valid: true, focus }
      expect(editorKey(key('Enter', ctrl), state)).toBe('stage')
      expect(editorKey(key('Enter', meta), state)).toBe('stage')
    }
  })

  it('stages on Enter in the scope group and leaves Enter on a button to the button', () => {
    expect(editorKey(key('Enter'), { listOpen: false, valid: true, focus: 'scope' })).toBe('stage')
    expect(editorKey(key('Enter'), { listOpen: false, valid: true, focus: 'stage' })).toBe('none')
  })
})

describe('editorKey during text composition', () => {
  it('ignores keys while an input method composition is active', () => {
    for (const state of [open, closed]) {
      expect(editorKey(key('Enter', { isComposing: true }), state)).toBe('none')
      expect(editorKey(key('Escape', { isComposing: true }), state)).toBe('none')
      expect(editorKey(key('ArrowDown', { isComposing: true }), state)).toBe('none')
    }
  })

  it("ignores Safari's Enter that ends a composition, which reports key code 229", () => {
    expect(editorKey(key('Enter', { keyCode: 229 }), open)).toBe('none')
    expect(editorKey(key('Enter', { keyCode: 229 }), closed)).toBe('none')
    expect(editorKey(key('Enter', { keyCode: 13 }), closed)).toBe('stage')
  })
})

describe('editorKey with modifiers it does not own', () => {
  it('leaves Ctrl, Cmd, and Alt arrows to the input in either list state', () => {
    for (const state of [open, closed]) {
      for (const modifier of [ctrl, meta, { altKey: true }]) {
        expect(editorKey(key('ArrowDown', modifier), state)).toBe('none')
        expect(editorKey(key('ArrowUp', modifier), state)).toBe('none')
      }
    }
  })

  it('leaves Shift+Enter and Alt+Enter alone in either list state', () => {
    for (const state of [open, closed]) {
      expect(editorKey(key('Enter', { shiftKey: true }), state)).toBe('none')
      expect(editorKey(key('Enter', { altKey: true }), state)).toBe('none')
    }
  })

  it('ignores a held Enter, so an accept and a stage take two presses', () => {
    for (const state of [open, closed, { ...closed, focus: 'scope' as const }]) {
      expect(editorKey(key('Enter', { repeat: true }), state)).toBe('none')
      expect(editorKey(key('Enter', { ...ctrl, repeat: true }), state)).toBe('none')
    }
    expect(editorKey(key('ArrowDown', { repeat: true }), open)).toBe('move-next')
  })
})

describe('controlKey', () => {
  const control = (attributes: Record<string, string> = {}) => {
    const element = document.createElement('button')
    for (const [name, value] of Object.entries(attributes)) element.setAttribute(name, value)
    return element
  }

  it('stages on Enter from a scope radio, but leaves Enter on a button to the button', () => {
    expect(controlKey({ key: 'Enter' }, control({ role: 'radio' }), true)).toBe('stage')
    expect(controlKey({ key: 'Enter' }, control(), true)).toBe('none')
    expect(controlKey({ key: 'Enter', ctrlKey: true }, control(), true)).toBe('stage')
    expect(controlKey({ key: 'Enter' }, control({ role: 'radio' }), false)).toBe('none')
  })

  it('cancels on Escape unless the control has an open popup', () => {
    expect(controlKey({ key: 'Escape' }, control(), true)).toBe('cancel')
    expect(controlKey({ key: 'Escape' }, control({ 'aria-expanded': 'true' }), true)).toBe('none')
  })

  it('leaves arrows and typing to the control', () => {
    expect(controlKey({ key: 'ArrowRight' }, control({ role: 'radio' }), true)).toBe('none')
    expect(controlKey({ key: 'a' }, control(), true)).toBe('none')
  })
})
