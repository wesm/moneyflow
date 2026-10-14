import { describe, expect, it } from 'vitest'

import { providerCapability, testProviderStatus } from '../../test/provider'
import type { ProviderState } from './provider'
import { dataAge, elapsed, staleNotice, statusStaleReason } from './refresh-notice'

const now = Date.parse('2026-10-13T12:00:00Z')

function idle(overrides: Partial<ProviderState> = {}): ProviderState {
  return {
    phase: 'idle',
    announcement: '',
    capability: providerCapability(true),
    ...overrides,
  }
}

describe('refresh notice', () => {
  it.each([
    ['2026-10-13T11:59:30Z', 'just now'],
    ['2026-10-13T12:00:30Z', 'just now'],
    ['2026-10-13T11:59:00Z', '1 minute ago'],
    ['2026-10-13T11:15:00Z', '45 minutes ago'],
    ['2026-10-13T11:00:00Z', '1 hour ago'],
    ['2026-10-13T02:00:00Z', '10 hours ago'],
    ['2026-10-12T12:00:00Z', '1 day ago'],
    ['2026-10-03T12:00:00Z', '10 days ago'],
  ])('says data from %s is %s', (at, relative) => {
    expect(dataAge(at, now, 'en-US').relative).toBe(relative)
  })

  it('gives the date and time of the data in the reader locale', () => {
    const age = dataAge('2026-10-03T12:00:00Z', now, 'en-US')
    expect(age.absolute).toMatch(/^Oct [23], 2026, \d{1,2}:00\s[AP]M$/)
    expect(age.date).toMatch(/^Oct [23], 2026$/)
  })

  it.each([
    [0, '0:00'],
    [5_400, '0:05'],
    [754_000, '12:34'],
    [3_723_000, '1:02:03'],
  ])('shows %i ms of elapsed time as %s', (ms, text) => {
    expect(elapsed(ms)).toBe(text)
  })

  it('finds no staleness after a successful refresh', () => {
    expect(statusStaleReason(testProviderStatus({ last_success: '2026-10-13T11:00:00Z' }))).toBe(
      undefined,
    )
  })

  it.each([
    { refreshing: true },
    { last_attempt: '2026-10-13T11:00:00Z' },
    { provider_kind: 'monarch' },
  ])('does not mistake %j for an unfinished SimpleFIN refresh', (overrides) => {
    expect(
      statusStaleReason(
        testProviderStatus({
          provider_kind: 'simplefin',
          last_success: '2026-10-13T11:00:00Z',
          last_attempt: '2026-10-13T12:00:00Z',
          ...overrides,
        }),
      ),
    ).toBeUndefined()
  })

  it.each([
    [
      'provider_unavailable',
      'The provider is temporarily unavailable.',
      'Last refresh failed. The provider is temporarily unavailable.',
    ],
    [
      'provider_reconnect_required',
      'Reconnect the provider through the command line.',
      'Monarch needs you to reconnect before Moneyflow can refresh.',
    ],
    [
      'provider_deletion_confirmation_required',
      'Confirm the proposed provider removals.',
      'The last refresh would remove transactions and was not confirmed.',
    ],
  ])('explains stale data after %s', (code, detail, reason) => {
    const status = testProviderStatus({
      provider_kind: 'monarch',
      code,
      code_detail: detail,
      last_success: '2026-10-03T12:00:00Z',
    })
    expect(statusStaleReason(status)).toBe(reason)
  })

  it('does not call data stale while a refresh runs or after another process refreshed', () => {
    const failed = { code: 'provider_unavailable', last_success: '2026-10-03T12:00:00Z' }
    expect(statusStaleReason(testProviderStatus({ ...failed, refreshing: true }))).toBe(undefined)
    expect(statusStaleReason(testProviderStatus({ code: 'provider_refresh_stale' }))).toBe(
      undefined,
    )
  })

  it('calls data stale when a due refresh cannot run, and says why', () => {
    const capability = providerCapability(false, 'Unlock YNAB credentials before refreshing.')
    const due = testProviderStatus({ refresh_due: 'full', capability })
    expect(statusStaleReason(due)).toBe('Unlock YNAB credentials before refreshing.')
    expect(statusStaleReason(testProviderStatus({ capability }))).toBe(undefined)
  })

  it('leads the banner with the age of the data', () => {
    const status = testProviderStatus({
      code: 'provider_unavailable',
      code_detail: 'The provider is temporarily unavailable.',
      last_success: '2026-10-03T12:00:00Z',
    })
    const notice = staleNotice(idle({ phase: 'failed', status }), now, 'en-US')
    expect(notice?.text).toMatch(
      /^Showing data from 10 days ago \(Oct [23], 2026, \d{1,2}:00\s[AP]M\)\. Last refresh failed\. The provider is temporarily unavailable\.$/,
    )
    expect(notice?.retryAt).toBe(undefined)
  })

  it('prefers the reason this tab knows, such as a canceled refresh', () => {
    const status = testProviderStatus({
      code: 'provider_unavailable',
      last_success: '2026-10-03T12:00:00Z',
    })
    const notice = staleNotice(
      idle({ status, staleReason: 'Refresh was canceled; no provider data changed.' }),
      now,
      'en-US',
    )
    expect(notice?.text).toMatch(/\. Refresh was canceled; no provider data changed\.$/)
  })

  it('says when the retry delay ends', () => {
    const status = testProviderStatus({
      code: 'provider_unavailable',
      last_success: '2026-10-03T12:00:00Z',
      next_eligible: '2026-10-13T12:05:00Z',
    })
    expect(staleNotice(idle({ status }), now, 'en-US')?.retryAt).toMatch(/^\d{1,2}:05\s[AP]M$/)
    expect(staleNotice(idle({ status }), now + 300_000, 'en-US')?.retryAt).toBe(undefined)
  })

  it('limits manual refresh during the retry delay only for SimpleFIN', () => {
    const status = testProviderStatus({
      code: 'provider_unavailable',
      last_success: '2026-10-03T12:00:00Z',
    })
    expect(staleNotice(idle({ status }), now, 'en-US')?.manualWaits).toBe(false)
    const simplefin = { ...status, provider_kind: 'simplefin' }
    expect(staleNotice(idle({ status: simplefin }), now, 'en-US')?.manualWaits).toBe(true)
  })

  it('offers reconnect when the provider needs it', () => {
    const status = testProviderStatus({
      code: 'provider_reconnect_required',
      last_success: '2026-10-03T12:00:00Z',
    })
    expect(staleNotice(idle({ status }), now, 'en-US')?.reconnect).toBe(true)
  })

  it('stays hidden while the refresh dialog is open', () => {
    const status = testProviderStatus({
      code: 'provider_unavailable',
      last_success: '2026-10-03T12:00:00Z',
    })
    const dialog = { view: 'failed' as const, startedAt: now, reason: 'Failed.' }
    expect(staleNotice(idle({ status, dialog }), now, 'en-US')).toBe(undefined)
  })

  it('reports a status check that failed before any status arrived', () => {
    const notice = staleNotice(idle({ statusError: 'The server did not answer.' }), now, 'en-US')
    expect(notice?.text).toBe(
      'Moneyflow could not check when provider data was last refreshed. The server did not answer.',
    )
  })

  it('reports a failed status check over cached data with its known age', () => {
    const status = testProviderStatus({ last_success: '2026-10-03T12:00:00Z' })
    const notice = staleNotice(
      idle({ status, statusError: 'The server did not answer.' }),
      now,
      'en-US',
    )
    expect(notice?.checkFailed).toBe(true)
    expect(notice?.text).toMatch(
      /^Showing data from 10 days ago \(Oct [23], 2026, .*\)\. Moneyflow could not check for newer provider data\. The server did not answer\.$/,
    )
  })

  it('reports a failed status check over a failure the tab already showed', () => {
    const status = testProviderStatus({ last_success: '2026-10-03T12:00:00Z' })
    const notice = staleNotice(
      idle({ status, staleReason: 'Rate limited.', statusError: 'The server did not answer.' }),
      now,
      'en-US',
    )
    expect(notice?.checkFailed).toBe(true)
  })

  it('says so when no refresh has completed', () => {
    const status = testProviderStatus({ code: 'provider_unavailable' })
    expect(staleNotice(idle({ status }), now, 'en-US')?.text).toMatch(
      /^No refresh has completed for this profile\. Last refresh failed\./,
    )
  })
})
