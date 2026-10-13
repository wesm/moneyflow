import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App.svelte'
import type { CatalogController } from './lib/controller/catalog.svelte'
import type { ViewController } from './lib/controller/view-controller.svelte'
import { testProviderWriteController } from './test/provider-write'

describe('Moneyflow application scaffold', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('mounts the shared chrome around a profile loading state', () => {
    render(App, { basePath: '/moneyflow/', controller: stubController() })

    expect(screen.getByRole('main', { name: 'Moneyflow' })).not.toBeNull()
    expect(screen.getByRole('heading', { name: 'Loading financial view…' })).not.toBeNull()
    expect(screen.getByRole('status').textContent).toBe('Loading profile data…')
    expect(screen.getByRole('button', { name: /change theme/i })).not.toBeNull()
  })

  it('renders the profile catalog at the base route without constructing a finance controller', () => {
    const catalog = {
      state: { profiles: [], loading: false, announcement: '', problem: undefined },
      load: vi.fn(async () => undefined),
    } as unknown as CatalogController

    render(App, { basePath: '/moneyflow/', catalog })

    expect(screen.getByRole('heading', { name: 'Choose a Moneyflow profile' })).not.toBeNull()
    expect(catalog.load).toHaveBeenCalledTimes(1)
  })

  it('renders a safe invalid-link shell with a reset action', async () => {
    const controller = stubController({
      loading: false,
      problem: {
        kind: 'invalid-view',
        code: 'invalid_view_state',
        detail: 'The view URL is invalid.',
      },
    })
    render(App, { basePath: '/moneyflow/', controller })

    expect(
      screen.getByRole('heading', { name: 'This Moneyflow link cannot be opened' }),
    ).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset view' }))
    expect(controller.reset).toHaveBeenCalledTimes(1)
  })

  it('says the server is unreachable only when the first load could not reach it', async () => {
    const controller = stubController({
      loading: false,
      problem: {
        kind: 'request',
        code: 'network_error',
        detail: 'Moneyflow could not reach the server. Check the connection and retry.',
      },
    })
    render(App, { basePath: '/moneyflow/', controller })

    expect(screen.getByText('Connection problem')).not.toBeNull()
    expect(screen.getByText(/could not reach the server/)).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(controller.retry).toHaveBeenCalledTimes(1)
  })

  it('shows the server reason when the first load is refused', () => {
    const detail = 'This profile was saved by a newer Moneyflow. Install that version to open it.'
    render(App, {
      basePath: '/moneyflow/',
      controller: stubController({
        loading: false,
        problem: { kind: 'request', code: 'schema_newer', detail },
      }),
    })

    expect(screen.getByRole('heading', { name: 'The financial view did not load' })).not.toBeNull()
    expect(screen.getByText(detail)).not.toBeNull()
    expect(screen.queryByText('Connection problem')).toBeNull()
    expect(screen.queryByText(/server is reachable/)).toBeNull()
  })

  it('offers fresh setup for a legacy profile with no provider', async () => {
    const profile = {
      key: 'legacy',
      id: '',
      display_name: 'Moneyflow',
      provider_kind: '',
      status: 'setup_incomplete',
    }
    const catalog = {
      state: { profiles: [profile], loading: false, announcement: '', problem: undefined },
      load: vi.fn(async () => undefined),
      canonicalID: vi.fn(async () => 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'),
      announce: vi.fn(),
    } as unknown as CatalogController
    render(App, { basePath: '/moneyflow/', catalog })
    await fireEvent.click(screen.getByRole('button', { name: /Moneyflow.*Setup incomplete/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Start fresh' }))
    expect(screen.getByRole('heading', { name: 'Choose a provider' })).not.toBeNull()
    expect(catalog.canonicalID).not.toHaveBeenCalled()
  })

  it('rechecks the profile and provider on focus', async () => {
    const controller = stubController()
    render(App, { basePath: '/moneyflow/', controller })
    await Promise.resolve()
    vi.mocked(controller.recheck).mockClear()
    vi.mocked(controller.provider.poll).mockClear()

    await fireEvent.focus(window)

    expect(controller.recheck).toHaveBeenCalledTimes(1)
    expect(controller.provider.poll).toHaveBeenCalledTimes(1)
  })

  it('polls provider status only while the document is visible', async () => {
    vi.useFakeTimers()
    try {
      const controller = stubController()
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
      const rendered = render(App, { basePath: '/moneyflow/', controller })
      await vi.advanceTimersByTimeAsync(60_000)
      expect(controller.provider.poll).not.toHaveBeenCalled()
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      await fireEvent(document, new Event('visibilitychange'))
      expect(controller.provider.poll).toHaveBeenCalledTimes(1)
      rendered.unmount()
    } finally {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      vi.useRealTimers()
    }
  })

  it('writes pending cursor history when the page hides, is left, or unmounts', async () => {
    const controller = stubController()
    const rendered = render(App, { basePath: '/moneyflow/', controller })
    try {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
      await fireEvent(document, new Event('visibilitychange'))
      expect(controller.flushHistory).toHaveBeenCalledTimes(1)

      await fireEvent(window, new Event('pagehide'))
      expect(controller.flushHistory).toHaveBeenCalledTimes(2)

      rendered.unmount()
      expect(controller.flushHistory).toHaveBeenCalledTimes(3)
    } finally {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    }
  })

  it('rolls back a newly added profile when onboarding cannot start and the user goes back', async () => {
    const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'
    const catalogState: CatalogController['state'] = {
      profiles: [],
      loading: false,
      announcement: '',
      problem: undefined,
    }
    const catalog = {
      state: catalogState,
      load: vi.fn(async () => undefined),
      create: vi.fn(async (displayName: string) => {
        const created = {
          key: profileID,
          id: profileID,
          display_name: displayName,
          provider_kind: 'monarch',
          status: 'setup_incomplete',
        }
        catalogState.profiles = [created]
        return created
      }),
      canonicalID: vi.fn(async () => profileID),
      cancelNew: vi.fn(async () => true),
    } as unknown as CatalogController
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new Error('offline')))
    render(App, { basePath: '/moneyflow/', catalog })

    await fireEvent.click(screen.getByRole('button', { name: /Add profile/ }))
    await fireEvent.click(screen.getByRole('button', { name: /Monarch Money/ }))
    await fireEvent.input(screen.getByLabelText('Profile name'), {
      target: { value: 'New Profile' },
    })
    await fireEvent.submit(screen.getByRole('button', { name: 'Create profile' }).closest('form')!)
    await vi.waitFor(() => expect(catalog.create).toHaveBeenCalledWith('New Profile', 'monarch'))
    await screen.findByRole('heading', { name: 'Profile setup was interrupted' })
    await fireEvent.click(screen.getByRole('button', { name: 'Back to profiles' }))

    expect(catalog.cancelNew).toHaveBeenCalledWith(profileID)
  })
})

function stubController(overrides: Partial<ViewController> = {}): ViewController {
  return {
    projection: undefined,
    loading: true,
    opening: false,
    announcement: '',
    cursorIdentity: undefined,
    cursorIndex: 0,
    neighbors: [],
    problem: undefined,
    editing: {} as ViewController['editing'],
    review: {} as ViewController['review'],
    provider: {
      state: { phase: 'idle', announcement: '', capability: undefined },
      sync: vi.fn(),
      poll: vi.fn(async () => undefined),
      refresh: vi.fn(async () => true),
      confirm: vi.fn(async () => true),
      dismissConfirmation: vi.fn(),
    } as ViewController['provider'],
    providerWrite: testProviderWriteController(),
    duplicates: {} as ViewController['duplicates'],
    export: {} as ViewController['export'],
    hydrate: vi.fn(async () => undefined),
    recheck: vi.fn(async () => undefined),
    moveCursor: vi.fn(async () => undefined),
    moveCursorTo: vi.fn(async () => undefined),
    moveHome: vi.fn(async () => undefined),
    showIndex: vi.fn(async () => undefined),
    showAnchor: vi.fn(async () => undefined),
    recheckNewer: vi.fn(async () => undefined),
    reloadDay: vi.fn(async () => undefined),
    insights: vi.fn(() => new Promise<never>(() => undefined)),
    apply: vi.fn(async () => true),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(async () => 'applied' as const),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    hold: <T>(work: Promise<T>) => work,
    settled: async () => undefined,
    restore: vi.fn(async () => undefined),
    flushHistory: vi.fn(),
    retry: vi.fn(async () => undefined),
    reset: vi.fn(async () => undefined),
    ...overrides,
  }
}
