import type { EditorCatalog } from '../api/client'

export type EditorChoice = NonNullable<EditorCatalog['merchants']>[number]

/** One row of the editor's list: an existing value, or `Create "…"` for new text. */
export interface EditorOption {
  /** The choice ID; empty for a create option. */
  id: string
  label: string
  /** The category's group label; empty for merchants and create options. */
  secondary: string
  /** Committed transactions across the whole profile, hidden ones included. */
  count: number
  create: boolean
}

/**
 * Filters editor choices by the typed text and offers `Create "…"` when it names no existing
 * value.
 *
 * Matching is a case-insensitive substring of the label, in catalog order. `Create "…"` comes
 * last and appears only when `create` is on, the trimmed text is not blank, and no label equals
 * it case-insensitively.
 *
 * @param choices Catalog merchants or categories.
 * @param query The combobox text.
 * @param create Whether new values may be created.
 * @param groups Catalog groups, which label each category's `parent_id` as secondary text.
 */
export function editorOptions(
  choices: EditorChoice[],
  query: string,
  create: boolean,
  groups: EditorChoice[] = [],
): EditorOption[] {
  const text = query.trim()
  const needle = text.toLocaleLowerCase()
  const groupLabels = new Map(groups.map((group) => [group.id, group.label]))
  const options: EditorOption[] = []
  let exact = false
  for (const choice of choices) {
    const label = choice.label.toLocaleLowerCase()
    if (!label.includes(needle)) continue
    exact ||= label.trim() === needle
    options.push({
      id: choice.id,
      label: choice.label,
      secondary: choice.parent_id ? (groupLabels.get(choice.parent_id) ?? '') : '',
      count: choice.transaction_count,
      create: false,
    })
  }
  if (create && text && !exact) {
    options.push({ id: '', label: text, secondary: '', count: 0, create: true })
  }
  return options
}
