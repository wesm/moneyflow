import type { EditorCatalog } from '../api/client'
import type { MutationIntent } from '../controller/editing'
import { editorOptions, type EditorChoice, type EditorOption } from './options'
import { transactionCount } from './toasts'

/** Which manager is open: `C` manages categories and `G` manages category groups. */
export type ManagerKind = 'category' | 'group'

/** The server's taxonomy actions, as `category.manage` and `category-group.manage` take them. */
export type ManagerAction = 'create' | 'rename' | 'move' | 'merge' | 'delete'

/** One inline step: the action, the item it changes, and what the user has entered so far. */
export interface ManagerStep {
  action: ManagerAction
  /** The item the step changes; absent when creating. */
  source?: EditorChoice | undefined
  /** The name typed for create and rename. */
  label: string
  /** The chosen group (create, move), destination (merge), or replacement (delete) ID. */
  target: string
}

/** A row of the manager's list: a catalog item with its group and count. */
export interface ManagerRow extends EditorOption {
  protected: boolean
}

const SHORTCUTS: Record<string, ManagerAction> = {
  n: 'create',
  r: 'rename',
  g: 'move',
  m: 'merge',
  d: 'delete',
}

/** The toolbar button that starts each action. */
export const ACTION_LABELS: Record<ManagerAction, string> = {
  create: 'New',
  rename: 'Rename',
  move: 'Move to group',
  merge: 'Merge',
  delete: 'Delete',
}

/** The button that confirms each action's step. */
export const CONFIRM_LABELS: Record<ManagerAction, string> = {
  create: 'Create',
  rename: 'Rename',
  move: 'Move',
  merge: 'Merge',
  delete: 'Delete',
}

/** The heading of an open step, such as "Merge Dining into". */
export function stepTitle(kind: ManagerKind, step: ManagerStep): string {
  const name = step.source?.label ?? ''
  switch (step.action) {
    case 'create':
      return kind === 'category' ? 'New category' : 'New group'
    case 'rename':
      return `Rename ${name}`
    case 'move':
      return `Move ${name} to group`
    case 'merge':
      return `Merge ${name} into`
    case 'delete':
      return `Delete ${name}`
  }
}

/** What a merge or delete does to the items under the source; nothing for other actions. */
export function stepNote(kind: ManagerKind, action: ManagerAction): string {
  const moved = kind === 'category' ? 'transactions' : 'categories'
  if (action === 'merge') return `Its ${moved} move to the destination.`
  if (action === 'delete') return `Its ${moved} move to the replacement.`
  return ''
}

/** The label of a step's picker: the group to move to, a destination, or a replacement. */
export function pickerLabel(action: ManagerAction): string {
  if (action === 'move') return 'Group'
  return action === 'merge' ? 'Destination' : 'Replacement'
}

/** The manager's actions in toolbar order, with the key that starts each. */
export function managerActions(kind: ManagerKind): Array<{ action: ManagerAction; key: string }> {
  return Object.entries(SHORTCUTS)
    .filter(([, action]) => kind === 'category' || action !== 'move')
    .map(([key, action]) => ({ action, key }))
}

/**
 * The action a key starts in the manager's shortcut scope, or `undefined`. The keys act only
 * outside text fields, so typing in the filter types letters. Modified keys start nothing, and
 * `g` (move to another group) applies to categories only.
 */
export function managerShortcut(
  event: KeyboardEvent,
  kind: ManagerKind,
): ManagerAction | undefined {
  if (event.ctrlKey || event.metaKey || event.altKey || event.isComposing) return undefined
  if (textEntry(event.target)) return undefined
  const action = SHORTCUTS[event.key]
  return action === 'move' && kind === 'group' ? undefined : action
}

/** Whether `action` can start on `source`: creating needs none; the rest need an unprotected one. */
export function canStart(action: ManagerAction, source: EditorChoice | undefined): boolean {
  return action === 'create' || (source !== undefined && !source.protected)
}

/** The categories or groups the manager lists. */
export function managerChoices(catalog: EditorCatalog | undefined, kind: ManagerKind) {
  return (kind === 'category' ? catalog?.categories : catalog?.groups) ?? []
}

/** The listed rows matching the filter, each category labelled with its group. */
export function managerRows(
  choices: EditorChoice[],
  filter: string,
  groups: EditorChoice[],
): ManagerRow[] {
  const locked = new Set(choices.filter((choice) => choice.protected).map((choice) => choice.id))
  return editorOptions(choices, filter, false, groups).map((option) => ({
    ...option,
    protected: locked.has(option.id),
  }))
}

/**
 * What a step may choose: another group for a move, or another item of the same kind for a
 * merge's destination or a delete's replacement. A new category picks from every group.
 */
export function stepChoices(
  step: ManagerStep,
  kind: ManagerKind,
  catalog: EditorCatalog | undefined,
): EditorChoice[] {
  const groups = catalog?.groups ?? []
  if (step.action === 'create') return groups
  if (step.action === 'move') return groups.filter((group) => group.id !== step.source?.parent_id)
  return managerChoices(catalog, kind).filter((choice) => choice.id !== step.source?.id)
}

/**
 * Whether the step has what its action needs. Create needs a name, and a group for a category.
 * Rename needs a new name. Move, merge, and delete need an explicit group, destination, or
 * replacement. The server still validates every staged action.
 */
export function stepReady(step: ManagerStep, kind: ManagerKind): boolean {
  const label = step.label.trim()
  if (step.action === 'create') return label !== '' && (kind === 'group' || step.target !== '')
  if (step.action === 'rename') return label !== '' && label !== step.source?.label
  return step.target !== ''
}

/** The staged intent for a ready step. `newID` names the item a create adds. */
export function managerIntent(kind: ManagerKind, step: ManagerStep, newID: string): MutationIntent {
  const { action, target } = step
  const label = step.label.trim()
  return {
    action: kind === 'category' ? 'category.manage' : 'category-group.manage',
    input: {
      taxonomy: action,
      entity_id: action === 'create' ? newID : (step.source?.id ?? ''),
      ...(action === 'create' || action === 'rename' ? { label } : {}),
      ...(action === 'create' && kind === 'category' ? { group_id: target } : {}),
      ...(action === 'move' || action === 'merge' ? { destination_id: target } : {}),
      ...(action === 'delete' ? { replacement_id: target } : {}),
    },
  }
}

/**
 * Where a key moves the list's active row: the next or previous row, the first (Home), or the
 * last (End). `-1` means Up from the first row, which returns to the filter. Other keys and
 * modified arrows return `undefined`.
 */
export function listMove(event: KeyboardEvent, index: number, count: number): number | undefined {
  if (event.altKey || event.ctrlKey || event.metaKey) return undefined
  if (event.key === 'ArrowUp') return count === 0 ? -1 : index - 1
  if (count === 0) return undefined
  if (event.key === 'ArrowDown') return Math.min(count - 1, index + 1)
  if (event.key === 'Home') return 0
  if (event.key === 'End') return count - 1
  return undefined
}

/** A row's accessible name, such as "Dining, Food, 12 transactions". */
export function rowName(row: ManagerRow): string {
  const count = transactionCount(row.count)
  return [row.label, row.secondary, row.protected ? 'protected' : '', count]
    .filter(Boolean)
    .join(', ')
}

/** Controls that act on their own Enter, so the same Enter must not also confirm the step. */
const OWN_ENTER = 'button, a[href], select, textarea, [role=button], [role=link], [role=combobox]'

/**
 * What an Enter does while a step is open in the manager `panel`:
 * - `confirm` stages the step;
 * - `swallow` prevents the key and does nothing else;
 * - `pass` leaves the key alone.
 *
 * Enter confirms from the step's name field or the panel itself, not from a control with its own
 * Enter, such as a button or the destination combobox. A held Enter's repeats, a modified Enter,
 * and an Enter while the step is staging confirm nothing.
 */
export function stepEnter(
  event: KeyboardEvent,
  panel: Element | undefined,
  staging: boolean,
): 'confirm' | 'swallow' | 'pass' {
  if (event.key !== 'Enter' || event.defaultPrevented || !panel) return 'pass'
  const target = event.target
  if (!(target instanceof Element) || !panel.contains(target)) return 'pass'
  if (target.closest(OWN_ENTER)) return 'pass'
  return plainEnter(event) && !staging ? 'confirm' : 'swallow'
}

/** An Enter that is neither a held repeat nor modified. */
export function plainEnter(event: KeyboardEvent): boolean {
  return !(event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey)
}

function textEntry(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    (target instanceof HTMLElement && target.isContentEditable)
  )
}
