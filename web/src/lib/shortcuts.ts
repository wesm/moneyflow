import { createShortcutManager, type ShortcutManager } from '@kenn-io/kit-ui'
import type { TransitionBody, ViewProjection } from './api/client'
import { status } from './shell/status.svelte'

export type Dimension = NonNullable<TransitionBody['dimension']>
type Capability = NonNullable<ViewProjection['capabilities']>[number]
export type LocalAction =
  | 'cursor.up'
  | 'cursor.down'
  | 'cursor.home'
  | 'cursor.end'
  | 'cursor.page-up'
  | 'cursor.page-down'
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
  | 'time.choose'

const bindings = [
  { id: 'cursor.up', display: '↑/k', combos: ['arrowup', 'k'], local: true },
  { id: 'cursor.down', display: '↓/j', combos: ['arrowdown', 'j'], local: true },
  { id: 'cursor.home', display: 'Home/T', combos: ['home', 'shift+t'], local: true },
  { id: 'cursor.end', display: 'End/B', combos: ['end', 'shift+b'], local: true },
  { id: 'cursor.page-up', display: 'PgUp', combos: ['pageup'], local: true },
  { id: 'cursor.page-down', display: 'PgDn', combos: ['pagedown'], local: true },
  { id: 'view.cycle-grouping', display: 'g', combos: ['g'] },
  {
    id: 'view.set-grouping',
    display: '1-5',
    combos: ['1', '2', '3', '4', '5'],
    dimensions: ['merchant', 'category', 'group', 'account', 'time'],
  },
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
  { id: 'time.clear-period', display: 'a', combos: ['a'] },
  { id: 'time.previous-period', display: '←', combos: ['arrowleft'] },
  { id: 'time.next-period', display: '→', combos: ['arrowright'] },
  { id: 'time.choose', display: 't', combos: ['t'], local: true },
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
    /** While delete is unavailable, `x` shows the reason instead of doing nothing. */
    explains: true,
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
  const bound = new Set<string>(bindings.map((binding) => binding.id))
  for (const capability of capabilities) {
    if (capability.key_display && !bound.has(capability.id)) {
      throw new Error(`server capability ${capability.id} has no browser shortcut`)
    }
  }
}

interface ShortcutHandlers {
  local(action: LocalAction): void
  apply(action: string, dimension?: Dimension): void
}

/** What a binding's key does under the capability, or nothing while it is unavailable. */
function bindingAction(
  binding: (typeof bindings)[number],
  capability: Capability | undefined,
  handlers: ShortcutHandlers,
  index: number,
): (() => void) | undefined {
  if (!capability) return undefined
  if (!capability.available) {
    const reason = capability.reason
    return 'explains' in binding && reason ? () => status.notify(reason) : undefined
  }
  if ('local' in binding) {
    const action = ('localID' in binding ? binding.localID : binding.id) as LocalAction
    return () => handlers.local(action)
  }
  if ('dimensions' in binding) return () => handlers.apply(binding.id, binding.dimensions[index])
  return () => handlers.apply(binding.id)
}

/** Binds the shortcuts the capabilities allow. Each key first clears the status notice. */
export function createMoneyflowShortcuts(
  capabilities: Capability[],
  handlers: ShortcutHandlers,
): { manager: ShortcutManager; destroy(): void } {
  validateCapabilities(capabilities)
  const byID = new Map(capabilities.map((capability) => [capability.id, capability]))
  const manager = createShortcutManager()
  const cleanup: Array<() => void> = []
  for (const binding of bindings) {
    for (const [index, combo] of binding.combos.entries()) {
      const action = bindingAction(binding, byID.get(binding.id), handlers, index)
      if (!action) continue
      cleanup.push(
        manager.register(combo, () => {
          status.clear()
          action()
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
  // A focused button activates with Enter or Space; those keys must not also drill or select.
  if (target instanceof HTMLButtonElement && (event.key === 'Enter' || event.key === ' ')) {
    return false
  }
  // A control that handled the key claims it: the Period segments move with the arrows, and a
  // popover closes on Escape. The same key must not also shift the period or go back.
  if (event.defaultPrevented) return false
  // A held Enter that confirmed or committed in an overlay must not drill once focus returns.
  if (event.key === 'Enter' && event.repeat) return false
  return manager.handleKeydown(event)
}
