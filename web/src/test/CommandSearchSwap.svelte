<script lang="ts">
  import CommandSearch from '../components/shell/CommandSearch.svelte'
  import type { SearchCarry } from '../lib/shell/search-carry'

  type Props = Pick<import('svelte').ComponentProps<typeof CommandSearch>, 'controller'> & {
    carry: SearchCarry
    /** Which of two fields shows, as the command bar swaps fields across the phone width. */
    wide: boolean
  }

  let { controller, carry, wide }: Props = $props()
</script>

{#if wide}
  <CommandSearch {controller} {carry} ondone={() => undefined} />
{:else}
  <CommandSearch {controller} {carry} ondone={() => undefined} />
{/if}
