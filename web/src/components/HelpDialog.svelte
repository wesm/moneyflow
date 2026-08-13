<script lang="ts">
  import { KbdBadge, Modal } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../lib/api/client'

  type Capability = NonNullable<ViewProjection['capabilities']>[number]
  interface Props {
    capabilities: Capability[]
    onclose: () => void
  }
  let { capabilities, onclose }: Props = $props()
  const categories = $derived(
    [...new Set(capabilities.map((capability) => capability.category || 'Navigation'))].map(
      (category) => ({
        category,
        capabilities: capabilities.filter(
          (capability) => (capability.category || 'Navigation') === category,
        ),
      }),
    ),
  )
</script>

<Modal
  title="Keyboard shortcuts"
  closeLabel="Close help"
  {onclose}
  maxWidth="min(680px, calc(100vw - 32px))"
>
  <div class="help-groups">
    {#each categories as group (group.category)}
      <section>
        <h2>{group.category}</h2>
        <ul class="help-list">
          {#each group.capabilities as capability (capability.id)}
            <li class:help-list__unavailable={!capability.available}>
              <span>{capability.description}</span>
              {#if !capability.available}<span>Unavailable</span>{/if}
              {#if capability.key_display}<KbdBadge keys={[capability.key_display]} />{/if}
            </li>
          {/each}
        </ul>
      </section>
    {/each}
    <section>
      <h2>Terminal lifecycle</h2>
      <ul class="help-list">
        <li><span>Quit application · TUI only</span><KbdBadge keys={['q']} /></li>
        <li><span>Force quit application · TUI only</span><KbdBadge keys={['Ctrl+C']} /></li>
      </ul>
    </section>
  </div>
</Modal>
