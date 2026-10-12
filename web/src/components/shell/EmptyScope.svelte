<script lang="ts">
  import { Button, EmptyState } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import type { EmptyScope } from '../../lib/shell/period'

  interface Props {
    scope: EmptyScope
    onjump: () => void
  }

  let { scope, onjump }: Props = $props()
  let body = $state<HTMLElement>()

  // The table this replaces may have held focus; keep it on the page instead of the body.
  onMount(() => {
    const active = document.activeElement
    if (active === null || active === document.body) body?.querySelector('button')?.focus()
  })
</script>

<div class="mf-empty-scope" bind:this={body}>
  <EmptyState title="No transactions in {scope.label}">
    <Button size="sm" onclick={onjump}>Jump to latest</Button>
  </EmptyState>
</div>
