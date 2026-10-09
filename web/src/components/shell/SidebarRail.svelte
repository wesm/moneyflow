<script lang="ts">
  import { IconButton, ThemeToggle, Tooltip } from '@kenn-io/kit-ui'
  import PenLineIcon from '@lucide/svelte/icons/pen-line'

  import ProviderStatus from '../ProviderStatus.svelte'
  import type { PendingSummary, ViewProjection } from '../../lib/api/client'
  import type { ProviderController } from '../../lib/controller/provider'
  import { navIcons } from '../../lib/shell/nav-icons'
  import { navItems, type NavItem, type NavRun } from '../../lib/shell/nav-items'
  import { pendingText } from '../../lib/shell/pending'

  interface Props {
    projection: ViewProjection
    pending: PendingSummary
    provider: ProviderController
    writing?: boolean
    onrun: (run: NavRun) => void
    onreview: () => void
    onreconnect?: (() => void) | undefined
  }

  let {
    projection,
    pending,
    provider,
    writing = false,
    onrun,
    onreview,
    onreconnect,
  }: Props = $props()

  const sections = $derived(navItems(projection))
  const reviewAvailable = $derived(
    projection.capabilities?.some(({ id, available }) => id === 'changes.review' && available) ??
      false,
  )

  // kit IconButton has no aria-current prop; mark the current entry on its button.
  function current(active: boolean) {
    return (item: HTMLElement) => {
      const button = item.querySelector('button')
      if (active) button?.setAttribute('aria-current', 'page')
      else button?.removeAttribute('aria-current')
    }
  }
</script>

{#snippet entries(items: NavItem[])}
  <ul class="mf-rail__list">
    {#each items as item (item.id)}
      {@const Icon = navIcons[item.id]}
      <li {@attach current(item.active)}>
        <Tooltip text="{item.label} · {item.key}">
          <IconButton
            ariaLabel={item.label}
            title=""
            class={item.active ? 'mf-rail__item mf-rail__item--active' : 'mf-rail__item'}
            disabled={!item.available}
            onclick={() => onrun(item.run)}
          >
            {#if Icon}<Icon size={16} aria-hidden="true" />{/if}
          </IconButton>
        </Tooltip>
      </li>
    {/each}
  </ul>
{/snippet}

<aside class="mf-rail" aria-label="Moneyflow sidebar">
  <nav aria-label="Workspace">
    {@render entries(sections.explore)}
    <hr class="mf-rail__rule" />
    {@render entries(sections.maintain)}
  </nav>
  <ul class="mf-rail__list mf-rail__footer">
    <li class="mf-rail__pending">
      <Tooltip text="{pendingText(pending)} · w">
        <IconButton
          ariaLabel="Review pending changes"
          title=""
          class="mf-rail__item"
          disabled={!reviewAvailable && !writing}
          onclick={onreview}
        >
          <PenLineIcon size={16} aria-hidden="true" />
        </IconButton>
      </Tooltip>
      {#if pending.active_operations > 0}
        <span class="mf-rail__count" aria-hidden="true">{pending.active_operations}</span>
      {/if}
    </li>
    {#if projection.profile_kind !== 'amazon'}
      <li><ProviderStatus controller={provider} {onreconnect} rail /></li>
    {/if}
    <li><ThemeToggle size="md" /></li>
  </ul>
</aside>
