import { describe, expect, it } from 'vitest'

import { editorKey, type EditorFocus, type KeyLike } from './editor-keys'

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
  const focuses: EditorFocus[] = ['scope', 'stage', 'cancel']

  it('leaves Tab and Shift+Tab to the normal focus order, never changing scope', () => {
    for (const focus of ['combobox', ...focuses] as EditorFocus[]) {
      for (const listOpen of [true, false]) {
        const state = { listOpen, valid: true, focus }
        expect(editorKey(key('Tab'), state)).toBe('none')
        expect(editorKey(key('Tab', { shiftKey: true }), state)).toBe('none')
      }
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

  it('treats a list reported open outside the combobox as closed', () => {
    const state = { listOpen: true, valid: true, focus: 'scope' } as const
    expect(editorKey(key('Escape'), state)).toBe('cancel')
    expect(editorKey(key('Enter'), state)).toBe('stage')
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
    expect(editorKey(key('Enter'), { listOpen: false, valid: true, focus: 'cancel' })).toBe('none')
  })
})

describe('editorKey during text composition', () => {
  it('ignores keys that commit an input method composition', () => {
    expect(editorKey(key('Enter', { isComposing: true }), open)).toBe('none')
    expect(editorKey(key('Escape', { isComposing: true }), open)).toBe('none')
  })
})
