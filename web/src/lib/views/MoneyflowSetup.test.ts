import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick, type ComponentProps } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { appears } from '../../test/dom'
import { MoneyflowSetup } from './index'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'
const api = 'http://localhost:3000/embedded/moneyflow/api/v1/'

describe('MoneyflowSetup', () => {
  afterEach(() => {
    cleanup()
  })

  it('creates a local profile and completes at once', async () => {
    const { hostFetch, requests } = fakeServer('local')
    const completed = Promise.withResolvers<string>()
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'local',
      name: 'Example Budget',
      oncomplete: completed.resolve,
      oncancel: vi.fn(),
    })

    expect(await completed.promise).toBe(profileID)
    expect(requests).toEqual([
      {
        url: `${api}profiles`,
        body: '{"version":"1","display_name":"Example Budget","provider_kind":"local"}',
      },
    ])
  })

  it.each(['monarch', 'simplefin'] as const)(
    'removes the %s profile it created when the user cancels',
    async (provider) => {
      const { hostFetch, requests } = fakeServer(provider)
      const cancelled = Promise.withResolvers<void>()
      render(MoneyflowSetup, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        provider,
        name: 'Example Budget',
        oncomplete: vi.fn(),
        oncancel: () => cancelled.resolve(),
      })

      await fireEvent.click(await appears(() => backToProfiles()))

      await cancelled.promise
      expect(requests.at(-1)).toEqual({
        url: `${api}profiles/${profileID}/cancel`,
        body: '{"version":"1"}',
      })
    },
  )

  it('says why a new profile that the server keeps stays after a cancel', async () => {
    const { hostFetch } = fakeServer('simplefin', { removed: false })
    const oncancel = vi.fn()
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'simplefin',
      name: 'Example Budget',
      oncomplete: vi.fn(),
      oncancel,
    })

    await fireEvent.click(await appears(() => backToProfiles()))

    expect(
      await appears(() =>
        screen.queryByText(
          'Setup may already have connected the new profile, so it stays in the profile list.',
        ),
      ),
    ).not.toBeNull()
    expect(oncancel).not.toHaveBeenCalled()
  })

  it('keeps an existing profile when the user cancels its setup', async () => {
    const { hostFetch, requests } = fakeServer('monarch')
    const oncancel = vi.fn()
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'monarch',
      profileID,
      oncomplete: vi.fn(),
      oncancel,
    })

    await fireEvent.click(await appears(() => backToProfiles()))

    expect(oncancel).toHaveBeenCalledTimes(1)
    expect(requests.map(({ url }) => url)).not.toContain(`${api}profiles/${profileID}/cancel`)
  })

  it('says that a CSV profile cannot be created here', async () => {
    const { hostFetch, requests } = fakeServer('csv')
    const oncancel = vi.fn()
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'csv',
      name: 'Example Budget',
      oncomplete: vi.fn(),
      oncancel,
    })

    await tick()
    expect(screen.getByText('A CSV profile cannot be created here.')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(oncancel).toHaveBeenCalledTimes(1)
    expect(requests).toEqual([])
  })

  it.each([
    ['monarch', 'Profile setup was interrupted'],
    ['simplefin', 'Connect SimpleFIN (experimental)'],
    ['amazon', 'Confirm import settings'],
  ] as const)(
    'leaves the page frame and the theme to the host in %s setup',
    async (provider, title) => {
      const { hostFetch } = fakeServer(provider)
      render(MoneyflowSetup, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        provider,
        name: 'Example Budget',
        oncomplete: vi.fn(),
        oncancel: vi.fn(),
      })

      await appears(() => screen.queryByRole('heading', { name: title }))
      expect(screen.queryByRole('banner')).toBeNull()
      expect(screen.queryByRole('main')).toBeNull()
      expect(screen.queryByRole('contentinfo')).toBeNull()
      expect(screen.queryByRole('button', { name: /^Change theme/ })).toBeNull()
    },
  )

  it('frames setup as its own page for standalone moneyflow', async () => {
    const { hostFetch } = fakeServer('monarch')
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'monarch',
      name: 'Example Budget',
      chrome: true,
      oncomplete: vi.fn(),
      oncancel: vi.fn(),
    })

    await appears(() => screen.queryByRole('heading', { name: 'Profile setup was interrupted' }))
    expect(screen.getByRole('banner')).not.toBeNull()
    expect(screen.getByRole('main')).not.toBeNull()
    expect(screen.getByRole('contentinfo')).not.toBeNull()
    expect(screen.getByRole('button', { name: /^Change theme/ })).not.toBeNull()
  })
})

// svelte-check enforces these props: setup that creates a profile needs the host's name for it,
// and setup for an existing profile takes none.
const host = {
  apiBase: '/embedded/moneyflow/',
  fetch: vi.fn<typeof fetch>(),
  provider: 'monarch',
  oncomplete: vi.fn(),
  oncancel: vi.fn(),
} as const
void ([
  { ...host, name: 'Example Budget' },
  { ...host, profileID },
  // @ts-expect-error A profile that setup creates needs a name.
  { ...host },
  // @ts-expect-error An existing profile keeps its name.
  { ...host, profileID, name: 'Example Budget' },
] satisfies Array<ComponentProps<typeof MoneyflowSetup>>)

const backToProfiles = () => screen.queryByRole('button', { name: 'Back to profiles' })

/**
 * A moneyflow API that creates profiles and, unless `removed` is false, removes them on cancel.
 * Every other request fails with a 503 problem, so onboarding stops at its interrupted step.
 */
function fakeServer(provider: string, { removed = true } = {}) {
  const requests: Array<{ url: string; body: string }> = []
  const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
    const request = new Request(input, init)
    if (request.method === 'POST') requests.push({ url: request.url, body: await request.text() })
    if (request.url === `${api}profiles`) {
      return Response.json({
        version: '1',
        profile: {
          key: profileID,
          id: profileID,
          display_name: 'Example Budget',
          provider_kind: provider,
          status: 'setup_incomplete',
        },
      })
    }
    if (request.url === `${api}profiles/${profileID}/cancel`) {
      return Response.json({ version: '1', removed })
    }
    return Response.json(
      { status: 503, code: 'unavailable', detail: 'The provider is unavailable.' },
      { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
    )
  })
  return { hostFetch, requests }
}
