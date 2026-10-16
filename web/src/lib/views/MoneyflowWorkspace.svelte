<script lang="ts">
  import type { ComponentProps } from 'svelte'

  import ProfileWorkspace from './ProfileWorkspace.svelte'
  import type { ViewID } from './views'

  const props: ComponentProps<typeof ProfileWorkspace> = $props()
  let workspace = $state<ProfileWorkspace>()
  /** A view asked for before the workspace below mounted. */
  let early = $state<ViewID>()

  /** Shows a view. A view asked for while the workspace loads opens once it has loaded. */
  export function open(view: ViewID): void {
    if (workspace) workspace.open(view)
    else early = view
  }

  $effect(() => {
    if (!workspace || early === undefined) return
    workspace.open(early)
    early = undefined
  })
</script>

<!--
  The profile and read-only mode are the workspace's identity. A change to either stops the
  workspace and starts a new one, as if the host had just mounted it.
-->
{#key `${props.profileID} ${props.readOnly === true}`}
  <ProfileWorkspace bind:this={workspace} {...props} />
{/key}
