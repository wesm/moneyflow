<script lang="ts">
  import { Button, Card, KbdBadge } from '@kenn-io/kit-ui'

  interface Props {
    providerName: string
    /** Opens the write status drawer. */
    onstatus: () => void
  }

  let { providerName, onstatus }: Props = $props()
</script>

<div class="mf-lock-banner">
  <Card level="inset" padding="sm">
    <div class="mf-lock-banner__row">
      <p class="mf-lock-banner__text">
        Writing to {providerName} · Editing is locked until the write finishes
      </p>
      <Button size="sm" onclick={onstatus}
        >Status{#snippet trailing()}<KbdBadge keys={['w']} />{/snippet}</Button
      >
    </div>
  </Card>
</div>
