<script lang="ts">
  import { SelectDropdown } from '@kenn-io/kit-ui'

  import type { TransitionBody, ViewProjection } from '../../lib/api/client'
  import { containMenuKeys } from '../../lib/shell/menu-keys'
  import { currencyChoices } from '../../lib/shell/period'

  type CurrencyScope = NonNullable<TransitionBody['currency']>

  interface Props {
    projection: ViewProjection
    /** Switches the view to another currency. */
    onchange: (currency: CurrencyScope) => void
  }

  let { projection, onchange }: Props = $props()

  const key = (scope: CurrencyScope) => `${scope.currency}:${scope.scale}`
  const choices = $derived(currencyChoices(projection))
  const options = $derived(
    choices.map((entry) => ({
      value: key(entry),
      label: `${entry.currency} · ${entry.count.toLocaleString('en-US')} ${
        entry.count === 1 ? 'transaction' : 'transactions'
      }`,
      triggerLabel: entry.currency,
    })),
  )
  const value = $derived(projection.view.currency ? key(projection.view.currency) : '')

  function choose(next: string): void {
    const scope = choices.find((entry) => key(entry) === next)
    if (scope) onchange({ currency: scope.currency, scale: scope.scale })
  }
</script>

{#if options.length > 1}
  <div class="mf-currency-chip-keys" {@attach containMenuKeys}>
    <SelectDropdown class="mf-currency-chip" title="Currency" {options} {value} onchange={choose} />
  </div>
{/if}

<style>
  .mf-currency-chip-keys {
    display: contents;
  }
</style>
