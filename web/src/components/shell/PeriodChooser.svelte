<script lang="ts">
  import {
    Button,
    Calendar,
    floatingPopoverStyle,
    IconButton,
    KbdBadge,
    SegmentedControl,
  } from '@kenn-io/kit-ui'
  import { autoReposition, dismissable } from '@kenn-io/kit-ui/utils/popover'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import { onMount, untrack } from 'svelte'

  import PeriodGrid from './PeriodGrid.svelte'
  import {
    anchorDate,
    chooserTab,
    customDraft,
    dayLabel,
    monthCells,
    periodContaining,
    scopeKind,
    yearCells,
    type ChooserTab,
    type DateRange,
    type Period,
  } from '../../lib/shell/period'

  interface Props {
    /** The label button the chooser hangs from. */
    anchor: HTMLElement
    range: DateRange
    first: string
    latest: string
    /** Applies a period; undefined selects all time. */
    onpick: (period: Period | undefined) => void
    /** Closes without moving focus; Escape then focuses the label button. */
    ondismiss: () => void
  }

  let { anchor, range, first, latest, onpick, ondismiss }: Props = $props()

  const initial = untrack(() => ({ range, latest }))
  const start = anchorDate(initial.range, initial.latest) ?? initial.latest
  let tab = $state<ChooserTab>(chooserTab(scopeKind(initial.range)))
  let dayMonth = $state(start)
  let monthYear = $state(Number(start.slice(0, 4)))
  let draft = $state(customDraft(initial.range))
  let customMonth = $state(initial.range?.to ?? start)
  let panel = $state<HTMLElement>()
  let position = $state('')

  const kind = $derived(scopeKind(range))
  const firstYear = $derived(Number(first.slice(0, 4)))
  const latestYear = $derived(Number(latest.slice(0, 4)))
  const tabs = [
    { value: 'day', label: 'Day' },
    { value: 'month', label: 'Month' },
    { value: 'year', label: 'Year' },
    { value: 'custom', label: 'Custom' },
  ]

  function place(): void {
    if (!panel) return
    const width = Math.min(264, window.innerWidth - 16)
    position = `${floatingPopoverStyle({
      trigger: anchor.getBoundingClientRect(),
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      popoverWidth: width,
      popoverHeight: panel.offsetHeight,
      triggerGap: 6,
    })}; width: ${width}px`
  }

  function pickDay(date: string): void {
    onpick(periodContaining(date, 'day'))
  }

  function pickCustomDay(date: string): void {
    if (!draft.from || draft.to) draft = { from: date, to: '' }
    else if (date < draft.from) draft = { from: date, to: draft.from }
    else draft = { from: draft.from, to: date }
  }

  function keydown(event: KeyboardEvent): void {
    const plain = !event.ctrlKey && !event.metaKey && !event.altKey
    if (event.key !== 'a' || !plain || event.defaultPrevented) return
    event.preventDefault()
    onpick(undefined)
  }

  // Tabbing out closes the chooser, so its shortcut scope never outlives its focus.
  function focusout(event: FocusEvent): void {
    const next = event.relatedTarget
    if (!(next instanceof Node) || panel?.contains(next) || anchor.contains(next)) return
    ondismiss()
  }

  onMount(() => {
    place()
    panel?.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')?.focus()
    anchor.addEventListener('focusout', focusout)
    const cleanups = [
      () => anchor.removeEventListener('focusout', focusout),
      // A press outside leaves focus where it lands; Escape returns it to the label button.
      dismissable({
        owners: () => [panel, anchor],
        dismiss: ondismiss,
        escapeFocus: () => anchor,
      }),
      autoReposition(() => panel, place),
    ]
    return () => cleanups.forEach((cleanup) => cleanup())
  })
</script>

<div
  class="mf-period-chooser kit-popover-card"
  style={position}
  role="dialog"
  aria-label="Choose period"
  tabindex="-1"
  bind:this={panel}
  onkeydown={keydown}
  onfocusout={focusout}
>
  <SegmentedControl
    block
    ariaLabel="Period type"
    options={tabs}
    value={tab}
    onchange={(value) => (tab = value as ChooserTab)}
  />
  <div class="mf-period-chooser__body">
    {#if tab === 'day'}
      <Calendar
        bind:month={dayMonth}
        locale="en-US"
        selected={kind === 'day' && range ? range : null}
        maxDate={latest}
        onpick={pickDay}
      />
    {:else if tab === 'month'}
      <div class="mf-period-chooser__stepper">
        <IconButton
          ariaLabel="Previous year"
          size="sm"
          disabled={monthYear <= firstYear}
          onclick={() => (monthYear -= 1)}
        >
          <ChevronLeftIcon size={14} aria-hidden="true" />
        </IconButton>
        <span aria-live="polite">{monthYear}</span>
        <IconButton
          ariaLabel="Next year"
          size="sm"
          disabled={monthYear >= latestYear}
          onclick={() => (monthYear += 1)}
        >
          <ChevronRightIcon size={14} aria-hidden="true" />
        </IconButton>
      </div>
      <PeriodGrid
        label="Months of {monthYear}"
        columns={4}
        cells={monthCells(monthYear, range, first, latest)}
        starts={[start, latest].map((date) => periodContaining(date, 'month').start)}
        onpick={(date) => onpick(periodContaining(date, 'month'))}
      />
    {:else if tab === 'year'}
      <PeriodGrid
        label="Years"
        columns={4}
        cells={yearCells(firstYear, latestYear, range)}
        starts={[start, latest].map((date) => periodContaining(date, 'year').start)}
        onpick={(date) => onpick(periodContaining(date, 'year'))}
      />
    {:else}
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
        bind:month={customMonth}
        locale="en-US"
        selected={draft.from ? { from: draft.from, to: draft.to || draft.from } : null}
        maxDate={latest}
        onpick={pickCustomDay}
      />
      <Button
        size="sm"
        tone="info"
        surface="solid"
        disabled={!draft.from || !draft.to}
        onclick={() => onpick({ start: draft.from, end: draft.to })}
      >
        Apply
      </Button>
    {/if}
  </div>
  <div class="mf-period-chooser__footer">
    <Button size="sm" onclick={() => onpick(undefined)}>
      All time <KbdBadge keys={['a']} />
    </Button>
  </div>
</div>
