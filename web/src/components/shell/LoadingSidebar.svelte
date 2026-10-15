<script lang="ts">
  import { AppSidebar, ThemeToggle } from '@kenn-io/kit-ui'
</script>

<!--
  The sidebar until the first view arrives: the same frame at every width, with no entries yet,
  so the workspace keeps its place when the view fills it in.
-->

{#snippet brand({ rail }: { rail: boolean })}
  {#if !rail}
    <span class="mf-sidebar__brand"><span class="moneyflow-brand">Moneyflow</span></span>
  {/if}
{/snippet}

{#snippet narrowBar()}
  <span class="mf-sidebar__brand"><span class="moneyflow-brand">Moneyflow</span></span>
{/snippet}

{#snippet footer({ rail }: { rail: boolean; drawer: boolean })}
  {#if rail}
    <ul class="mf-rail__list">
      <li><ThemeToggle size="md" /></li>
    </ul>
  {:else}
    <div class="mf-sidebar__footer">
      <div class="mf-sidebar__system"><ThemeToggle size="sm" class="mf-sidebar__theme" /></div>
    </div>
  {/if}
{/snippet}

<div class="mf-sidebar">
  <AppSidebar
    groups={[]}
    location={{ section: null, view: null }}
    page=""
    storageKey="moneyflow-sidebar"
    label="Workspace"
    {brand}
    {narrowBar}
    {footer}
  />
</div>
