<script lang="ts">
  import EditorCombobox from '../components/editing/EditorCombobox.svelte'
  import type { EditorKeyAction } from '../lib/editing/editor-keys'
  import { editorOptions, type EditorChoice, type EditorOption } from '../lib/editing/options'

  /** Derives the options from the bound text, as the editor host does. */
  interface Props {
    choices: EditorChoice[]
    groups?: EditorChoice[]
    value?: string
    valid?: boolean
    onaccept: (option: EditorOption) => void
    onkeyaction: (action: EditorKeyAction) => void
  }

  let {
    choices,
    groups = [],
    value = $bindable(''),
    valid = true,
    onaccept,
    onkeyaction,
  }: Props = $props()
  const options = $derived(editorOptions(choices, value, true, groups))
</script>

<div data-testid="editor-host">
  <EditorCombobox label="Merchant" {options} bind:value {valid} {onaccept} {onkeyaction} />
</div>
