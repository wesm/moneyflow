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
      problem: { kind: 'invalid-view', code: 'invalid_view_state' },
    })
    render(App, { basePath: '/moneyflow/', controller })

    expect(
      screen.getByRole('heading', { name: 'This Moneyflow link cannot be opened' }),
    ).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset view' }))
    expect(controller.reset).toHaveBeenCalledTimes(1)
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
    announcement: '',
    cursorIdentity: undefined,
    cursorIndex: 0,
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
    apply: vi.fn(async () => true),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(async () => true),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    restore: vi.fn(async () => undefined),
    retry: vi.fn(async () => undefined),
    reset: vi.fn(async () => undefined),
    ...overrides,
  }
}
