import type { ProviderStatus } from '../api/client'
import type { ProviderState } from './provider'

/** The provider a refresh reads, for dialog titles and messages. */
export function providerLabel(kind: string | undefined): string {
  if (kind === 'monarch') return 'Monarch'
  if (kind === 'ynab') return 'YNAB'
  if (kind === 'simplefin') return 'SimpleFIN'
  return 'the provider'
}

/** Who runs a refresh this tab did not start, from the interface that holds the lease. */
export function refreshOwner(renderer: string | undefined): string {
  if (renderer === 'tui') return 'The terminal app (moneyflow tui)'
  // MCP servers hold leases as command-line processes.
  if (renderer === 'cli') return 'A Moneyflow command or MCP client'
  if (renderer === 'web') return 'Another browser tab or window'
  return 'Another Moneyflow process'
}

export interface DataAge {
  /** Such as `10 days ago`. */
  relative: string
  /** Such as `Oct 3, 2026, 9:15 AM`. */
  absolute: string
  /** Such as `Oct 3, 2026`. */
  date: string
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** How old data refreshed at `at` is at `now`. A clock ahead of the reader's reads as just now. */
export function dataAge(at: string, now: number, locale?: string): DataAge {
  const time = Date.parse(at)
  const age = now - time
  const when = new Date(time)
  return {
    relative: relativeAge(age),
    absolute: new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
      when,
    ),
    date: new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(when),
  }
}

function relativeAge(age: number): string {
  if (age < MINUTE) return 'just now'
  if (age < HOUR) return ago(Math.floor(age / MINUTE), 'minute')
  if (age < DAY) return ago(Math.floor(age / HOUR), 'hour')
  return ago(Math.floor(age / DAY), 'day')
}

function ago(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? '' : 's'} ago`
}

/** A running time such as `0:05`, `12:34`, or `1:02:03`. */
export function elapsed(ms: number): string {
  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  const ss = String(seconds % 60).padStart(2, '0')
  if (hours === 0) return `${minutes}:${ss}`
  return `${hours}:${String(minutes % 60).padStart(2, '0')}:${ss}`
}

/** Codes that do not mean the shown data missed a refresh. */
const FRESH_CODES = new Set(['provider_refresh_stale', 'provider_refresh_in_progress'])

/**
 * Why the data `status` describes predates a refresh that did not complete, or undefined when it
 * does not. The data is stale while no refresh runs and the last attempt failed or remains
 * unfinished, or the server says a refresh is due (app.ProviderRefreshDue) but it is unavailable.
 */
export function statusStaleReason(status: ProviderStatus): string | undefined {
  if (status.refreshing) return undefined
  const code = status.code
  if (code === 'provider_reconnect_required') {
    return `${providerLabel(status.provider_kind)} needs you to reconnect before Moneyflow can refresh.`
  }
  if (code === 'provider_deletion_confirmation_required') {
    return 'The last refresh would remove transactions and was not confirmed.'
  }
  if (code && !FRESH_CODES.has(code)) {
    return `Last refresh failed. ${status.code_detail ?? 'The provider reported an error.'}`
  }
  if (
    status.provider_kind === 'simplefin' &&
    (Date.parse(status.last_attempt ?? '') || 0) > (Date.parse(status.last_success ?? '') || 0)
  ) {
    return 'The last refresh did not finish. Automatic refresh is paused.'
  }
  if (status.refresh_due !== 'none' && !status.capability.available) {
    return status.capability.reason ?? 'Refresh is unavailable.'
  }
  return undefined
}

export interface StaleNotice {
  /** Such as `Showing data from 10 days ago (Oct 3, 2026, 9:15 AM). Last refresh failed. …` */
  text: string
  /** When the retry delay ends, while it has not, such as `10:40 AM`. */
  retryAt?: string | undefined
  /** The provider refuses manual refresh until `retryAt`, as SimpleFIN does. */
  manualWaits: boolean
  /** The provider needs reconnecting before refresh can run. */
  reconnect: boolean
  /** The status request failed, so retrying it is the action, not a refresh. */
  checkFailed: boolean
  /** Newer provider data could not be loaded into the view, so rereading it is the action. */
  rereadFailed: boolean
  /** Newer provider data is loading into the view; there is nothing to do yet. */
  loading: boolean
}

/**
 * The persistent banner for data that missed a refresh, or undefined while the data is current
 * or the refresh dialog says what is happening.
 */
export function staleNotice(
  state: ProviderState,
  now: number,
  locale?: string,
): StaleNotice | undefined {
  if (state.dialog) return undefined
  if (state.viewError || state.viewLoading) {
    return {
      text: state.viewError ?? 'Newer provider data is loading.',
      manualWaits: false,
      reconnect: false,
      checkFailed: false,
      rereadFailed: state.viewError !== undefined,
      loading: state.viewError === undefined,
    }
  }
  const status = state.status
  if (state.statusError) return checkFailed(state.statusError, status, now, locale)
  if (!status || state.phase === 'refreshing') return undefined
  const reason = state.staleReason ?? statusStaleReason(status)
  if (!reason) return undefined
  const lead = dataLead(status, now, locale)
  const eligible = status.next_eligible ? Date.parse(status.next_eligible) : 0
  return {
    text: `${lead} ${reason}`,
    retryAt:
      eligible > now
        ? new Intl.DateTimeFormat(locale, { timeStyle: 'short' }).format(new Date(eligible))
        : undefined,
    manualWaits: status.provider_kind === 'simplefin',
    reconnect: status.code === 'provider_reconnect_required',
    checkFailed: false,
    rereadFailed: false,
    loading: false,
  }
}

/** The last status request failed, so the age shown is the last one known. */
function checkFailed(
  error: string,
  status: ProviderStatus | undefined,
  now: number,
  locale: string | undefined,
): StaleNotice {
  const text = status
    ? `${dataLead(status, now, locale)} Moneyflow could not check for newer provider data. ${error}`
    : `Moneyflow could not check when provider data was last refreshed. ${error}`
  return {
    text,
    manualWaits: false,
    reconnect: false,
    checkFailed: true,
    rereadFailed: false,
    loading: false,
  }
}

function dataLead(status: ProviderStatus, now: number, locale: string | undefined): string {
  return status.last_success
    ? showing(dataAge(status.last_success, now, locale))
    : 'No refresh has completed for this profile.'
}

function showing(age: DataAge): string {
  return `Showing data from ${age.relative} (${age.absolute}).`
}
