<script lang="ts">
  import { Button, EmptyState } from '@kenn-io/kit-ui'

  import type { EmptyScope } from '../../lib/shell/period'

  interface Props {
    scope: EmptyScope
    onjump: () => void
  }

  let { scope, onjump }: Props = $props()
</script>

<EmptyState title="No transactions in {scope.label}">
  <Button size="sm" onclick={onjump}>Jump to latest</Button>
</EmptyState>
