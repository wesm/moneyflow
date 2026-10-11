<script lang="ts">
  import { KbdBadge, ThemeToggle } from '@kenn-io/kit-ui'

  import PendingCard from './PendingCard.svelte'
  import ProviderStatus from '../ProviderStatus.svelte'
  import type { PendingSummary, ViewProjection } from '../../lib/api/client'
  import type { ProviderController } from '../../lib/controller/provider'
  import { navIcons } from '../../lib/shell/nav-icons'
  import { availableCapabilities } from '../../lib/shell/capabilities'
  import { focusClicked } from '../../lib/shell/focus'
  import {
    ariaKeyshortcuts,
    hasProviderStatus,
    navItems,
    type NavItem,
    type NavRun,
  } from '../../lib/shell/nav-items'

  interface Props {
    projection: ViewProjection
    pending: PendingSummary
    write?: { completed: number; total: number } | undefined
    provider: ProviderController
    onrun: (run: NavRun) => void
    onreview: () => void
    onwrite: () => void
    onreconnect?: (() => void) | undefined
  }

  let { projection, pending, write, provider, onrun, onreview, onwrite, onreconnect }: Props =
    $props()

  const providerNames: Record<ViewProjection['profile_kind'], string> = {
    monarch: 'Monarch',
    ynab: 'YNAB',
    simplefin: 'SimpleFIN',
    amazon: 'Amazon',
    local: 'Local',
    csv: 'CSV',
  }
  const sections = $derived(navItems(projection))
  const reviewAvailable = $derived(availableCapabilities(projection).has('changes.review'))

  // kit CollapsibleSidebar renders the <aside> without a name; name it apart from Visualizations.
  function nameLandmark(node: HTMLElement): void {
    node.closest('aside')?.setAttribute('aria-label', 'Moneyflow sidebar')
  }
</script>

{#snippet section(title: string, items: NavItem[])}
  <p class="mf-sidebar__label" id="mf-sidebar-{title.toLowerCase()}">{title}</p>
  <ul class="mf-sidebar__list" aria-labelledby="mf-sidebar-{title.toLowerCase()}">
    {#each items as item (item.id)}
      {@const Icon = navIcons[item.id]}
      <li>
        <button
          type="button"
          class="mf-nav-item"
          aria-current={item.active ? 'page' : undefined}
          aria-keyshortcuts={ariaKeyshortcuts(item.key)}
          aria-disabled={item.available ? undefined : 'true'}
          title={item.reason}
          onclick={(event) => {
            if (!item.available) return
            focusClicked(event)
            onrun(item.run)
          }}
        >
          {#if Icon}<Icon size={14} aria-hidden="true" />{/if}
          <span class="mf-nav-item__label">{item.label}</span>
          <span class="mf-nav-item__key" aria-hidden="true"><KbdBadge keys={[item.key]} /></span>
        </button>
      </li>
    {/each}
  </ul>
{/snippet}

<div class="mf-sidebar" {@attach nameLandmark}>
  <div class="mf-sidebar__brand">
    <span class="moneyflow-brand">Moneyflow</span>
    <span class="mf-sidebar__provider">{providerNames[projection.profile_kind]}</span>
  </div>
  <nav class="mf-sidebar__nav" aria-label="Workspace">
    {@render section('Explore', sections.explore)}
    {@render section('Maintain', sections.maintain)}
  </nav>
  <div class="mf-sidebar__footer">
    <PendingCard {pending} {reviewAvailable} {write} {onreview} {onwrite} />
    <div class="mf-sidebar__system">
      {#if hasProviderStatus(projection.profile_kind)}
        <ProviderStatus controller={provider} {onreconnect} />
      {/if}
      <ThemeToggle size="sm" class="mf-sidebar__theme" />
    </div>
  </div>
</div>
