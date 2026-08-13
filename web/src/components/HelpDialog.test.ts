import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import HelpDialog from './HelpDialog.svelte'

describe('HelpDialog', () => {
  afterEach(cleanup)

  it('groups server capabilities, marks unavailable actions, and identifies TUI-only keys', () => {
    render(HelpDialog, {
      capabilities: [
        {
          id: 'overlay.search',
          key_display: '/',
          description: 'Search transactions',
          category: 'Filters',
          available: true,
        },
        {
          id: 'transaction.edit-merchant',
          key_display: 'm',
          description: 'Edit merchant name',
          category: 'Actions',
          available: false,
        },
      ],
      onclose: () => undefined,
    })
    expect(screen.getByRole('heading', { name: 'Filters' })).not.toBeNull()
    expect(screen.getByText('Edit merchant name').closest('li')?.textContent).toContain(
      'Unavailable',
    )
    expect(screen.getByLabelText('q').closest('li')?.textContent).toContain('TUI only')
    expect(screen.getByLabelText('Ctrl+C').closest('li')?.textContent).toContain('TUI only')
  })
})
