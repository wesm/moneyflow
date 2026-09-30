<script lang="ts">
  import { Button, Card } from '@kenn-io/kit-ui'

  import type { ProfileSummary, RecoveryResponse } from '../../lib/api/catalog-client'

  interface Props {
    profile: ProfileSummary
    recovery?: RecoveryResponse | undefined
    busy?: boolean
    onconfirm: () => void
    onfresh: () => void
    onback: () => void
  }

  let { profile, recovery, busy = false, onconfirm, onfresh, onback }: Props = $props()
  const recoverable = $derived(profile.status === 'needs_recovery')
</script>

<section class="profile-panel" aria-labelledby="recovery-title">
  <p class="moneyflow-eyebrow">{profile.display_name}</p>
  <h1 id="recovery-title">
    {recoverable ? 'Profile recovery' : 'This profile cannot be opened by this version'}
  </h1>
  {#if recoverable}
    <p>This profile's database cannot be opened by this version of Moneyflow.</p>
    {#if recovery}
      <Card title="Backup location" level="inset"><code>{recovery.plan.backup_path}</code></Card>
      <p>
        Recreate moves the old database into this backup and replaces it with an empty one. Saved
        credentials stay on disk. You will need to reconnect.
      </p>
    {:else}
      <p role="status">Preparing the exact recovery plan…</p>
    {/if}
  {:else if profile.status === 'requires_newer_moneyflow'}
    <p>Install a newer Moneyflow version to open this profile.</p>
  {:else if profile.status === 'setup_incomplete' || profile.status === 'reconnect'}
    <p>This profile has no supported provider to finish setup.</p>
  {:else}
    <p>This profile was created by an unsupported Moneyflow version.</p>
  {/if}
  <p>Start fresh to set up a separate profile. Your existing profile and files will be kept.</p>
  <div class="profile-actions">
    <Button onclick={onback}>Back</Button>
    {#if recoverable && recovery}
      <Button disabled={busy} tone="danger" onclick={onconfirm}>
        {busy ? 'Recreating…' : 'Recreate profile'}
      </Button>
    {/if}
    <Button disabled={busy} tone="info" surface="solid" onclick={onfresh}>Start fresh</Button>
  </div>
</section>
