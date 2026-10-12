import { tick } from 'svelte'

import type { ViewProjection } from '../api/client'
import type { ViewController } from '../controller/view-controller.svelte'
import { EditorDraft } from '../editing/draft.svelte'
import type { LocalAction } from '../shortcuts'
import { rowActionButtons, type RowActionButton } from '../table/actions'
import { currentPageRows, cursorTarget, type CursorMove } from '../table/cursor-keys'
import type { EditorAnchor, OverlayState } from './overlay-state.svelte'
import { activeCurrency } from './period'
import { status } from './status.svelte'

export interface FocusedRow {
  identity: string
  kind: 'transaction' | 'aggregate'
}

export interface LocalActionContext {
  controller: ViewController
  overlays: OverlayState
  /**
   * Runs `work` after the queued view transitions, and holds later ones until it settles. An
   * edit or `i` then reads the projection they leave (the selection a quick Space made and the
   * row the cursor moved to), and a staged edit cannot abort a transition queued after it.
   */
  queue(work: () => unknown): Promise<unknown>
  /** Returns focus to the table unless a dialog is open. */
  focusGrid(): void
  /** Opens Amazon order import, which the app shell owns. */
  importAmazon(): void
  /** Focuses the command bar's search field. */
  focusSearch(): void
  /** Opens the command bar's Filters menu. */
  openFilters(): void
}

/** Finds the row under the cursor in the loaded window. */
export function focusedRow(
  projection: ViewProjection | undefined,
  cursorIndex: number,
): FocusedRow | undefined {
  const rows = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])]
  const row = rows.find((candidate) => candidate.index === cursorIndex)
  if (!row) return undefined
  return { identity: row.identity, kind: projection?.detail_rows ? 'transaction' : 'aggregate' }
}

/**
 * Moves the cursor from a pointer or a grid key, after pending view changes such as an Esc
 * return. These never pass through the shortcut manager, so the move clears the status notice
 * here, as every next action does.
 */
export async function moveCursor(
  context: LocalActionContext,
  move: () => Promise<void>,
): Promise<void> {
  status.clear()
  await context.queue(move)
}

/**
 * Queues `work` after view transitions, to run once the shell has drawn their result. It runs
 * after a queued newer-insights recheck that failed, too.
 */
function whenSettled(context: LocalActionContext, work: () => unknown): void {
  void context.queue(async () => {
    await tick()
    await work()
  })
}

/** Opens the information drawer for a transaction once the server returns its details. */
export async function openTransactionInformation(
  context: LocalActionContext,
  identity: string,
): Promise<void> {
  const information = await context.controller.transactionInformation?.(identity)
  if (!information) return
  context.overlays.information = information
  context.overlays.open('information')
}

/**
 * Where an editor or the delete confirmation for `target` hangs: above the selection bar while
 * rows are selected, or else below the row. A selection bar that has not rendered yet leaves it
 * below the row.
 */
function anchorFor(context: LocalActionContext, target: FocusedRow): EditorAnchor | undefined {
  const bar = context.controller.projection?.selection_count
    ? document.querySelector<HTMLElement>('.mf-selection-bar')
    : null
  const row = document.getElementById(`moneyflow-row-${target.identity}`)
  if (bar) return { element: bar, above: true }
  return row ? { element: row, above: false } : undefined
}

/**
 * Opens the merchant or category editor for `target`, such as the cursor or a pointed row, with
 * an empty draft.
 */
export function openEditor(
  context: LocalActionContext,
  field: 'merchant' | 'category',
  target: FocusedRow | undefined,
): void {
  if (!target) return
  context.overlays.anchor = anchorFor(context, target)
  context.overlays.editTarget = target
  context.overlays.editorDraft = new EditorDraft()
  context.overlays.open(field)
}

/** Moves the cursor as a key pressed outside the table asks, by the page the table last showed. */
function moveBy(controller: ViewController, move: CursorMove): Promise<void> {
  const total = controller.projection?.total_rows ?? 0
  return controller.moveCursorTo(
    cursorTarget(move, controller.cursorIndex, total, currentPageRows()),
  )
}

/** Opens a pointed row's editor after queued view transitions, as `m` and `c` do. */
export function openEditorWhenSettled(
  context: LocalActionContext,
  field: 'merchant' | 'category',
  target: FocusedRow,
): void {
  whenSettled(context, () => openEditor(context, field, target))
}

/** Maps each browser-only shortcut to its handler. */
export function localActions(context: LocalActionContext): Record<LocalAction, () => void> {
  const { controller, overlays } = context
  const row = () => focusedRow(controller.projection, controller.cursorIndex)
  const later = (work: () => unknown) => () => whenSettled(context, work)
  return {
    'cursor.up': () => void context.queue(() => controller.moveCursor(-1)),
    'cursor.down': () => void context.queue(() => controller.moveCursor(1)),
    'cursor.home': () => void context.queue(() => controller.moveHome()),
    'cursor.end': () => void context.queue(() => moveBy(controller, 'end')),
    'cursor.page-up': () => void context.queue(() => moveBy(controller, 'page-up')),
    'cursor.page-down': () => void context.queue(() => moveBy(controller, 'page-down')),
    'overlay.search': () => context.focusSearch(),
    'overlay.filters': () => context.openFilters(),
    'overlay.help': () => overlays.open('help'),
    'edit.merchant': later(() => openEditor(context, 'merchant', row())),
    'edit.category': later(() => openEditor(context, 'category', row())),
    'manage.categories': () => overlays.open('categories'),
    'manage.groups': () => overlays.open('groups'),
    'edit.review': () =>
      overlays.open(controller.providerWrite.state.status?.phase ? 'write' : 'review'),
    'view.duplicates': () => overlays.open('duplicates'),
    'provider.refresh': () => {
      if (controller.projection?.profile_kind === 'amazon') context.importAmazon()
      else void controller.provider.refresh()
    },
    'transactions.export': () => void beginExport(context),
    'time.choose': () => {
      const projection = controller.projection
      if (projection && activeCurrency(projection)) overlays.open('period')
    },
    'edit.undo': later(() => controller.editing.undo()),
    'edit.redo': later(() => controller.editing.redo()),
    'edit.hide': later(() => hideRow(context, row())),
    'edit.delete': later(() => confirmDelete(context, row())),
    'transaction.info': later(async () => {
      const target = row()
      if (target?.kind === 'transaction') await openTransactionInformation(context, target.identity)
    }),
  }
}

/**
 * Runs a row actions sheet item on `target` through the handler its key uses, after queued view
 * transitions as the key does. A transition can leave the item unavailable, such as a toggle
 * that selects rows, so it is checked again then. An editor or the delete confirmation replaces
 * the sheet and returns focus to the sheet's opener when it closes.
 */
export function runRowAction(
  context: LocalActionContext,
  action: RowActionButton['action'],
  target: FocusedRow,
): void {
  if (action === 'transaction.info')
    return void openTransactionInformation(context, target.identity)
  if (action === 'edit.hide') context.overlays.close()
  whenSettled(context, () => {
    const projection = context.controller.projection
    const button = projection && rowActionButtons(projection).find((b) => b.action === action)
    if (!button) return
    if (button.reason) return status.notify(button.reason)
    if (action === 'edit.hide') return hideRow(context, target)
    if (action === 'edit.merchant') openEditor(context, 'merchant', target)
    else if (action === 'edit.category') openEditor(context, 'category', target)
    else confirmDelete(context, target)
  })
}

/**
 * Deletes the transactions the delete confirmation names, closing it once the edit is staged.
 * Resolves whether the server accepted the delete.
 */
export async function confirmDirectDelete(context: LocalActionContext): Promise<boolean> {
  const { controller, overlays } = context
  const target = overlays.deleteTarget
  if (!target) return false
  const accepted = await controller.editing.submit({
    action: 'transaction.delete',
    input: {},
    target,
  })
  // The confirmation may have been dismissed, and another overlay opened, while this staged.
  if (accepted && overlays.current === 'delete' && overlays.deleteTarget === target) {
    overlays.close()
  }
  return accepted
}

/** Stages hiding or showing `target`'s transactions, such as the cursor or a pointed row. */
export async function hideRow(
  context: LocalActionContext,
  target: FocusedRow | undefined,
): Promise<void> {
  if (!target) return
  await context.controller.editing.submit({
    action: 'transaction.toggle-hidden',
    input: {},
    target,
  })
}

/**
 * Asks to confirm deleting the transaction `row`, or the selection when there is one. The
 * confirmation hangs where the editor would.
 */
export function confirmDelete(context: LocalActionContext, row: FocusedRow | undefined): void {
  if (row?.kind !== 'transaction') return
  const selected = context.controller.projection?.selection_count
  context.overlays.anchor = anchorFor(context, row)
  context.overlays.deleteTarget = { identity: row.identity, kind: 'transaction' }
  context.overlays.deleteCount = selected ? selected : 1
  context.overlays.open('delete')
}

async function beginExport(context: LocalActionContext): Promise<void> {
  const projection = context.controller.projection
  if (!projection) return
  if (await context.controller.export.open(projection.canonical_query)) {
    context.overlays.open('export')
  }
}
