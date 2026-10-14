<script lang="ts">
  import {
    Button,
    Calendar,
    floatingPopoverStyle,
    KbdBadge,
    monthGridDates,
    SegmentedControl,
    type SegmentedControlOption,
  } from '@kenn-io/kit-ui'
  import { autoReposition, dismissable } from '@kenn-io/kit-ui/utils/popover'
  import { onMount, tick, untrack } from 'svelte'

  import PeriodGrid from './PeriodGrid.svelte'
  import PeriodCustom from './PeriodCustom.svelte'
  import PeriodMonths from './PeriodMonths.svelte'
  import {
    anchorDate,
    calendarStep,
    CHOOSER_KEYS,
    chooserTab,
    chooserTabForKey,
    clampDate,
    customDraft,
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

  const initial = untrack(() => ({ range, first, latest }))
  // A link or a currency switch can leave the period outside the data, where every cell is
  // disabled; the chooser then starts from the nearest day that has data.
  const start = clampDate(
    anchorDate(initial.range, initial.latest) ?? initial.latest,
    initial.first,
    initial.latest,
  )
  let tab = $state<ChooserTab>(chooserTab(scopeKind(initial.range)))
  let dayMonth = $state(start)
  let monthYear = $state(Number(start.slice(0, 4)))
  let draft = $state(customDraft(initial.range))
  let customMonth = $state(initial.range?.to ?? start)
  let panel = $state<HTMLElement>()
  let body = $state<HTMLElement>()
  let position = $state('')

  const kind = $derived(scopeKind(range))
  const tabs: SegmentedControlOption[] = [
    { value: 'day', label: 'Day' },
    { value: 'month', label: 'Month' },
    { value: 'year', label: 'Year' },
    { value: 'custom', label: 'Custom' },
  ]

  function place(): void {
    if (!panel) return
    const width = Math.min(280, window.innerWidth - 16)
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

  // Calendar days are the only buttons in the body with a pressed state.
  const dayButtons = (): HTMLElement[] => [
    ...(body?.querySelectorAll<HTMLElement>('button[aria-pressed]') ?? []),
  ]

  // Day and Custom each keep their own calendar month.
  function shownMonth(): string {
    return tab === 'day' ? dayMonth : customMonth
  }

  async function focusDay(date: string, month: string): Promise<void> {
    if (tab === 'day') dayMonth = month
    else customMonth = month
    await tick()
    dayButtons()[monthGridDates(month).indexOf(date)]?.focus()
  }

  // A calendar focuses its selected day, else the anchor, kept inside first to latest.
  function calendarFocus(): string {
    let date = draft.to || draft.from || start
    if (tab === 'day') date = kind === 'day' && range ? range.from : start
    return clampDate(date, first, latest)
  }

  /**
   * Focuses the most relevant cell of the current tab, or the tab itself when no cell can take
   * focus, so the chooser's keys keep working.
   */
  async function focusBody(): Promise<void> {
    await tick()
    if (tab === 'month' || tab === 'year') {
      body?.querySelector<HTMLElement>('[role="group"] button[tabindex="0"]')?.focus()
    } else {
      const date = calendarFocus()
      const month = shownMonth()
      await focusDay(date, date.slice(0, 7) === month.slice(0, 7) ? month : date)
    }
    if (body?.contains(document.activeElement)) return
    panel?.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')?.focus()
  }

  function stepDay(event: KeyboardEvent): boolean {
    if (tab !== 'day' && tab !== 'custom') return false
    const index = event.target instanceof HTMLElement ? dayButtons().indexOf(event.target) : -1
    const month = shownMonth()
    const date = monthGridDates(month)[index]
    if (date === undefined) return false
    const next = calendarStep(event.key, date, month, { first, latest })
    if (!next) return false
    void focusDay(next.date, next.month)
    return true
  }

  // The chooser's keys win over the global shortcuts while it is open.
  function handleKey(event: KeyboardEvent): boolean {
    if (event.key === 'a') {
      onpick(undefined)
      return true
    }
    const next = chooserTabForKey(event.key)
    if (next) {
      tab = next
      void focusBody()
      return true
    }
    const onTab =
      event.target instanceof HTMLElement && event.target.getAttribute('role') === 'radio'
    if (onTab && (event.key === 'ArrowDown' || event.key === 'Enter')) {
      void focusBody()
      return true
    }
    return stepDay(event)
  }

  function keydown(event: KeyboardEvent): void {
    const plain = !event.ctrlKey && !event.metaKey && !event.altKey
    if (plain && !event.defaultPrevented && handleKey(event)) event.preventDefault()
  }

  // Tabbing out closes the chooser, so its shortcut scope never outlives its focus.
  function focusout(event: FocusEvent): void {
    const next = event.relatedTarget
    if (!(next instanceof Node) || panel?.contains(next) || anchor.contains(next)) return
    ondismiss()
  }

  onMount(() => {
    place()
    void focusBody()
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

{#snippet tabLabel(option: SegmentedControlOption)}
  {option.label}<KbdBadge keys={[CHOOSER_KEYS[option.value as ChooserTab]]} />
{/snippet}

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
    segment={tabLabel}
    onchange={(value) => (tab = value as ChooserTab)}
  />
  <div class="mf-period-chooser__body" bind:this={body}>
    {#if tab === 'day'}
      <Calendar
        bind:month={dayMonth}
        locale="en-US"
        selected={kind === 'day' && range ? range : null}
        maxDate={latest}
        onpick={pickDay}
      />
    {:else if tab === 'month'}
      <PeriodMonths
        bind:year={monthYear}
        {range}
        {first}
        {latest}
        starts={[start, latest].map((date) => periodContaining(date, 'month').start)}
        onpick={(date) => onpick(periodContaining(date, 'month'))}
      />
    {:else if tab === 'year'}
      <PeriodGrid
        label="Years"
        columns={4}
        cells={yearCells(Number(first.slice(0, 4)), Number(latest.slice(0, 4)), range)}
        starts={[start, latest].map((date) => periodContaining(date, 'year').start)}
        onpick={(date) => onpick(periodContaining(date, 'year'))}
      />
    {:else}
      <PeriodCustom bind:month={customMonth} bind:draft {latest} onapply={onpick} />
    {/if}
  </div>
  <div class="mf-period-chooser__footer">
    <Button size="sm" onclick={() => onpick(undefined)}>
      All time <KbdBadge keys={['a']} />
    </Button>
  </div>
</div>
