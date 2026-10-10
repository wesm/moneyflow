import { describe, expect, it } from 'vitest'

import { editorOptions, type EditorChoice } from './options'

function choice(id: string, label: string, count: number, parent?: string): EditorChoice {
  return {
    id,
    label,
    protected: false,
    transaction_count: count,
    ...(parent ? { parent_id: parent } : {}),
  }
}

const merchants = [
  choice('m-1', 'Corner Grocery', 12),
  choice('m-2', 'Example Coffee', 3),
  choice('m-3', 'Grocery Outlet', 7),
]

describe('editorOptions', () => {
  it('lists every choice with its count when the query is empty', () => {
    expect(editorOptions(merchants, '', true)).toEqual([
      { id: 'm-1', label: 'Corner Grocery', secondary: '', count: 12, create: false },
      { id: 'm-2', label: 'Example Coffee', secondary: '', count: 3, create: false },
      { id: 'm-3', label: 'Grocery Outlet', secondary: '', count: 7, create: false },
    ])
  })

  it('filters labels by a case-insensitive substring, keeping catalog order', () => {
    const labels = editorOptions(merchants, 'GROCERY', false).map((option) => option.label)
    expect(labels).toEqual(['Corner Grocery', 'Grocery Outlet'])
  })

  it('offers Create with the trimmed text after the matches when no label matches exactly', () => {
    const options = editorOptions(merchants, '  Grocer  ', true)
    expect(options.map((option) => option.label)).toEqual([
      'Corner Grocery',
      'Grocery Outlet',
      'Grocer',
    ])
    expect(options.at(-1)).toEqual({
      id: '',
      label: 'Grocer',
      secondary: '',
      count: 0,
      create: true,
    })
  })

  it('treats a label that differs only in case as an existing value, not a create', () => {
    const options = editorOptions(merchants, 'corner grocery ', true)
    expect(options).toEqual([
      { id: 'm-1', label: 'Corner Grocery', secondary: '', count: 12, create: false },
    ])
  })

  it('offers only Create when nothing matches', () => {
    expect(editorOptions(merchants, 'New Bakery', true)).toEqual([
      { id: '', label: 'New Bakery', secondary: '', count: 0, create: true },
    ])
  })

  it('offers no Create for blank text or when creating is off', () => {
    expect(editorOptions(merchants, '   ', true).some((option) => option.create)).toBe(false)
    expect(editorOptions(merchants, 'New Bakery', false)).toEqual([])
  })

  it('handles an empty catalog', () => {
    expect(editorOptions([], '', true)).toEqual([])
    expect(editorOptions([], 'Gifts', true)).toEqual([
      { id: '', label: 'Gifts', secondary: '', count: 0, create: true },
    ])
  })

  it('shows each category group label as secondary text', () => {
    const groups = [choice('g-food', 'Food', 0), choice('g-home', 'Home', 0)]
    const categories = [
      choice('c-1', 'Groceries', 9, 'g-food'),
      choice('c-2', 'Rent', 1, 'g-home'),
      choice('c-3', 'Uncategorized', 2),
      choice('c-4', 'Orphan', 4, 'g-missing'),
    ]
    expect(editorOptions(categories, '', false, groups)).toEqual([
      { id: 'c-1', label: 'Groceries', secondary: 'Food', count: 9, create: false },
      { id: 'c-2', label: 'Rent', secondary: 'Home', count: 1, create: false },
      { id: 'c-3', label: 'Uncategorized', secondary: '', count: 2, create: false },
      { id: 'c-4', label: 'Orphan', secondary: '', count: 4, create: false },
    ])
  })

  it('matches the label only, not the group label', () => {
    const groups = [choice('g-food', 'Food', 0)]
    const categories = [choice('c-1', 'Groceries', 9, 'g-food')]
    expect(editorOptions(categories, 'food', false, groups)).toEqual([])
  })
})
