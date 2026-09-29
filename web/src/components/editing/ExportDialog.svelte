<script lang="ts">
  import { Button, Modal, SegmentedControl } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import type { ExportController, ExportFormat, ExportScope } from '../../lib/controller/export'

  interface Props {
    controller: ExportController
    onclose: () => void
  }

  let { controller, onclose }: Props = $props()
  let container = $state<HTMLDivElement | undefined>()
  const exportState = $derived(controller.state)
  const running = $derived(exportState.phase === 'exporting')

  const formats = [
    { value: 'parquet', label: 'Parquet' },
    { value: 'csv', label: 'CSV' },
    { value: 'sqlite', label: 'SQLite' },
  ]
  const scopes = [
    { value: 'full', label: 'Full' },
    { value: 'filtered', label: 'Filtered' },
  ]

  onMount(() => container?.querySelector<HTMLButtonElement>('[role="radio"]')?.focus())

  function close(): void {
    if (running && !exportState.canCancel) return
    if (running) {
      controller.cancel()
      return
    }
    controller.close()
    onclose()
  }

  async function execute(): Promise<void> {
    if (running || exportState.count === 0) return
    await controller.export()
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (event.repeat || event.altKey || event.ctrlKey || event.metaKey) return
    if (event.key === 'Escape') {
      if (running && !exportState.canCancel) return
      event.preventDefault()
      close()
      return
    }
    if (
      event.key === 'Enter' &&
      !(event.target instanceof HTMLButtonElement) &&
      !running &&
      exportState.count > 0
    ) {
      event.preventDefault()
      void execute()
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<Modal title="Export transactions" closeLabel="Cancel export" onclose={close}>
  <div class="export-dialog" bind:this={container}>
    <fieldset>
      <legend class="editing-label">Format</legend>
      <SegmentedControl
        options={formats}
        value={exportState.format}
        onchange={(value) => controller.setFormat(value as ExportFormat)}
        ariaLabel="Export format"
        disabled={running}
        block
      />
    </fieldset>
    <fieldset>
      <legend class="editing-label">Scope</legend>
      <SegmentedControl
        options={scopes}
        value={exportState.scope}
        onchange={(value) => controller.setScope(value as ExportScope)}
        ariaLabel="Export scope"
        disabled={running}
        block
      />
    </fieldset>
    <p>{exportState.count} committed {exportState.count === 1 ? 'transaction' : 'transactions'}</p>
    <p class="export-dialog__estimate">The completed file records the authoritative row count.</p>
    {#if (exportState.preview?.active_operations ?? 0) > 0}
      <p role="note">
        {exportState.preview?.active_operations} pending
        {(exportState.preview?.active_operations ?? 0) === 1 ? 'operation is' : 'operations are'}
        excluded.{#if exportState.preview?.commit_available}
          Commit them first to include their changes.{/if}
      </p>
    {/if}
    {#if (exportState.preview?.inactive_operations ?? 0) > 0}
      <p role="note">
        {exportState.preview?.inactive_operations} inactive redo
        {(exportState.preview?.inactive_operations ?? 0) === 1 ? 'operation is' : 'operations are'}
        excluded.
      </p>
    {/if}
    {#if exportState.preview?.temporary_profile}
      <p role="note">This temporary profile will download the export through your browser.</p>
    {/if}
    {#if exportState.announcement}
      <p role="status" aria-live="polite">{exportState.announcement}</p>
    {/if}
  </div>
  {#snippet footer()}
    <Button type="button" disabled={running && !exportState.canCancel} onclick={close}
      >{running && exportState.canCancel ? 'Cancel export' : 'Close'}</Button
    ><Button
      type="button"
      tone="info"
      surface="solid"
      disabled={running || exportState.count === 0}
      onclick={() => void execute()}>{running ? 'Exporting…' : 'Export'}</Button
    >
  {/snippet}
</Modal>

<style>
  .export-dialog {
    display: grid;
    gap: var(--kit-space-3);
    min-inline-size: min(30rem, calc(100vw - 3rem));
  }
  .export-dialog fieldset {
    display: grid;
    gap: var(--kit-space-2);
    margin: 0;
    padding: 0;
    border: 0;
  }
  .export-dialog p {
    margin: 0;
  }
  .export-dialog__estimate {
    color: var(--kit-text-muted);
  }
</style>
