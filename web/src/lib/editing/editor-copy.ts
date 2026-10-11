import type { SegmentedControlOption } from '@kenn-io/kit-ui'

import type { MerchantPreview, ViewProjection } from '../api/client'
import type { MutationIntent } from '../controller/editing'
import type { MerchantScope } from './merchant-scope'
import type { EditorChoice } from './options'
import { transactionCount } from './toasts'

export type EditorKind = 'merchant' | 'category'
/** A merchant edit's scope: the targeted transactions, or every transaction of the merchant. */
export type EditScope = 'transactions' | 'entity'

const OUTSIDE_REASONS = new Intl.ListFormat('en', { type: 'disjunction' })

/** The text next to the editor's title: "· 3 selected", or the target row's transactions. */
export function countLine(projection: ViewProjection, identity: string): string {
  if (projection.selection_count > 0) return `· ${projection.selection_count} selected`
  if (projection.detail_rows) return '· 1 transaction'
  const row = projection.aggregate_rows?.find((candidate) => candidate.identity === identity)
  return row ? `· ${transactionCount(row.count)}` : ''
}

/**
 * The merchant scope choices. Counts appear once their previews arrive. The whole merchant stays
 * listed when unavailable, disabled, with the reason as its title.
 */
export function scopeOptions(
  scope: MerchantScope,
  counts: Partial<Record<EditScope, number | undefined>>,
  selecting: boolean,
): SegmentedControlOption[] {
  const selected = counts.transactions
  let transactions = selecting ? 'Selected transactions' : 'These transactions'
  if (selected !== undefined) {
    transactions = selecting
      ? `${selected} selected ${selected === 1 ? 'transaction' : 'transactions'}`
      : transactionCount(selected)
  }
  const whole = counts.entity
  const entity: SegmentedControlOption = {
    value: 'entity',
    label:
      scope.entity && whole !== undefined
        ? `Whole merchant · ${transactionCount(whole)}`
        : 'Whole merchant',
    disabled: !scope.entity,
    ...(scope.reason ? { title: scope.reason } : {}),
  }
  return [{ value: 'transactions', label: transactions }, entity]
}

/**
 * The impact notice. A whole-merchant rename states its full count and how many of those
 * transactions the current view hides; a rename onto another existing merchant names it. A
 * rename onto the merchant being edited, such as a case fix, is not a merge.
 *
 * @param preview The preview for `scope`, once loaded.
 * @param merge The existing merchant label the typed name matches.
 * @param currency The view's currency, which the outside-view reasons leave unnamed.
 */
export function impactLines(
  preview: MerchantPreview | undefined,
  scope: EditScope,
  merge: string | undefined,
  currency: string | undefined,
): string[] {
  const lines: string[] = []
  if (preview && scope === 'entity') {
    lines.push(`Changes all ${transactionCount(preview.affected_transactions)} of this merchant.`)
    const outside = preview.outside_view
    if (outside > 0) {
      const currencies = new Set(preview.outside_currencies.map((entry) => entry.currency))
      currencies.delete(currency ?? '')
      const reasons = OUTSIDE_REASONS.format(['other periods', 'filters', ...currencies])
      const verb = outside === 1 ? 'is' : 'are'
      lines.push(`${outside} of these ${verb} outside the current view (${reasons}).`)
    }
  }
  if (merge && merge !== sourceMerchant(preview, scope)) lines.push(`Merges into ${merge}`)
  return lines
}

/**
 * The one merchant every affected transaction has now, when the preview shows it: always for
 * the whole merchant, and for chosen transactions only when the preview lists them all.
 */
function sourceMerchant(
  preview: MerchantPreview | undefined,
  scope: EditScope,
): string | undefined {
  const [first, ...rest] = preview?.transactions ?? []
  if (!preview || !first) return undefined
  if (scope === 'transactions' && rest.length + 1 < preview.affected_transactions) return undefined
  return rest.every((row) => row.merchant === first.merchant) ? first.merchant : undefined
}

/** The Stage button's text, with the affected count once it is known. */
export function stageLabel(affected: number | undefined): string {
  return affected === undefined ? 'Stage change' : `Stage change to ${transactionCount(affected)}`
}

/** The choice whose label is the typed text, ignoring case and surrounding space. */
export function matchChoice(choices: EditorChoice[], text: string): EditorChoice | undefined {
  const needle = text.trim().toLowerCase()
  if (!needle) return undefined
  return choices.find((choice) => choice.label.trim().toLowerCase() === needle)
}

/** What the editor stages. */
export interface StagedEdit {
  kind: EditorKind
  target: { kind: string; identity: string }
  text: string
  /** The merchant scope; category edits always change the targeted transactions. */
  scope: EditScope | ''
  /** The existing merchant or category the text names. */
  match?: EditorChoice | undefined
  /** The group of a new category. */
  groupId?: string | undefined
}

/**
 * The mutation for a draft. A merchant rename onto an existing label merges into it; a new label
 * for selected transactions gets a fresh merchant ID, while a whole-merchant rename keeps its
 * merchant. A new category gets a fresh ID in the chosen group.
 */
export function editorIntent(draft: StagedEdit): MutationIntent {
  const label = draft.text.trim()
  if (draft.kind === 'merchant') {
    const scope = draft.scope || 'transactions'
    const fresh = scope === 'transactions' ? `merchant_${crypto.randomUUID()}` : ''
    const destination = draft.match?.id ?? fresh
    return {
      action: 'transaction.edit-merchant',
      target: draft.target,
      input: { scope, label, ...(destination ? { destination_id: destination } : {}) },
    }
  }
  const input = draft.match
    ? { scope: 'transactions', destination_id: draft.match.id }
    : {
        scope: 'transactions',
        destination_id: `category_${crypto.randomUUID()}`,
        label,
        group_id: draft.groupId ?? '',
      }
  return { action: 'transaction.edit-category', target: draft.target, input }
}
