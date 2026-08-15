<script lang="ts">
  import TaxonomyManagerForm from './TaxonomyManagerForm.svelte'
  import type { EditingController } from '../../lib/controller/editing'
  interface Props {
    controller: EditingController
    onclose: () => void
  }
  let { controller, onclose }: Props = $props()
</script>

<TaxonomyManagerForm kind="category" {controller} {onclose} />
