import { createShortcutManager, type ShortcutManager } from '@kenn-io/kit-ui'
import type { ViewProjection } from './api/client'

type Capability = NonNullable<ViewProjection['capabilities']>[number]
export type LocalAction =
  | 'cursor.up'
  | 'cursor.down'
  | 'cursor.home'
  | 'overlay.filters'
  | 'overlay.search'
  | 'overlay.help'
  | 'edit.merchant'
  | 'edit.category'
  | 'manage.categories'
  | 'manage.groups'
  | 'edit.hide'
  | 'edit.undo'
  | 'edit.redo'
  | 'edit.review'
  | 'view.duplicates'
  | 'transactions.export'
  | 'edit.delete'
  | 'transaction.info'
  | 'provider.refresh'

const bindings = [
  { id: 'cursor.up', display: '↑/k', combos: ['arrowup', 'k'], local: true },
  { id: 'cursor.down', display: '↓/j', combos: ['arrowdown', 'j'], local: true },
  { id: 'cursor.home', display: 'home', combos: ['home'], local: true },
  { id: 'view.cycle-grouping', display: 'g', combos: ['g'] },
  { id: 'view.show-detail', display: 'd', combos: ['d'] },
  {
    id: 'view.find-duplicates',
    display: 'D',
    combos: ['shift+d'],
    local: true,
    localID: 'view.duplicates',
  },
  { id: 'view.switch-accounts', display: 'A', combos: ['shift+a'] },
  { id: 'view.drill', display: 'enter', combos: ['enter'] },
  { id: 'view.back', display: 'esc', combos: ['escape'] },
  { id: 'time.toggle-granularity', display: 't', combos: ['t'] },
  { id: 'time.clear-period', display: 'a', combos: ['a'] },
  { id: 'time.previous-period', display: '←', combos: ['arrowleft'] },
  { id: 'time.next-period', display: '→', combos: ['arrowright'] },
  { id: 'sort.cycle', display: 's', combos: ['s'] },
  { id: 'sort.reverse', display: 'v', combos: ['v'] },
  { id: 'selection.toggle', display: 'space', combos: ['space'] },
  { id: 'selection.toggle-all', display: 'ctrl+a', combos: ['ctrl+a'] },
  {
    id: 'transaction.show-info',
    display: 'i',
    combos: ['i'],
    local: true,
    localID: 'transaction.info',
  },
  { id: 'overlay.filters', display: 'f', combos: ['f'], local: true },
  { id: 'overlay.search', display: '/', combos: ['/'], local: true },
  { id: 'overlay.help', display: '?', combos: ['?'], local: true },
  {
    id: 'transaction.edit-merchant',
    display: 'm',
    combos: ['m'],
    local: true,
    localID: 'edit.merchant',
  },
  {
    id: 'transaction.edit-category',
    display: 'c',
    combos: ['c'],
    local: true,
    localID: 'edit.category',
  },
  {
    id: 'category.manage',
    display: 'C',
    combos: ['shift+c'],
    local: true,
    localID: 'manage.categories',
  },
  {
    id: 'category-group.manage',
    display: 'G',
    combos: ['shift+g'],
    local: true,
    localID: 'manage.groups',
  },
  {
    id: 'transaction.toggle-hidden',
    display: 'h',
    combos: ['h'],
    local: true,
    localID: 'edit.hide',
  },
  {
    id: 'transaction.delete',
    display: 'x',
    combos: ['x'],
    local: true,
    localID: 'edit.delete',
  },
  { id: 'changes.undo', display: 'u', combos: ['u'], local: true, localID: 'edit.undo' },
  { id: 'changes.redo', display: 'U', combos: ['shift+u'], local: true, localID: 'edit.redo' },
  { id: 'changes.review', display: 'w', combos: ['w'], local: true, localID: 'edit.review' },
  {
    id: 'provider.refresh',
    display: 'r',
    combos: ['r'],
    local: true,
    localID: 'provider.refresh',
  },
  {
    id: 'transactions.export',
    display: 'E',
    combos: ['shift+e'],
    local: true,
  },
] as const

export function validateCapabilities(capabilities: Capability[]): void {
  const byID = new Map(capabilities.map((capability) => [capability.id, capability]))
  for (const binding of bindings) {
    const capability = byID.get(binding.id)
    if (capability && capability.key_display !== binding.display) {
      throw new Error(`server capability ${binding.id} conflicts with the browser shortcut table`)
    }
  }
}

export function createMoneyflowShortcuts(
  capabilities: Capability[],
  handlers: { local(action: LocalAction): void; apply(action: string): void },
): { manager: ShortcutManager; destroy(): void } {
  validateCapabilities(capabilities)
  const available = new Set(
    capabilities.filter((capability) => capability.available).map((capability) => capability.id),
  )
  const manager = createShortcutManager()
  const cleanup: Array<() => void> = []
  for (const binding of bindings) {
    if (!available.has(binding.id)) continue
    for (const combo of binding.combos) {
      cleanup.push(
        manager.register(combo, () => {
          if ('local' in binding) {
            handlers.local(('localID' in binding ? binding.localID : binding.id) as LocalAction)
          } else handlers.apply(binding.id)
        }),
      )
    }
  }
  return { manager, destroy: () => cleanup.forEach((remove) => remove()) }
}

export function handleMoneyflowKeydown(manager: ShortcutManager, event: KeyboardEvent): boolean {
  const target = event.target
  if (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement ||
    (target instanceof HTMLElement && target.isContentEditable)
  ) {
    return false
  }
  return manager.handleKeydown(event)
}
