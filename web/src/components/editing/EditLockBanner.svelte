<script lang="ts">
  import { Button, Card, KbdBadge } from '@kenn-io/kit-ui'

  import { lockText } from '../../lib/editing/lock-text'

  interface Props {
    providerName: string
    /** The provider write's phase, which sets what the text asks people to do. */
    phase: string
    /** Opens the write status drawer. */
    onstatus: () => void
  }

  let { providerName, phase, onstatus }: Props = $props()
</script>

<div class="mf-lock-banner">
  <Card level="inset" padding="sm">
    <div class="mf-lock-banner__row">
      <p class="mf-lock-banner__text">
        {lockText(providerName, phase)}
      </p>
      <Button size="sm" onclick={onstatus}
        >Status{#snippet trailing()}<KbdBadge keys={['w']} />{/snippet}</Button
      >
    </div>
  </Card>
</div>
