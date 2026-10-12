<script lang="ts">
  import { IconButton, SidebarToggle, ThemeToggle, Tooltip } from '@kenn-io/kit-ui'
  import PenLineIcon from '@lucide/svelte/icons/pen-line'

  import ProviderStatus from '../ProviderStatus.svelte'
  import type { PendingSummary, ViewProjection } from '../../lib/api/client'
  import type { ProviderController } from '../../lib/controller/provider'
  import { navIcons } from '../../lib/shell/nav-icons'
  import { availableCapabilities, unavailableReason } from '../../lib/shell/capabilities'
  import { focusClicked } from '../../lib/shell/focus'
  import {
    ariaKeyshortcuts,
    hasProviderStatus,
    navItems,
    type NavItem,
    type NavRun,
  } from '../../lib/shell/nav-items'
  import { sidebarState, toggleExpanded } from '../../lib/shell/sidebar-state.svelte'
  import type { WriteProgress } from '../../lib/shell/status.svelte'

  interface Props {
    projection: ViewProjection
    pending: PendingSummary
    provider: ProviderController
    write?: WriteProgress | undefined
    onrun: (run: NavRun) => void
    onreview: () => void
    onreconnect?: (() => void) | undefined
  }

  let { projection, pending, provider, write, onrun, onreview, onreconnect }: Props = $props()

  const sections = $derived(navItems(projection))
  const layout = sidebarState()
  const reviewAvailable = $derived(availableCapabilities(projection).has('changes.review'))
  const waiting = $derived(pending.active_operations > 0)
  // Like the sidebar card: review needs pending edits; a running write always opens its drawer.
  const reviewOffered = $derived(write !== undefined || (waiting && reviewAvailable))
  const pendingTip = $derived.by(() => {
    if (write) return `Write ${write.completed}/${write.total} · w`
    if (!waiting) return 'No pending edits'
    if (!reviewAvailable) {
      return `Review pending changes · ${unavailableReason(projection, 'changes.review')}`
    }
    return 'Review pending changes · w'
  })

  // kit IconButton has no aria-current or aria-keyshortcuts prop; set them on its button.
  function entryAttributes(item: NavItem) {
    return (node: HTMLElement) => {
      const button = node.querySelector('button')
      button?.setAttribute('aria-keyshortcuts', ariaKeyshortcuts(item.key))
      if (item.active) button?.setAttribute('aria-current', 'page')
      else button?.removeAttribute('aria-current')
    }
  }
</script>

{#snippet entries(items: NavItem[])}
  <ul class="mf-rail__list">
    {#each items as item (item.id)}
      {@const Icon = navIcons[item.id]}
      <li {@attach entryAttributes(item)}>
        <Tooltip text="{item.label} · {item.reason ?? item.key}">
          <IconButton
            ariaLabel={item.label}
            title=""
            class={item.active ? 'mf-rail__item mf-rail__item--active' : 'mf-rail__item'}
            ariaDisabled={!item.available}
            onclick={(event) => {
              focusClicked(event)
              onrun(item.run)
            }}
          >
            {#if Icon}<Icon size={16} aria-hidden="true" />{/if}
          </IconButton>
        </Tooltip>
      </li>
    {/each}
  </ul>
{/snippet}

<aside class="mf-rail" aria-label="Moneyflow sidebar">
  {#if layout?.wide}
    <span class="mf-collapse-control" {@attach toggleExpanded(false)}>
      <SidebarToggle state="collapsed" onclick={() => void layout.setCollapsed(false)} />
    </span>
    <hr class="mf-rail__rule" />
  {/if}
  <nav aria-label="Workspace">
    {@render entries(sections.explore)}
    <hr class="mf-rail__rule" />
    {@render entries(sections.maintain)}
  </nav>
  <ul class="mf-rail__list mf-rail__footer">
    <li class="mf-rail__pending">
      <Tooltip text={pendingTip}>
        <IconButton
          ariaLabel="Review pending changes"
          title=""
          class="mf-rail__item"
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
</aside>
