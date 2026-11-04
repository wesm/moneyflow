<script lang="ts">
  import { KbdBadge, Modal } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../lib/api/client'

  type Capability = NonNullable<ViewProjection['capabilities']>[number]
  interface Props {
    capabilities: Capability[]
    onclose: () => void
  }
  /** One help row: the capabilities it describes and the browser keys that reach them. */
  interface Entry {
    ids: string[]
    keys: string[][]
    /** The browser's wording; without one the row uses the server's description. */
    label?: string
  }
  interface Row {
    id: string
    label: string
    keys: string[][]
    available: boolean
    reason: string | undefined
  }

  // The browser's own help, written for the browser's keys. The server's descriptions and
  // categories serve the terminal, whose layout and wording do not fit here.
  const sections: Array<{ title: string; entries: Entry[] }> = [
    {
      title: 'Move around',
      entries: [
        { ids: ['cursor.up'], keys: [['↑'], ['k']], label: 'Previous row' },
        { ids: ['cursor.down'], keys: [['↓'], ['j']], label: 'Next row' },
        { ids: ['cursor.home'], keys: [['Home'], ['Shift', 'T']], label: 'First row' },
        { ids: ['cursor.end'], keys: [['End'], ['Shift', 'B']], label: 'Last row' },
        {
          ids: ['cursor.page-up', 'cursor.page-down'],
          keys: [['PgUp'], ['PgDn']],
          label: 'Page up / down',
        },
        { ids: ['view.drill'], keys: [['Enter']], label: 'Open the selected row' },
        { ids: ['view.back'], keys: [['Esc']], label: 'Go back' },
      ],
    },
    {
      title: 'Views',
      entries: [
        {
          ids: ['view.set-grouping'],
          keys: [['1–5']],
          label: 'Merchants, categories, groups, accounts, time',
        },
        { ids: ['view.cycle-grouping'], keys: [['g']], label: 'Next grouping' },
        { ids: ['view.show-detail'], keys: [['d']], label: 'All transactions' },
        { ids: ['view.switch-accounts'], keys: [['Shift', 'A']], label: 'Accounts' },
        { ids: ['view.find-duplicates'], keys: [['Shift', 'D']], label: 'Duplicates' },
      ],
    },
    {
      title: 'Time',
      entries: [
        { ids: ['time.choose'], keys: [['p']], label: 'Choose a period' },
        {
          ids: ['time.previous-period', 'time.next-period'],
          keys: [['←'], ['→']],
          label: 'Previous / next period',
        },
        { ids: ['time.toggle-granularity'], keys: [['t']], label: 'Group by year, month, or day' },
        { ids: ['time.clear-period'], keys: [['a']], label: 'All time' },
      ],
    },
    {
      title: 'Sort',
      entries: [
        { ids: ['sort.cycle'], keys: [['s']], label: 'Next sort column' },
        { ids: ['sort.reverse'], keys: [['v']], label: 'Reverse the order' },
      ],
    },
    {
      title: 'Find',
      entries: [
        { ids: ['overlay.search'], keys: [['/']], label: 'Search' },
        { ids: ['overlay.filters'], keys: [['f']], label: 'Filters' },
      ],
    },
    {
      title: 'Edit',
      entries: [
        { ids: ['transaction.show-info'], keys: [['i']], label: 'Transaction details' },
        { ids: ['transaction.edit-merchant'], keys: [['m']], label: 'Rename merchant' },
        { ids: ['transaction.edit-category'], keys: [['c']], label: 'Change category' },
        {
          ids: ['transaction.toggle-hidden'],
          keys: [['h']],
          label: 'Hide from or show in reports',
        },
        { ids: ['transaction.delete'], keys: [['x']], label: 'Delete transaction' },
        { ids: ['selection.toggle'], keys: [['Space']], label: 'Select row' },
        { ids: ['selection.toggle-all'], keys: [['Ctrl', 'A']], label: 'Select all' },
        { ids: ['changes.undo'], keys: [['u']], label: 'Undo' },
        { ids: ['changes.redo'], keys: [['Shift', 'U']], label: 'Redo' },
        { ids: ['changes.review'], keys: [['w']], label: 'Review changes' },
        { ids: ['category.manage'], keys: [['Shift', 'C']], label: 'Edit categories' },
        { ids: ['category-group.manage'], keys: [['Shift', 'G']], label: 'Edit groups' },
      ],
    },
    {
      title: 'Data',
      entries: [
        // The server names the action for the profile, such as an Amazon import.
        { ids: ['provider.refresh'], keys: [['r']] },
        { ids: ['provider.refresh-history'], keys: [['Shift', 'R']], label: 'Refresh all history' },
        { ids: ['transactions.export'], keys: [['Shift', 'E']], label: 'Export' },
        { ids: ['overlay.help'], keys: [['?']], label: 'Keyboard shortcuts' },
      ],
    },
  ]
  const described = new Set(sections.flatMap((section) => section.entries.flatMap((e) => e.ids)))

  let { capabilities, onclose }: Props = $props()

  const groups = $derived.by(() => {
    const keyed = new Map(
      capabilities.filter((c) => c.key_display).map((capability) => [capability.id, capability]),
    )
    const rowFor = (entry: Entry): Row | undefined => {
      const offered = entry.ids.flatMap((id) => keyed.get(id) ?? [])
      if (offered.length === 0) return undefined
      return {
        id: entry.ids.join(' '),
        label: entry.label ?? offered[0]!.description,
        keys: entry.keys,
        available: offered.some((c) => c.available),
        reason: offered.find((c) => c.reason)?.reason,
      }
    }
    const listed = sections.map((section) => ({
      title: section.title,
      rows: section.entries.flatMap((entry) => rowFor(entry) ?? []),
    }))
    // A key the server adds before this table knows it still appears, in the server's words.
    const more = [...keyed.values()]
      .filter((c) => !described.has(c.id))
      .map((c) => rowFor({ ids: [c.id], keys: [[c.key_display!]] })!)
    return [...listed, { title: 'More', rows: more }].filter((group) => group.rows.length > 0)
  })
</script>

<Modal
  title="Keyboard shortcuts"
  closeLabel="Close help"
  {onclose}
  maxWidth="min(760px, calc(100vw - 32px))"
>
  <div class="help-groups">
    {#each groups as group (group.title)}
      <section>
        <h2>{group.title}</h2>
        <ul class="help-list">
          {#each group.rows as row (row.id)}
            <li class:help-list__unavailable={!row.available} title={row.reason}>
              <span>{row.label}</span>
              <span class="help-list__keys">
                {#each row.keys as combo, index (index)}
                  <KbdBadge keys={combo} joiner="plus" />
                {/each}
              </span>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
</Modal>
