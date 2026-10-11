import { describe, expect, it } from 'vitest'

import {
  canStart,
  deleteDetail,
  listMove,
  managerActions,
  managerIntent,
  managerRows,
  managerShortcut,
  rowName,
  stepChoices,
  stepEnter,
  stepProblem,
  stepReady,
  startStep,
  type ManagerStep,
} from './manager'
import { testCatalog } from '../../test/editing'

const category = testCatalog.categories![1]!
const sentinel = testCatalog.categories![0]!
const categories = testCatalog.categories!
const catalog = {
  ...testCatalog,
  groups: [
    ...testCatalog.groups!,
    { id: 'group-b', label: 'Other Group', protected: false, transaction_count: 0, in_use: false },
  ],
}

const groups = catalog.groups

function key(name: string, init: KeyboardEventInit = {}, target?: Element): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key: name, bubbles: true, ...init })
  if (target) Object.defineProperty(event, 'target', { value: target })
  return event
}

function step(overrides: Partial<ManagerStep>): ManagerStep {
  return { action: 'create', label: '', target: '', replace: false, ...overrides }
}

describe('managerShortcut', () => {
  const list = document.createElement('ul')

  it('maps n, r, g, m, and d to the TUI actions outside text fields', () => {
    expect(
      ['n', 'r', 'g', 'm', 'd'].map((name) => managerShortcut(key(name, {}, list), 'category')),
    ).toEqual(['create', 'rename', 'move', 'merge', 'delete'])
  })

  it('leaves letters typed in the filter to the filter', () => {
    expect(managerShortcut(key('n', {}, document.createElement('input')), 'category')).toBe(
      undefined,
    )
  })

  it('ignores modified keys and other letters', () => {
    expect(managerShortcut(key('m', { ctrlKey: true }, list), 'category')).toBe(undefined)
    expect(managerShortcut(key('M', { shiftKey: true }, list), 'category')).toBe(undefined)
    expect(managerShortcut(key('x', {}, list), 'category')).toBe(undefined)
  })

  it('has no move for groups, which have no parent', () => {
    expect(managerShortcut(key('g', {}, list), 'group')).toBe(undefined)
    expect(managerActions('group').map(({ action }) => action)).toEqual([
      'create',
      'rename',
      'merge',
      'delete',
    ])
  })
})

describe('canStart', () => {
  it('creates without a source but changes only unprotected items', () => {
    expect(canStart('create', undefined)).toBe(true)
    expect(canStart('rename', undefined)).toBe(false)
    expect(canStart('merge', sentinel)).toBe(false)
    expect(canStart('delete', category)).toBe(true)
  })
})

describe('managerRows', () => {
  it('filters by label and labels each category with its group and protection', () => {
    const rows = managerRows(catalog.categories!, '', catalog.groups)
    expect(rows.map((row) => [row.label, row.secondary, row.count, row.protected])).toEqual([
      ['Uncategorized', '', 0, true],
      ['Example Category', 'Example Group', 4, false],
    ])
    expect(managerRows(catalog.categories!, 'exam', catalog.groups)).toHaveLength(1)
  })
})

describe('stepChoices', () => {
  it('offers every group to a new category and other groups to a move', () => {
    expect(stepChoices(step({}), 'category', catalog).map((group) => group.id)).toEqual([
      'group-a',
      'group-b',
    ])
    const move = step({ action: 'move', source: category })
    expect(stepChoices(move, 'category', catalog).map((group) => group.id)).toEqual(['group-b'])
  })

  it('offers the other items of the same kind as a destination or replacement', () => {
    const merge = step({ action: 'merge', source: category })
    expect(stepChoices(merge, 'category', catalog).map((choice) => choice.id)).toEqual([
      'category-uncategorized',
    ])
    const remove = step({ action: 'delete', source: catalog.groups[0] })
    expect(stepChoices(remove, 'group', catalog).map((choice) => choice.id)).toEqual(['group-b'])
  })
})

describe('stepReady', () => {
  it('needs a name, and a group for a new category', () => {
    expect(stepReady(step({ label: '  ' }), 'group', groups)).toBe(false)
    expect(stepReady(step({ label: 'New Group' }), 'group', groups)).toBe(true)
    expect(stepReady(step({ label: 'New Category' }), 'category', categories)).toBe(false)
    expect(
      stepReady(step({ label: 'New Category', target: 'group-a' }), 'category', categories),
    ).toBe(true)
  })

  it('needs a different name to rename', () => {
    const rename = step({ action: 'rename', source: category })
    expect(stepReady({ ...rename, label: 'Example Category ' }, 'category', categories)).toBe(false)
    expect(stepReady({ ...rename, label: 'Renamed Category' }, 'category', categories)).toBe(true)
  })

  it('needs an explicit destination to move or merge', () => {
    for (const action of ['move', 'merge'] as const) {
      expect(stepReady(step({ action, source: category }), 'category', categories)).toBe(false)
      expect(
        stepReady(step({ action, source: category, target: 'group-b' }), 'category', categories),
      ).toBe(true)
    }
  })
})

describe('deleting', () => {
  const unused = {
    ...category,
    id: 'category-b',
    label: 'Unused Category',
    transaction_count: 0,
    in_use: false,
  }
  const withUnused = { ...catalog, categories: [...categories, unused] }
  const remove = (source = category) => startStep('category', 'delete', source, withUnused)

  it('deletes an entry the server reports unused with no replacement', () => {
    const unusedStep = remove(unused)
    expect(unusedStep.replace).toBe(false)
    expect(stepReady(unusedStep, 'category', withUnused.categories)).toBe(true)
    expect(managerIntent('category', unusedStep, '').input).toEqual({
      taxonomy: 'delete',
      entity_id: 'category-b',
    })
  })

  it('needs a replacement for an entry in use', () => {
    const used = remove()
    expect(used.replace).toBe(true)
    expect(stepReady(used, 'category', withUnused.categories)).toBe(false)
    const replaced = { ...used, target: 'category-b' }
    expect(stepReady(replaced, 'category', withUnused.categories)).toBe(true)
    expect(managerIntent('category', replaced, '').input).toEqual({
      taxonomy: 'delete',
      entity_id: 'category-a',
      replacement_id: 'category-b',
    })
  })

  it('follows in_use, not the committed count, when only pending edits use the entry', () => {
    // A new category that a pending recategorization filled: 0 committed, but in use.
    const pending = { ...unused, in_use: true }
    expect(remove(pending).replace).toBe(true)
    expect(stepReady(remove(pending), 'category', withUnused.categories)).toBe(false)
    // Transactions that a pending edit moved out: counted, but no longer in use.
    expect(remove({ ...category, in_use: false }).replace).toBe(false)
  })

  it('asks for a replacement on delete steps only, and every step but create needs an entry', () => {
    expect(startStep('category', 'merge', category, withUnused).replace).toBe(false)
    expect(startStep('category', 'rename', category, withUnused)).toMatchObject({
      label: 'Example Category',
      target: '',
    })
    expect(startStep('category', 'create', undefined, withUnused).target).toBe('group-a')
    expect(startStep('group', 'create', undefined, withUnused).target).toBe('')
    expect(() => startStep('category', 'delete', undefined, withUnused)).toThrow(/delete/)
  })
})

describe('deleteDetail', () => {
  const empty = {
    id: 'group-c',
    label: 'Empty Group',
    protected: false,
    transaction_count: 0,
    in_use: false,
  }

  it('counts the transactions of a category or a group that has them', () => {
    expect(deleteDetail('category', category, catalog)).toBe('4 transactions')
    expect(deleteDetail('category', { ...category, transaction_count: 1 }, catalog)).toBe(
      '1 transaction',
    )
    expect(deleteDetail('group', catalog.groups[0]!, catalog)).toBe('4 transactions')
    expect(deleteDetail('group', empty, catalog)).toBe('0 transactions')
  })

  it('names pending edits for a category in use without committed transactions', () => {
    const pending = { ...category, transaction_count: 0, in_use: true }
    expect(deleteDetail('category', pending, catalog)).toBe('Pending edits use this category')
  })

  it('says pending edits already moved the contents of an entry no longer in use', () => {
    const moved = (count: number) => ({ ...category, transaction_count: count, in_use: false })
    expect(deleteDetail('category', moved(4), catalog)).toBe(
      'Pending edits already moved its 4 transactions elsewhere',
    )
    expect(deleteDetail('category', moved(1), catalog)).toBe(
      'Pending edits already moved its 1 transaction elsewhere',
    )
    expect(deleteDetail('group', { ...catalog.groups[0]!, in_use: false }, catalog)).toBe(
      'Pending edits already moved all its categories elsewhere',
    )
  })

  it('names the categories of a group in use without transactions', () => {
    const quiet = (id: string) => ({ ...category, id, parent_id: 'group-c', transaction_count: 0 })
    const holding = { ...catalog, categories: [...categories, quiet('c-1')] }
    expect(deleteDetail('group', { ...empty, in_use: true }, holding)).toBe('1 category')
    const both = { ...catalog, categories: [...categories, quiet('c-1'), quiet('c-2')] }
    expect(deleteDetail('group', { ...empty, in_use: true }, both)).toBe('2 categories')
  })
})

describe('stepProblem', () => {
  const taken = 'That label already exists; use merge instead.'

  it('refuses a new name that another entry already uses', () => {
    expect(stepProblem(step({ label: 'Example Category' }), categories)).toBe(taken)
    expect(
      stepReady(step({ label: 'Example Category', target: 'group-a' }), 'category', categories),
    ).toBe(false)
  })

  it('ignores case and surrounding space, as the TUI does', () => {
    expect(stepProblem(step({ label: '  example category ' }), categories)).toBe(taken)
    const rename = step({ action: 'rename', source: category, label: 'UNCATEGORIZED' })
    expect(stepProblem(rename, categories)).toBe(taken)
    expect(stepReady(rename, 'category', categories)).toBe(false)
  })

  it('lets an entry keep its own name in another case', () => {
    const rename = step({ action: 'rename', source: category, label: 'example category' })
    expect(stepProblem(rename, categories)).toBe('')
    expect(stepReady(rename, 'category', categories)).toBe(true)
  })

  it('checks only names, not destinations', () => {
    const merge = step({ action: 'merge', source: category, label: 'Uncategorized', target: 't' })
    expect(stepProblem(merge, categories)).toBe('')
    expect(stepProblem(step({ label: 'Fresh Name' }), categories)).toBe('')
  })
})

describe('managerIntent', () => {
  it('stages a category create with its new ID, trimmed name, and group', () => {
    const intent = managerIntent(
      'category',
      step({ label: ' New Category ', target: 'group-a' }),
      'category_new',
    )
    expect(intent).toEqual({
      action: 'category.manage',
      input: {
        taxonomy: 'create',
        entity_id: 'category_new',
        label: 'New Category',
        group_id: 'group-a',
      },
    })
  })

  it('stages a group create without a group', () => {
    expect(managerIntent('group', step({ label: 'New Group' }), 'group_new').input).toEqual({
      taxonomy: 'create',
      entity_id: 'group_new',
      label: 'New Group',
    })
  })

  it('sends rename labels, move and merge destinations, and delete replacements', () => {
    const intent = (action: ManagerStep['action']) =>
      managerIntent('category', step({ action, source: category, label: 'Name', target: 't' }), '')
        .input
    expect(intent('rename')).toEqual({ taxonomy: 'rename', entity_id: 'category-a', label: 'Name' })
    expect(intent('move')).toEqual({
      taxonomy: 'move',
      entity_id: 'category-a',
      destination_id: 't',
    })
    expect(intent('merge')).toEqual({
      taxonomy: 'merge',
      entity_id: 'category-a',
      destination_id: 't',
    })
    expect(intent('delete')).toEqual({
      taxonomy: 'delete',
      entity_id: 'category-a',
      replacement_id: 't',
    })
    expect(managerIntent('group', step({ action: 'merge', source: category }), '').action).toBe(
      'category-group.manage',
    )
  })
})

describe('stepEnter', () => {
  function panel() {
    const element = document.createElement('div')
    element.innerHTML =
      '<input type="text"><button>Merge</button><input role="combobox"><div tabindex="-1"></div>'
    const [field, button, combobox, surface] = Array.from(element.children)
    return { element, field: field!, button: button!, combobox: combobox!, surface: surface! }
  }

  it('confirms from the name field and the panel', () => {
    const { element, field, surface } = panel()
    expect(stepEnter(key('Enter', {}, field), element, false)).toBe('confirm')
    expect(stepEnter(key('Enter', {}, surface), element, false)).toBe('confirm')
  })

  it('leaves Enter to buttons and the combobox, which act on their own', () => {
    const { element, button, combobox } = panel()
    expect(stepEnter(key('Enter', {}, button), element, false)).toBe('pass')
    expect(stepEnter(key('Enter', {}, combobox), element, false)).toBe('pass')
  })

  it('swallows a held, modified, or in-flight Enter', () => {
    const { element, field } = panel()
    expect(stepEnter(key('Enter', { repeat: true }, field), element, false)).toBe('swallow')
    expect(stepEnter(key('Enter', { ctrlKey: true }, field), element, false)).toBe('swallow')
    expect(stepEnter(key('Enter', {}, field), element, true)).toBe('swallow')
  })

  it('passes an Enter that accepts an input method candidate', () => {
    const { element, field } = panel()
    expect(stepEnter(key('Enter', { isComposing: true }, field), element, false)).toBe('pass')
    expect(stepEnter(key('Enter', { keyCode: 229 }, field), element, false)).toBe('pass')
  })

  it('passes other keys, claimed keys, and keys outside the panel', () => {
    const { element, field } = panel()
    expect(stepEnter(key('a', {}, field), element, false)).toBe('pass')
    const claimed = key('Enter', { cancelable: true }, field)
    claimed.preventDefault()
    expect(stepEnter(claimed, element, false)).toBe('pass')
    expect(stepEnter(key('Enter', {}, document.body), element, false)).toBe('pass')
  })
})

describe('listMove', () => {
  it('moves through the list, clamping at the last row', () => {
    expect(listMove(key('ArrowDown'), 0, 3)).toBe(1)
    expect(listMove(key('ArrowDown'), 2, 3)).toBe(2)
    expect(listMove(key('Home'), 2, 3)).toBe(0)
    expect(listMove(key('End'), 0, 3)).toBe(2)
  })

  it('returns to the filter with Up from the first row or an empty list', () => {
    expect(listMove(key('ArrowUp'), 1, 3)).toBe(0)
    expect(listMove(key('ArrowUp'), 0, 3)).toBe(-1)
    expect(listMove(key('ArrowUp'), 0, 0)).toBe(-1)
  })

  it('ignores modified arrows and other keys', () => {
    expect(listMove(key('ArrowDown', { altKey: true }), 0, 3)).toBe(undefined)
    expect(listMove(key('n'), 0, 3)).toBe(undefined)
    expect(listMove(key('ArrowDown'), 0, 0)).toBe(undefined)
  })
})

describe('rowName', () => {
  it('names the label, group, protection, and count', () => {
    const [sentinelRow, row] = managerRows(catalog.categories!, '', catalog.groups)
    expect(rowName(row!)).toBe('Example Category, Example Group, 4 transactions')
    expect(rowName(sentinelRow!)).toBe('Uncategorized, protected, 0 transactions')
  })
})
