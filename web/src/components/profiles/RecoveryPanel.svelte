<script lang="ts">
  import { Button, Card } from '@kenn-io/kit-ui'

  import type { ProfileSummary, RecoveryResponse } from '../../lib/api/catalog-client'

  interface Props {
    profile: ProfileSummary
    recovery?: RecoveryResponse | undefined
    busy?: boolean
    onconfirm: () => void
    onback: () => void
  }

  let { profile, recovery, busy = false, onconfirm, onback }: Props = $props()
  const recoverable = $derived(profile.status === 'needs_recovery')
</script>

<section class="profile-panel" aria-labelledby="recovery-title">
  <p class="moneyflow-eyebrow">{profile.display_name}</p>
  <h1 id="recovery-title">
    {recoverable ? 'Profile recovery' : 'This profile cannot be opened by this version'}
  </h1>
  {#if recoverable}
    <p>
      Moneyflow can preserve the existing database in a backup directory and install a pristine
      current profile. Provider session and encrypted credentials remain on disk.
    </p>
    {#if recovery}
      <Card title="Backup location" level="inset"><code>{recovery.plan.backup_path}</code></Card>
      <p>
        This removes the old local database from active use. It does not reconnect automatically.
      </p>
    {:else}
      <p role="status">Preparing the exact recovery plan…</p>
    {/if}
  {:else if profile.status === 'requires_newer_moneyflow'}
    <p>
      Install a newer Moneyflow version to open this profile. Recreate is intentionally unavailable.
    </p>
  {:else}
    <p>
      The profile manifest was written by an unsupported version. No destructive action is offered.
    </p>
  {/if}
  <div class="profile-actions">
    <Button onclick={onback}>Back</Button>
    {#if recoverable && recovery}
      <Button disabled={busy} tone="danger" surface="solid" onclick={onconfirm}>
        {busy ? 'Recreating…' : 'Recreate profile'}
      </Button>
    {/if}
  </div>
</section>
