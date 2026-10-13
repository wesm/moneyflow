import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import StaleBanner from './StaleBanner.svelte'
import { stubProvider, testProviderStatus } from '../test/provider'

const now = Date.parse('2026-10-13T12:00:00Z')

function failed(overrides: Parameters<typeof testProviderStatus>[0] = {}) {
  return stubProvider(true, {
    phase: 'failed',
    status: testProviderStatus({
      provider_kind: 'monarch',
      code: 'provider_unavailable',
      code_detail: 'The provider is temporarily unavailable.',
      last_success: '2026-10-03T12:00:00Z',
      ...overrides,
    }),
  })
}

describe('StaleBanner', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('says how old the data is and why, and refreshes on request', async () => {
    vi.useFakeTimers({ now })
    const controller = failed()
    render(StaleBanner, { controller })
    expect(screen.getByRole('status').textContent).toMatch(
      /Showing data from 10 days ago \(Oct [23], 2026, .*\)\. Last refresh failed\. The provider is temporarily unavailable\./,
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(controller.refresh).toHaveBeenCalledOnce()
  })

  it('waits out the retry delay and says when refresh is available', () => {
    vi.useFakeTimers({ now })
    render(StaleBanner, { controller: failed({ next_eligible: '2026-10-13T12:05:00Z' }) })
    expect(screen.getByRole('button', { name: 'Refresh' }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByText(/^Available at \d{1,2}:05/)).not.toBeNull()
  })

  it('offers reconnect when the provider needs it', async () => {
    vi.useFakeTimers({ now })
    const onreconnect = vi.fn()
    const controller = failed({ code: 'provider_reconnect_required' })
    render(StaleBanner, { controller, onreconnect })
    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    expect(onreconnect).toHaveBeenCalledOnce()
  })

  it('retries a status check that failed', async () => {
    const controller = stubProvider(true, { statusError: 'The server did not answer.' })
    render(StaleBanner, { controller })
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(controller.poll).toHaveBeenCalledOnce()
  })

  it('stays hidden while the data is current', () => {
    render(StaleBanner, {
      controller: stubProvider(true, {
        status: testProviderStatus({ last_success: '2026-10-13T11:00:00Z' }),
      }),
    })
    expect(screen.queryByRole('status')).toBeNull()
  })
})
