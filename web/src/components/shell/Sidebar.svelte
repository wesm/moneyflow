<script lang="ts">
  import {
    AppSidebar,
    IconButton,
    ThemeToggle,
    Tooltip,
    type SidebarGroup,
    type SidebarSection,
  } from '@kenn-io/kit-ui'
  import ListIcon from '@lucide/svelte/icons/list'
  import PenLineIcon from '@lucide/svelte/icons/pen-line'

  import PendingCard from './PendingCard.svelte'
  import ProviderStatus from '../ProviderStatus.svelte'
  import type { PendingSummary, ViewProjection } from '../../lib/api/client'
  import type { ProviderController } from '../../lib/controller/provider'
  import { availableCapabilities, unavailableReason } from '../../lib/shell/capabilities'
  import { focusClicked } from '../../lib/shell/focus'
  import { navIcons } from '../../lib/shell/nav-icons'
  import { hasProviderStatus, navItems, type NavItem, type NavRun } from '../../lib/shell/nav-items'
  import { status, type WriteProgress } from '../../lib/shell/status.svelte'

  interface Props {
    projection: ViewProjection
    pending: PendingSummary
    write?: WriteProgress | undefined
    provider: ProviderController
    onrun: (run: NavRun) => void
    onreview: () => void
    onwrite: () => void
    onreconnect?: (() => void) | undefined
    /**
     * Reports the full footer (the pending card) while it shows, in the sidebar or in the narrow
     * drawer, and `undefined` once it is gone.
     */
    onfooter?: ((footer: HTMLElement | undefined) => void) | undefined
  }

  let {
    projection,
    pending,
    write,
    provider,
    onrun,
    onreview,
    onwrite,
    onreconnect,
    onfooter,
  }: Props = $props()

  const providerNames: Record<ViewProjection['profile_kind'], string> = {
    monarch: 'Monarch',
    ynab: 'YNAB',
    simplefin: 'SimpleFIN',
    amazon: 'Amazon',
    local: 'Local',
    csv: 'CSV',
  }
  const items = $derived(navItems(projection))
  const groups = $derived<SidebarGroup[]>([
    { label: 'Explore', sections: items.explore.map(section) },
    { label: 'Maintain', sections: items.maintain.map(section) },
  ])
  const current = $derived(items.explore.find((item) => item.active)?.id ?? null)
  const reviewAvailable = $derived(availableCapabilities(projection).has('changes.review'))
  const waiting = $derived(pending.active_operations > 0)
  // Like the pending card: review needs pending edits; a running write always opens its drawer.
  const reviewOffered = $derived(write !== undefined || (waiting && reviewAvailable))
  const pendingTip = $derived.by(() => {
    if (write) return `Write ${write.completed}/${write.total} · w`
    if (!waiting) return 'No pending edits'
    if (!reviewAvailable) {
      return `Review pending changes · ${unavailableReason(projection, 'changes.review')}`
    }
    return 'Review pending changes · w'
  })

  function section(item: NavItem): SidebarSection {
    return {
      id: item.id,
      label: item.label,
      href: `#${item.id}`,
      icon: navIcons[item.id] ?? ListIcon,
      viewRuns: [],
    }
  }

  /**
   * Entries run view actions in place; their links only give them a target. An unavailable entry
   * says why in the status line. Focus moves to the entry first, as WebKit does not focus a
   * clicked link.
   */
  function entryClicked(event: MouseEvent): void {
    const link = event.target instanceof Element ? event.target.closest('a') : null
    const all = [...items.explore, ...items.maintain]
    const item = all.find((entry) => link?.getAttribute('href') === `#${entry.id}`)
    if (!link || !item) return
    event.preventDefault()
    if (!item.available) {
      if (item.reason) status.notify(item.reason)
      return
    }
    link.focus({ preventScroll: true })
    onrun(item.run)
  }

  let reported: HTMLElement | undefined

  function footerShown(footer: HTMLElement): () => void {
    reported = footer
    onfooter?.(footer)
    return () => {
      // Crossing a width band can show the next footer before this one goes.
      if (reported !== footer) return
      reported = undefined
      onfooter?.(undefined)
    }
  }
</script>

{#snippet brand({ rail }: { rail: boolean })}
  {#if !rail}
    <span class="mf-sidebar__brand">
      <span class="moneyflow-brand">Moneyflow</span>
      <span class="mf-sidebar__provider">{providerNames[projection.profile_kind]}</span>
    </span>
  {/if}
{/snippet}

{#snippet narrowBar()}
  <span class="mf-sidebar__brand">
    <span class="moneyflow-brand">Moneyflow</span>
    <span class="mf-sidebar__provider">{providerNames[projection.profile_kind]}</span>
  </span>
{/snippet}

{#snippet footer({ rail, drawer }: { rail: boolean; drawer: boolean })}
  {#if rail}
    <ul class="mf-rail__list">
      <li class="mf-rail__pending">
        <Tooltip text={pendingTip}>
          <IconButton
            ariaLabel="Review pending changes"
            title=""
            ariaDisabled={!reviewOffered}
            onclick={(event) => {
              focusClicked(event)
              onreview()
            }}
          >
            <PenLineIcon size={16} aria-hidden="true" />
          </IconButton>
        </Tooltip>
        {#if waiting && reviewOffered}
          <span class="mf-rail__count" aria-hidden="true">{pending.active_operations}</span>
        {/if}
      </li>
      {#if hasProviderStatus(projection.profile_kind)}
        <li><ProviderStatus controller={provider} {onreconnect} rail /></li>
      {/if}
      <li><ThemeToggle size="md" /></li>
    </ul>
  {:else}
    <div
      class={['mf-sidebar__footer', drawer && 'mf-sidebar__footer--drawer']}
      {@attach footerShown}
    >
      <PendingCard {pending} {reviewAvailable} {write} {onreview} {onwrite} />
      <div class="mf-sidebar__system">
        {#if hasProviderStatus(projection.profile_kind)}
          <ProviderStatus controller={provider} {onreconnect} />
        {/if}
        <ThemeToggle size="sm" class="mf-sidebar__theme" />
      </div>
    </div>
  {/if}
{/snippet}

<!-- The entries are links: Enter clicks them, and this handler sees that click. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="mf-sidebar" onclick={entryClicked}>
  <AppSidebar
    {groups}
    location={{ section: current, view: null }}
    page={projection.canonical_query}
    storageKey="moneyflow-sidebar"
    label="Workspace"
    {brand}
    {narrowBar}
    {footer}
  />
</div>
