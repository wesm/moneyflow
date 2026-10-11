import { getContext, setContext, tick } from 'svelte'
import { MediaQuery } from 'svelte/reactivity'

import { SIDEBAR_FULL_QUERY } from './layout'

const storageKey = 'moneyflow-sidebar'
const contextKey = Symbol('moneyflow-sidebar')

/**
 * Tracks the sidebar collapse control. Where the window is wide enough for the full sidebar, the
 * viewer can collapse it to the icon rail, and the choice persists in this browser. Narrower
 * windows show the rail or the phone drawer whatever the choice.
 */
export class SidebarState {
  #collapsed = $state(readCollapsed())
  #wide: { readonly current: boolean }

  /** @param wide Whether the window is wide enough for the full sidebar. */
  constructor(wide: { readonly current: boolean } = new MediaQuery(SIDEBAR_FULL_QUERY)) {
    this.#wide = wide
  }

  /** Whether the window is wide enough for the full sidebar, so the collapse control shows. */
  get wide(): boolean {
    return this.#wide.current
  }

  /** Whether the full sidebar shows. */
  get full(): boolean {
    return this.#wide.current && !this.#collapsed
  }

  /** Collapses or expands the sidebar, then focuses the control that undoes it. */
  async setCollapsed(collapsed: boolean): Promise<void> {
    this.#collapsed = collapsed
    writeCollapsed(collapsed)
    await tick()
    document.querySelector<HTMLElement>('.mf-collapse-control button')?.focus()
  }
}

/**
 * Gives the kit `SidebarToggle` inside the node its `aria-expanded` state, which the toggle has
 * no prop for (kit-ui upstream gap). An Attachment factory.
 */
export function toggleExpanded(expanded: boolean): (node: HTMLElement) => void {
  return (node) => node.querySelector('button')?.setAttribute('aria-expanded', String(expanded))
}

/** Creates the shell's sidebar state and shares it with the sidebar and the rail. */
export function provideSidebarState(): SidebarState {
  return setContext(contextKey, new SidebarState())
}

/** The shell's sidebar state, or undefined outside the shell. */
export function sidebarState(): SidebarState | undefined {
  return getContext<SidebarState | undefined>(contextKey)
}

// Browsers can refuse storage (private windows, blocked site data); the sidebar then starts full
// and forgets the choice on reload, which is all it can do.
function readCollapsed(): boolean {
  try {
    return localStorage.getItem(storageKey) === 'collapsed'
  } catch {
    return false
  }
}

function writeCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(storageKey, collapsed ? 'collapsed' : 'full')
  } catch {
    // See readCollapsed: the choice lasts until the page reloads.
  }
}
