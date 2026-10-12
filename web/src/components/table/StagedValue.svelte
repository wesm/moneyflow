<script lang="ts">
  interface Props {
    /** The committed value the row shows. */
    committed: string
    /** The value after commit, when a pending edit changes it. */
    staged?: string | undefined
  }

  let { committed, staged }: Props = $props()
</script>

{#if staged === undefined || staged === committed}
  {committed}
{:else}
  <span class="mf-staged"><s>{committed}</s> → {staged}</span>
{/if}
