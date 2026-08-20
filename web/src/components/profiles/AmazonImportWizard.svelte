<script lang="ts">
  import { Button, Spinner, StatusBar, TextInput, ThemeToggle, TopBar } from '@kenn-io/kit-ui'

  import type { AmazonImportController } from '../../lib/controller/amazon-import.svelte'

  interface Props {
    controller: AmazonImportController
    initialCurrency?: string
    initialScale?: number
    oncomplete: () => void
    oncancel: () => void
  }

  let {
    controller,
    initialCurrency = 'USD',
    initialScale = 2,
    oncomplete,
    oncancel,
  }: Props = $props()
  let currency = $state('')
  let scale = $state('')
  let initialized = false
  let taxonomySource = $state('')
  let advanced = $state(false)
  let sourceMode = $state<'files' | 'directory'>('files')
  let selectedFiles = $state<File[]>([])
  let localProblem = $state('')

  $effect(() => {
    if (initialized) return
    currency = initialCurrency
    scale = initialScale.toString(10)
    initialized = true
  })

  async function confirmSettings(): Promise<void> {
    const parsedScale = Number.parseInt(scale, 10)
    if (
      !/^[A-Za-z]{3}$/.test(currency.trim()) ||
      !Number.isInteger(parsedScale) ||
      parsedScale < 0 ||
      parsedScale > 9
    ) {
      localProblem = 'Currency must have three letters and scale must be between 0 and 9.'
      return
    }
    localProblem = ''
    await controller.start(currency, parsedScale, taxonomySource)
  }

  async function importFiles(): Promise<void> {
    if (selectedFiles.length === 0) {
      localProblem = 'Choose at least one Amazon order-history CSV file.'
      return
    }
    localProblem = ''
    if (!(await controller.upload(selectedFiles))) return
    await controller.execute()
  }

  async function cancel(): Promise<void> {
    if (await controller.cancel()) oncancel()
  }
</script>

<div class="moneyflow-app onboarding-wizard amazon-import-wizard">
  <TopBar ariaLabel="Amazon import">
    {#snippet left()}<span class="moneyflow-brand">Moneyflow</span>{/snippet}
    {#snippet right()}<ThemeToggle size="sm" />{/snippet}
  </TopBar>
  <main class="profile-main" aria-label="Amazon order-history import">
    <section class="profile-panel" aria-labelledby="amazon-import-title">
      <p class="moneyflow-eyebrow">Amazon orders</p>
      {#if controller.state.phase === 'settings'}
        <h1 id="amazon-import-title">Confirm import settings</h1>
        <p>
          Moneyflow stores exact integer minor units. Currency and scale cannot change after the
          first import.
        </p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void confirmSettings()
          }}
        >
          <label for="amazon-currency">Currency</label>
          <TextInput id="amazon-currency" bind:value={currency} block autocomplete="off" />
          <label for="amazon-scale">Minor-unit scale</label>
          <TextInput id="amazon-scale" bind:value={scale} block autocomplete="off" />
          <Button type="button" size="sm" onclick={() => (advanced = !advanced)}>
            {advanced ? 'Hide advanced options' : 'Advanced options'}
          </Button>
          {#if advanced}
            <label for="amazon-taxonomy-source">Clone taxonomy from profile name or ID</label>
            <TextInput
              id="amazon-taxonomy-source"
              bind:value={taxonomySource}
              block
              autocomplete="off"
            />
            <p class="profile-hint">
              Taxonomy is copied once from committed state; later changes are independent.
            </p>
          {/if}
          {#if localProblem}<p class="editing-error" role="alert">{localProblem}</p>{/if}
          <div class="profile-actions">
            <Button type="button" onclick={() => void cancel()}>Cancel</Button>
            <Button type="submit" tone="info" surface="solid">Continue</Button>
          </div>
        </form>
      {:else if controller.state.phase === 'source' || controller.state.phase === 'uploading'}
        <h1 id="amazon-import-title">Choose order-history CSV files</h1>
        <p>Files are copied into private staging and removed after the atomic import attempt.</p>
        <div class="profile-actions" aria-label="Amazon import source">
          <Button onclick={() => (sourceMode = 'files')}>Files</Button>
          <Button onclick={() => (sourceMode = 'directory')}>Directory</Button>
        </div>
        {#if sourceMode === 'files'}
          <label for="amazon-files">Amazon CSV files</label>
          <input
            id="amazon-files"
            type="file"
            accept=".csv,text/csv"
            multiple
            onchange={(event) => (selectedFiles = Array.from(event.currentTarget.files ?? []))}
          />
        {:else}
          <label for="amazon-directory">Amazon export directory</label>
          <input
            id="amazon-directory"
            type="file"
            accept=".csv,text/csv"
            multiple
            webkitdirectory={true}
            onchange={(event) => (selectedFiles = Array.from(event.currentTarget.files ?? []))}
          />
        {/if}
        <p>
          {selectedFiles.length.toLocaleString()} file{selectedFiles.length === 1 ? '' : 's'} selected.
        </p>
        {#if localProblem}<p class="editing-error" role="alert">{localProblem}</p>{/if}
        <div class="profile-actions">
          <Button onclick={() => void cancel()}>Cancel</Button>
          <Button
            tone="info"
            surface="solid"
            disabled={controller.state.phase === 'uploading'}
            onclick={() => void importFiles()}>Import</Button
          >
        </div>
      {:else if controller.state.phase === 'importing'}
        <h1 id="amazon-import-title">Importing order history</h1>
        <div class="onboarding-working"><Spinner label="Amazon import in progress" /></div>
        <p>
          {controller.state.snapshot?.progress.completed.toLocaleString() ?? '0'} records processed. The
          profile changes atomically only after validation succeeds.
        </p>
      {:else if controller.state.phase === 'complete'}
        <h1 id="amazon-import-title">Amazon import complete</h1>
        <p>{controller.state.announcement}</p>
        <dl class="editing-summary">
          <div>
            <dt>Inserted</dt>
            <dd>{controller.state.snapshot?.result.inserted ?? 0}</dd>
          </div>
          <div>
            <dt>Updated</dt>
            <dd>{controller.state.snapshot?.result.updated ?? 0}</dd>
          </div>
          <div>
            <dt>Restored</dt>
            <dd>{controller.state.snapshot?.result.restored ?? 0}</dd>
          </div>
          <div>
            <dt>Retired</dt>
            <dd>{controller.state.snapshot?.result.retired ?? 0}</dd>
          </div>
        </dl>
        <div class="profile-actions">
          <Button tone="info" surface="solid" onclick={oncomplete}>Open profile</Button>
        </div>
      {:else}
        <h1 id="amazon-import-title">Amazon import needs attention</h1>
        <p class="editing-error" role="alert">
          {controller.state.problem ?? controller.state.announcement}
        </p>
        {#if controller.state.coordinate}
          <p>
            {controller.state.coordinate.relative_filename}, record {controller.state.coordinate
              .record}
            {#if controller.state.coordinate.column}
              · {controller.state.coordinate.column}{/if}
          </p>
        {/if}
        <div class="profile-actions">
          <Button onclick={() => void cancel()}>Close</Button>
        </div>
      {/if}
      <p class="kit-sr-only" aria-live="polite">{controller.state.announcement}</p>
    </section>
  </main>
  <StatusBar>
    {#snippet left()}<span>Amazon order-history import</span>{/snippet}
    {#snippet right()}<span>{controller.state.phase}</span>{/snippet}
  </StatusBar>
</div>
