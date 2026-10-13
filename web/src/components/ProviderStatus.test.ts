import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProviderStatus from './ProviderStatus.svelte'
import { providerCapability, stubProvider, testProviderStatus } from '../test/provider'

describe('ProviderStatus', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('shows the unbound reason and disables refresh', () => {
    render(ProviderStatus, { controller: stubProvider(false) })
    const button = screen.getByRole('button', { name: 'Refresh provider data' })
    expect(button.hasAttribute('disabled')).toBe(true)
    expectVisible('Connect a provider before refreshing.')
  })

  it('shows why a write in progress disables refresh', () => {
    const controller = stubProvider(true, {
      capability: providerCapability(false, 'Finish the provider write before refreshing.'),
    })
    render(ProviderStatus, { controller })
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).toHaveProperty(
      'disabled',
      true,
    )
    expectVisible('Finish the provider write before refreshing.')
  })

  it('names the interface that is refreshing', () => {
    const controller = stubProvider(true, {
      phase: 'refreshing',
      status: testProviderStatus({ refreshing: true, owner_renderer: 'tui' }),
      dialog: { view: 'other', startedAt: 0 },
    })
    render(ProviderStatus, { controller })
    expectVisible('The terminal app (moneyflow tui) is refreshing this profile.')
  })

  it('shows a refresh failure next to the button', () => {
    vi.useFakeTimers({ now: Date.parse('2026-10-11T12:00:00Z') })
    const controller = stubProvider(true, {
      phase: 'failed',
      announcement: 'Last refresh failed: The provider is temporarily unavailable.',
      status: testProviderStatus({ last_success: '2026-10-03T12:00:00Z' }),
    })
    render(ProviderStatus, { controller })
    expectVisible('Last refresh failed: The provider is temporarily unavailable.')
    expect(screen.getByText('Updated 8d ago')).not.toBeNull()
  })

  it('shows a warning the last refresh reported next to the button', () => {
    const warning = 'Refresh saved, but the full refresh was not recorded.'
    const controller = stubProvider(true, { status: testProviderStatus({ warning }) })
    render(ProviderStatus, { controller })
    expectVisible(warning)
  })

  it('names the refresh kind while it runs', () => {
    const controller = stubProvider(true, {
      phase: 'refreshing',
      kind: 'recent',
      status: testProviderStatus({ progress: { fetched: 20, total: 40 } }),
    })
    render(ProviderStatus, { controller })
    expect(screen.getByRole('status').textContent).toContain(
      'Refreshing recent data (last 90 days): 20 of 40',
    )
    expect(screen.getByRole('button', { name: 'Refreshing…' })).not.toBeNull()
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

  it('shows the update time, not progress, once a refresh finishes', async () => {
    vi.useFakeTimers({ now: Date.parse('2026-10-07T12:00:00Z') })
    const rendered = render(ProviderStatus, {
      controller: stubProvider(true, {
        phase: 'refreshing',
        status: testProviderStatus({ progress: { fetched: 20, total: 40 } }),
      }),
    })
    expect(screen.getByRole('status').textContent).toContain('20 of 40')

    // The next status poll still carries the finished refresh's counts.
    const finished = testProviderStatus({
      last_success: '2026-10-07T11:55:00Z',
      progress: { fetched: 40, total: 40 },
    })
    await rendered.rerender({ controller: stubProvider(true, { status: finished }) })
    expect(screen.queryByText(/^Refreshing/)).toBeNull()
    expect(screen.getByText('Updated 5m ago')).not.toBeNull()
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

function expectVisible(text: string): void {
  const element = screen.getByText(text)
  expect(element.closest('.kit-sr-only')).toBeNull()
}
