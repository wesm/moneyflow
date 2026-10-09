import { tick } from 'svelte'

import type { ViewProjection } from '../api/client'
import type { ViewController } from '../controller/view-controller.svelte'
import type { LocalAction } from '../shortcuts'
import type { RowActionButton } from '../table/selection'
import type { OverlayState } from './overlay-state.svelte'
import { activeCurrency } from './period'
import { status } from './status.svelte'

export interface FocusedRow {
  identity: string
  kind: 'transaction' | 'aggregate'
}

export interface LocalActionContext {
  controller: ViewController
  overlays: OverlayState
  /** Settles once queued view transitions finish, so `i` reads the row the cursor moved to. */
  afterTransitions(): Promise<void>
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
 * Moves the cursor from a pointer or a grid key. These never pass through the shortcut manager,
 * so the move clears the status notice here, as every next action does.
 */
export function moveCursor(move: () => Promise<void>): void {
  status.clear()
  void move()
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

/** Opens the merchant or category editor for `target`, such as the cursor or a pointed row. */
export function openEditor(
  context: LocalActionContext,
  field: 'merchant' | 'category',
  target: FocusedRow | undefined,
): void {
  if (!target) return
  context.overlays.editTarget = target
  context.overlays.open(field)
}

/** Maps each browser-only shortcut to its handler. */
export function localActions(context: LocalActionContext): Record<LocalAction, () => void> {
  const { controller, overlays } = context
  const row = () => focusedRow(controller.projection, controller.cursorIndex)
  return {
    'cursor.up': () => void controller.moveCursor(-1),
    'cursor.down': () => void controller.moveCursor(1),
    'cursor.home': () => void controller.moveHome(),
    'overlay.search': () => context.focusSearch(),
    'overlay.filters': () => context.openFilters(),
    'overlay.help': () => overlays.open('help'),
    'edit.merchant': () => openEditor(context, 'merchant', row()),
    'edit.category': () => openEditor(context, 'category', row()),
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
    'edit.undo': () => void controller.editing.undo(),
    'edit.redo': () => void controller.editing.redo(),
    'edit.hide': () => hideRow(context, row()),
    'edit.delete': () => confirmDelete(context, row()),
    'transaction.info': () =>
      void context.afterTransitions().then(async () => {
        await tick()
        const target = row()
        if (target?.kind === 'transaction')
          await openTransactionInformation(context, target.identity)
      }),
  }
}

/**
 * Runs a row actions sheet item on `target` through the handler its key uses. An editor or the
 * delete confirmation replaces the sheet and returns focus to the sheet's opener when it closes.
 */
export function runRowAction(
  context: LocalActionContext,
  action: RowActionButton['action'],
  target: FocusedRow,
): void {
  if (action === 'edit.merchant') return openEditor(context, 'merchant', target)
  if (action === 'edit.category') return openEditor(context, 'category', target)
  if (action === 'edit.delete') return confirmDelete(context, target)
  if (action === 'transaction.info')
    return void openTransactionInformation(context, target.identity)
  hideRow(context, target)
  context.overlays.close()
}

/** Deletes the transaction the delete confirmation names, closing it once the edit is staged. */
export async function confirmDirectDelete(context: LocalActionContext): Promise<void> {
  const { controller, overlays } = context
  const target = overlays.deleteTarget
  if (!target) return
  const accepted = await controller.editing.submit({
    action: 'transaction.delete',
    input: {},
    target,
  })
  if (accepted) overlays.close()
}

/** Stages hiding or showing `target`'s transactions, such as the cursor or a pointed row. */
export function hideRow(context: LocalActionContext, target: FocusedRow | undefined): void {
  if (!target) return
  void context.controller.editing.submit({ action: 'transaction.toggle-hidden', input: {}, target })
}

/** Asks to confirm deleting the transaction `row`, or the selection when there is one. */
export function confirmDelete(context: LocalActionContext, row: FocusedRow | undefined): void {
  if (row?.kind !== 'transaction') return
  const selected = context.controller.projection?.selection_count
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
