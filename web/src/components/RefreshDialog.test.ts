import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import RefreshDialog from './RefreshDialog.svelte'
import type { RefreshDialog as DialogState } from '../lib/controller/provider'
import { stubProvider, testProviderStatus } from '../test/provider'

const now = Date.parse('2026-10-13T12:00:00Z')

function open(dialog: Partial<DialogState>, overrides: Parameters<typeof stubProvider>[1] = {}) {
  return stubProvider(true, {
    phase: 'refreshing',
    kind: 'recent',
    status: testProviderStatus({
      provider_kind: 'monarch',
      last_success: '2026-10-03T12:00:00Z',
      refreshing: true,
    }),
    dialog: { view: 'progress', startedAt: now - 42_000, ...dialog },
    ...overrides,
  })
}

describe('RefreshDialog', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('names the provider and subject and shows an indeterminate start', () => {
    vi.useFakeTimers({ now })
    render(RefreshDialog, { controller: open({}) })
    const dialog = screen.getByRole('dialog', { name: 'Refreshing from Monarch' })
    expect(dialog.textContent).toContain('Recent data (last 90 days)')
    expect(dialog.textContent).toContain('Waiting for Monarch…')
    expect(dialog.textContent).toContain('Elapsed 0:42')
    expect(dialog.textContent).toMatch(/Last updated 10 days ago \(Oct [23], 2026/)
    const bar = screen.getByRole('progressbar', { name: 'Refresh progress' })
    expect(bar.hasAttribute('value')).toBe(false)
  })

  it('shows determinate progress once the provider reports a total', () => {
    vi.useFakeTimers({ now })
    const controller = open({}, {})
    controller.state.status!.progress = { fetched: 20, total: 40 }
    render(RefreshDialog, { controller })
    expect(document.querySelector('.mf-refresh__count')?.textContent?.trim()).toBe(
      'Fetched 20 of 40',
    )
    const bar = screen.getByRole('progressbar', { name: 'Refresh progress' })
    expect(bar.getAttribute('value')).toBe('20')
    expect(bar.getAttribute('max')).toBe('40')
  })

  it('cancels with the button and with Escape', async () => {
    const controller = open({})
    render(RefreshDialog, { controller })
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel refresh' }))
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(controller.cancel).toHaveBeenCalledTimes(2)
  })

  it('says Stopping… until the server stops the refresh', () => {
    render(RefreshDialog, { controller: open({ view: 'stopping' }) })
    const stopping = screen.getByRole('button', { name: 'Stopping…' })
    expect(stopping.hasAttribute('disabled')).toBe(true)
  })

  it('follows another interface refresh without a cancel button', async () => {
    const controller = open({ view: 'other' }, { kind: undefined })
    controller.state.status!.owner_renderer = 'tui'
    render(RefreshDialog, { controller })
    const dialog = screen.getByRole('dialog', { name: 'Refreshing from Monarch' })
    expect(dialog.textContent).toContain(
      'The terminal app (moneyflow tui) is refreshing this profile. Cancel it there.',
    )
    expect(screen.queryByRole('button', { name: 'Cancel refresh' })).toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(controller.cancel).not.toHaveBeenCalled()
  })

  it.each(['stopping', 'other'] as const)(
    'says why the %s dialog cannot follow the refresh and lets the user continue',
    async (view) => {
      const controller = open({ view }, { statusError: 'The server did not answer.' })
      render(RefreshDialog, { controller })
      expect(screen.getByRole('alert').textContent).toBe('The server did not answer.')
      await fireEvent.click(
        screen.getByRole('button', { name: /^Continue with data from Oct [23]/ }),
      )
      expect(controller.continueStale).toHaveBeenCalledOnce()
    },
  )

  it('keeps a failure open with Retry and Continue', async () => {
    const controller = open(
      { view: 'failed', reason: 'The provider is temporarily unavailable.' },
      { phase: 'failed' },
    )
    render(RefreshDialog, { controller })
    expect(screen.getByRole('dialog', { name: 'Refresh from Monarch failed' })).not.toBeNull()
    expect(screen.getByRole('alert').textContent).toBe('The provider is temporarily unavailable.')
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(controller.refresh).toHaveBeenCalledOnce()
    await fireEvent.click(screen.getByRole('button', { name: /^Continue with data from Oct [23]/ }))
    expect(controller.continueStale).toHaveBeenCalledOnce()
  })

  it('moves focus to Retry when a running refresh fails', async () => {
    const view = render(RefreshDialog, { controller: open({}) })
    await vi.waitFor(() =>
      expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true),
    )
    await view.rerender({
      controller: open(
        { view: 'failed', reason: 'The provider is unavailable.' },
        { phase: 'failed' },
      ),
    })
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Retry' })),
    )
  })

  it('offers reconnect when the provider needs it', async () => {
    const onreconnect = vi.fn()
    const controller = open(
      { view: 'reconnect', reason: 'Monarch needs you to reconnect.' },
      { phase: 'reconnect' },
    )
    render(RefreshDialog, { controller, onreconnect })
    expect(screen.getByRole('dialog', { name: 'Reconnect Monarch' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    expect(controller.continueStale).toHaveBeenCalledOnce()
    expect(onreconnect).toHaveBeenCalledOnce()
  })

  it('confirms suspicious removals by keyboard and dismisses with Escape', async () => {
    const controller = open({ view: 'confirmation' }, { phase: 'confirmation' })
    controller.state.status!.summary.removed_transactions = 5
    render(RefreshDialog, { controller })
    const dialog = screen.getByRole('dialog', { name: 'Confirm provider refresh' })
    expect(dialog.textContent).toContain('5 posted transactions')
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(controller.confirm).toHaveBeenCalledTimes(1)
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(controller.dismissConfirmation).toHaveBeenCalledOnce()
  })

  it('shows progress at once and announces it at most every ten seconds', async () => {
    vi.useFakeTimers({ now })
    const progressed = (fetched: number) => {
      const controller = open({})
      controller.state.status!.progress = { fetched, total: 40 }
      return controller
    }
    const view = render(RefreshDialog, { controller: progressed(1) })
    const live = screen.getByTestId('refresh-live')
    const count = () => document.querySelector('.mf-refresh__count')?.textContent?.trim()
    expect(live.textContent).toBe('Fetched 1 of 40')

    await view.rerender({ controller: progressed(30) })
    expect(count()).toBe('Fetched 30 of 40')
    await vi.advanceTimersByTimeAsync(5_000)
    expect(live.textContent).toBe('Fetched 1 of 40')
    await vi.advanceTimersByTimeAsync(5_000)
    expect(live.textContent).toBe('Fetched 30 of 40')
  })

  it('renders nothing while no refresh dialog is open', () => {
    render(RefreshDialog, { controller: stubProvider(true) })
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
