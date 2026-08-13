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

const bindings = [
  { id: 'cursor.up', display: '↑/k', combos: ['arrowup', 'k'], local: true },
  { id: 'cursor.down', display: '↓/j', combos: ['arrowdown', 'j'], local: true },
  { id: 'cursor.home', display: 'home', combos: ['home'], local: true },
  { id: 'view.cycle-grouping', display: 'g', combos: ['g'] },
  { id: 'view.show-detail', display: 'd', combos: ['d'] },
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
  { id: 'overlay.filters', display: 'f', combos: ['f'], local: true },
  { id: 'overlay.search', display: '/', combos: ['/'], local: true },
  { id: 'overlay.help', display: '?', combos: ['?'], local: true },
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
  const available = new Set(capabilities.map((capability) => capability.id))
  const manager = createShortcutManager()
  const cleanup: Array<() => void> = []
  for (const binding of bindings) {
    if (!available.has(binding.id)) continue
    for (const combo of binding.combos) {
      cleanup.push(
        manager.register(combo, () => {
          if ('local' in binding) handlers.local(binding.id as LocalAction)
          else handlers.apply(binding.id)
        }),
      )
    }
  }
  return { manager, destroy: () => cleanup.forEach((remove) => remove()) }
}
