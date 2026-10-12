<script lang="ts">
  import { floatingPopoverStyle } from '@kenn-io/kit-ui'
  import { autoReposition } from '@kenn-io/kit-ui/utils/popover'
  import { onMount, type Snippet } from 'svelte'

  import { trapEveryTab } from '../../lib/shell/focus'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    /** The cursor row, or the selection bar; the editor hangs below a row and above the bar. */
    anchor: HTMLElement | undefined
    above: boolean
    /** The column whose cell, when the anchor row has it, the editor lines up with. */
    field: string
    /** Prefix of the title and count IDs the body renders. */
    id: string
    /** `alertdialog` for a confirmation. */
    role?: 'dialog' | 'alertdialog'
    overlays: OverlayState
    /** Escape that nothing inside claimed. */
    oncancel: () => void
    /** A press outside, which leaves focus where it lands. */
    ondismiss: () => void
    children: Snippet
  }

  let {
    anchor,
    above,
    field,
    id,
    role = 'dialog',
    overlays,
    oncancel,
    ondismiss,
    children,
  }: Props = $props()
  let panel = $state<HTMLElement>()
  let position = $state('')

  function trigger(): Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'> {
    const box = anchor?.getBoundingClientRect() ?? { left: 0, right: 0, top: 0, bottom: 0 }
    const cell = anchor?.querySelector(`[data-column="${field}"], [data-column="name"]`)
    if (!cell) return box
    const { left, right } = cell.getBoundingClientRect()
    return { left, right, top: box.top, bottom: box.bottom }
  }

  function place(): void {
    if (!panel) return
    position = floatingPopoverStyle({
      trigger: trigger(),
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      popoverHeight: panel.offsetHeight,
      constrainWidth: true,
      maxWidth: 400,
      align: above ? 'end' : 'start',
      placement: above ? 'above' : 'auto',
    })
  }

  onMount(() => {
    place()
    const press = (event: MouseEvent) => {
      if (!(event.target instanceof Node) || !panel?.contains(event.target)) ondismiss()
    }
    document.addEventListener('mousedown', press)
    const stop = autoReposition(() => panel, place)
    return () => {
      stop()
      document.removeEventListener('mousedown', press)
    }
  })
</script>

<div
  class="mf-anchored-editor kit-popover-card"
  style={position}
  {role}
  aria-labelledby={`${id}-title`}
  aria-describedby={`${id}-count`}
  tabindex="-1"
  bind:this={panel}
  {@attach trapEveryTab}
  {@attach overlays.escapeLayer(oncancel)}
>
  {@render children()}
</div>

<style>
  .mf-anchored-editor {
    box-sizing: border-box;
    position: fixed;
    z-index: var(--z-popover);
    padding: var(--space-4);
  }
  .mf-anchored-editor:focus {
    outline: none;
  }
</style>
