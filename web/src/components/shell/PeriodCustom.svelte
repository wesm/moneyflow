<script lang="ts">
  import { Button, Calendar } from '@kenn-io/kit-ui'

  import { dayLabel, type Period } from '../../lib/shell/period'

  interface Props {
    /** Any day in the calendar's shown month (bindable). */
    month: string
    /** The range being picked; an empty string is an endpoint not yet picked (bindable). */
    draft: { from: string; to: string }
    latest: string
    onapply: (period: Period) => void
  }

  let { month = $bindable(), draft = $bindable(), latest, onapply }: Props = $props()

  function pick(date: string): void {
    if (!draft.from || draft.to) draft = { from: date, to: '' }
    else if (date < draft.from) draft = { from: date, to: draft.from }
    else draft = { from: draft.from, to: date }
  }
</script>

<dl class="mf-period-chooser__endpoints" aria-live="polite">
  <div
    class="mf-period-chooser__endpoint"
    class:mf-period-chooser__endpoint--next={!draft.from || draft.to !== ''}
  >
    <dt>From</dt>
    <dd>{draft.from ? dayLabel(draft.from) : '—'}</dd>
  </div>
  <div
    class="mf-period-chooser__endpoint"
    class:mf-period-chooser__endpoint--next={draft.from !== '' && draft.to === ''}
  >
    <dt>To</dt>
    <dd>{draft.to ? dayLabel(draft.to) : '—'}</dd>
  </div>
</dl>
<Calendar
  bind:month
  locale="en-US"
  selected={draft.from ? { from: draft.from, to: draft.to || draft.from } : null}
  maxDate={latest}
  onpick={pick}
/>
<Button
  size="sm"
  tone="info"
  surface="solid"
  disabled={!draft.from || !draft.to}
  onclick={() => onapply({ start: draft.from, end: draft.to })}
>
  Apply
</Button>
