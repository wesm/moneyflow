import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProviderStatus from './ProviderStatus.svelte'
import { stubProvider, testProviderStatus } from '../test/provider'

describe('ProviderStatus', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('shows the unbound reason and disables refresh', () => {
    render(ProviderStatus, { controller: stubProvider(false) })
    const button = screen.getByRole('button', { name: 'Refresh provider data' })
    expect(button.hasAttribute('disabled')).toBe(true)
    expect(screen.getByText('Connect a provider before refreshing.')).not.toBeNull()
  })

  it('refreshes from its button and announces bounded progress', async () => {
    const controller = stubProvider(true)
    const rendered = render(ProviderStatus, { controller })
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh provider data' }))
    expect(controller.refresh).toHaveBeenCalledTimes(1)

    const refreshing = stubProvider(true, {
      phase: 'refreshing',
      status: testProviderStatus({ progress: { fetched: 20, total: 40 } }),
    })
    await rendered.rerender({ controller: refreshing })
    expect(screen.getByRole('status').textContent).toContain('20 of 40')
  })

  it('shows when provider data last refreshed', () => {
    vi.useFakeTimers({ now: Date.parse('2026-10-07T12:00:00Z') })
    const controller = stubProvider(true, {
      status: testProviderStatus({ last_success: '2026-10-07T11:55:00Z' }),
    })
    render(ProviderStatus, { controller })
    expect(screen.getByText('Updated 5m ago')).not.toBeNull()
  })

  it('omits the update time before the first successful refresh', () => {
    render(ProviderStatus, { controller: stubProvider(true, { status: testProviderStatus() }) })
    expect(screen.queryByText(/^Updated/)).toBeNull()
  })

  it('offers in-place reconnect when the provider session expires', async () => {
    const controller = stubProvider(false, { phase: 'reconnect' })
    const onreconnect = vi.fn()
    render(ProviderStatus, { controller, onreconnect })

    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect provider' }))
    expect(onreconnect).toHaveBeenCalledTimes(1)
  })

  it('keeps the button names as icons in the rail', async () => {
    const controller = stubProvider(true, {
      status: testProviderStatus({ last_success: new Date().toISOString() }),
    })
    const rendered = render(ProviderStatus, { controller, rail: true })
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh provider data' }))
    expect(controller.refresh).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/^Updated/)).toBeNull()

    const onreconnect = vi.fn()
    await rendered.rerender({
      controller: stubProvider(false, { phase: 'reconnect' }),
      onreconnect,
      rail: true,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect provider' }))
    expect(onreconnect).toHaveBeenCalledTimes(1)
  })
})
