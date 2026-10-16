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

  it('removes the profile it created when the user cancels', async () => {
    const { hostFetch, requests } = fakeServer('monarch')
    const cancelled = Promise.withResolvers<void>()
    render(MoneyflowSetup, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      provider: 'monarch',
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
 * A moneyflow API that creates profiles and removes them on cancel. Every other request fails
 * with a 503 problem, so onboarding stops at its interrupted step.
 */
function fakeServer(provider: string) {
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
      return Response.json({ version: '1', removed: true })
    }
    return Response.json(
      { status: 503, code: 'unavailable', detail: 'The provider is unavailable.' },
      { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
    )
  })
  return { hostFetch, requests }
}
