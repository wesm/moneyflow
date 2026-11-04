import { cleanup, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import HelpDialog from './HelpDialog.svelte'

type Capability = {
  id: string
  key_display?: string
  description: string
  category: string
  available: boolean
}

function capability(id: string, key: string, available = true): Capability {
  return {
    id,
    key_display: key,
    description: `server text for ${id}`,
    category: 'Views',
    available,
  }
}

function row(label: string): HTMLElement {
  const item = screen.getByText(label).closest('li')
  if (!item) throw new Error(`no row for ${label}`)
  return item
}

describe('HelpDialog', () => {
  afterEach(cleanup)

  it('lists browser shortcuts under browser sections with their browser keys', () => {
    render(HelpDialog, {
      capabilities: [
        capability('cursor.up', '↑/k'),
        capability('view.show-detail', 'd'),
        capability('view.find-duplicates', 'D'),
        capability('selection.toggle-all', 'ctrl+a'),
        capability('overlay.search', '/'),
      ],
      onclose: () => undefined,
    })
    const views = screen.getByRole('heading', { name: 'Views' }).closest('section')!
    expect(within(views).getByText('All transactions')).not.toBeNull()
    expect(within(row('All transactions')).getByLabelText('d')).not.toBeNull()
    expect(within(row('Duplicates')).getByLabelText('Shift D')).not.toBeNull()
    expect(within(row('Select all')).getByLabelText('Ctrl A')).not.toBeNull()
    expect(within(row('Previous row')).getByLabelText('↑')).not.toBeNull()
    expect(within(row('Previous row')).getByLabelText('k')).not.toBeNull()
    expect(screen.queryByRole('heading', { name: 'Sort' })).toBeNull()
  })

  it('leaves out terminal-only keys and the separate close button', () => {
    render(HelpDialog, {
      capabilities: [capability('overlay.search', '/')],
      onclose: () => undefined,
    })
    expect(screen.queryByText(/TUI only/)).toBeNull()
    expect(screen.queryByLabelText('Ctrl+C')).toBeNull()
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })

  it('keeps an unavailable shortcut in its place without striking it out', () => {
    render(HelpDialog, {
      capabilities: [capability('changes.undo', 'u', false)],
      onclose: () => undefined,
    })
    expect(row('Undo')).not.toBeNull()
    expect(screen.queryByText('Unavailable')).toBeNull()
  })

  it('lists a keyed capability it has no entry for under its server description', () => {
    render(HelpDialog, {
      capabilities: [capability('view.future-thing', 'z')],
      onclose: () => undefined,
    })
    expect(screen.getByRole('heading', { name: 'More' })).not.toBeNull()
    expect(within(row('server text for view.future-thing')).getByLabelText('z')).not.toBeNull()
  })

  it('lists only capabilities that have a key', () => {
    render(HelpDialog, {
      capabilities: [
        capability('overlay.search', '/'),
        {
          id: 'view.set-currency',
          description: 'Show one currency',
          category: '',
          available: true,
        },
        { id: 'sort.set', description: 'Sort by a column', category: '', available: false },
      ],
      onclose: () => undefined,
    })
    expect(screen.getByText('Search')).not.toBeNull()
    expect(screen.queryByText('Show one currency')).toBeNull()
    expect(screen.queryByText('Sort by a column')).toBeNull()
  })
})
