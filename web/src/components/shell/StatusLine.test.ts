import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import StatusLine from './StatusLine.svelte'
import { status } from '../../lib/shell/status.svelte'

const hints = [
  { key: '↵', label: 'Open' },
  { key: '?', label: 'Help' },
]

function props(overrides: Record<string, unknown> = {}) {
  return {
    hints,
    connection: 'connected' as const,
    revision: '7',
    onretry: vi.fn(),
    ...overrides,
  }
}

describe('StatusLine', () => {
  afterEach(cleanup)

  it('shows key hints, the connection, and the profile revision', () => {
    render(StatusLine, props())
    const footer = screen.getByText('profile revision 7').closest('footer')!
    expect(footer.textContent).toContain('Open')
    expect(footer.textContent).toContain('Help')
    expect(screen.getByText('Connected')).not.toBeNull()
    expect(screen.getByText('profile revision 7')).not.toBeNull()
  })

  it('reports an update only after it has run for 300 ms', async () => {
    vi.useFakeTimers()
    try {
      const rendered = render(StatusLine, props({ connection: 'updating' }))
      expect(screen.getByText('Connected')).not.toBeNull()
      await vi.advanceTimersByTimeAsync(299)
      expect(screen.queryByText('Updating…')).toBeNull()
      await vi.advanceTimersByTimeAsync(1)
      expect(screen.getByText('Updating…')).not.toBeNull()
      expect(screen.queryByText('Connected')).toBeNull()

      await rendered.rerender(props({ connection: 'connected' }))
      await rendered.rerender(props({ connection: 'updating' }))
      expect(screen.getByText('Connected')).not.toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('offers a retry when offline', async () => {
    const onretry = vi.fn()
    render(StatusLine, props({ connection: 'offline', onretry }))
    expect(screen.getByText('Offline')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onretry).toHaveBeenCalledTimes(1)
  })

  it('offers a retry when the last request failed', async () => {
    const onretry = vi.fn()
    render(StatusLine, props({ connection: 'failed', onretry }))
    expect(screen.getByText('Not updated')).not.toBeNull()
    expect(screen.queryByText('Connected')).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onretry).toHaveBeenCalledTimes(1)
  })

  it('puts pending text in place of the hints and the revision at phone width', () => {
    render(StatusLine, props({ compact: true, pending: '1 pending edit · 1 transaction' }))
    expect(screen.queryByText('Open')).toBeNull()
    expect(screen.getByRole('status', { name: 'Pending profile changes' })).not.toBeNull()
    expect(screen.queryByText('profile revision 7')).toBeNull()
    expect(screen.getByText('Connected')).not.toBeNull()
  })

  it('carries the pending text only when given', async () => {
    const rendered = render(StatusLine, props())
    expect(screen.queryByText(/pending/)).toBeNull()
    await rendered.rerender(props({ pending: '2 pending edits · 5 transactions' }))
    expect(screen.getByRole('status', { name: 'Pending profile changes' }).textContent).toBe(
      '2 pending edits · 5 transactions',
    )
  })

  it('shows an activity in place of the key hints until a notice covers it', async () => {
    render(StatusLine, props({ activity: 'Writing to Monarch: 3 of 6…' }))
    expect(screen.getByText('Writing to Monarch: 3 of 6…')).not.toBeNull()
    expect(screen.queryByText('Open')).toBeNull()
    status.notify('Editing is locked while the write runs.')
    await tick()
    expect(screen.queryByText('Writing to Monarch: 3 of 6…')).toBeNull()
    status.clear()
    await tick()
    expect(screen.getByText('Writing to Monarch: 3 of 6…')).not.toBeNull()
  })

  it('shows a notice in a polite live region in place of the key hints', async () => {
    render(StatusLine, props())
    const region = screen.getByRole('status', { name: 'Notice' })
    expect(region.textContent?.trim()).toBe('')
    expect(screen.getByText('Open')).not.toBeNull()
    status.notify('Open the group to delete its transactions.')
    await tick()
    expect(region.textContent?.trim()).toBe('Open the group to delete its transactions.')
    expect(screen.queryByText('Open')).toBeNull()
    status.clear()
    await tick()
    expect(region.textContent?.trim()).toBe('')
    expect(screen.getByText('Open')).not.toBeNull()
  })
})
