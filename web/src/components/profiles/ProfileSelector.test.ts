import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProfileSelector from './ProfileSelector.svelte'

describe('profile selector', () => {
  afterEach(cleanup)

  it('supports Python selector keys and focusable unavailable providers', async () => {
    render(ProfileSelector, { props: syntheticCatalogProps() })

    await fireEvent.keyDown(window, { key: 'a' })
    expect(screen.getByRole('heading', { name: 'Choose a provider' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'y' })
    expect(screen.getByRole('status').textContent).toContain('YNAB is not available in Go yet.')
  })

  it('supports arrows, Home, Enter, d/a/n, Escape, and q without hiding local statuses', async () => {
    const props = syntheticCatalogProps()
    render(ProfileSelector, { props })

    expect(screen.getByText('Ready')).not.toBeNull()
    expect(screen.getByText('Local only')).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    await fireEvent.keyDown(window, { key: 'Home' })
    await fireEvent.keyDown(window, { key: 'Enter' })
    expect(props.onopen).toHaveBeenCalledWith('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa')
    await fireEvent.keyDown(window, { key: 'd' })
    expect(props.ondemo).toHaveBeenCalledTimes(1)
    await fireEvent.keyDown(window, { key: 'n' })
    expect(screen.getByRole('heading', { name: 'Choose a provider' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('heading', { name: 'Choose a Moneyflow profile' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'q' })
    expect(props.onexit).toHaveBeenCalledTimes(1)
  })

  it('requires an explicit offline choice for local-only profiles', async () => {
    const props = syntheticCatalogProps()
    render(ProfileSelector, { props })

    await fireEvent.click(screen.getByRole('button', { name: /Beta/ }))
    expect(screen.getByRole('heading', { name: 'Open this profile offline?' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Open Offline' }))
    expect(props.onopen).toHaveBeenCalledWith('profile_bbbbbbbbbbbbbbbbbbbbbbbbbb')
  })

  it('never offers Recreate for profiles that require a newer Moneyflow', async () => {
    const props = syntheticCatalogProps()
    props.profiles = [
      {
        ...props.profiles[0]!,
        display_name: 'Future profile',
        status: 'requires_newer_moneyflow',
      },
    ]
    render(ProfileSelector, { props })

    await fireEvent.click(screen.getByRole('button', { name: /Future profile/ }))
    expect(
      screen.getByRole('heading', { name: 'This profile cannot be opened by this version' }),
    ).not.toBeNull()
    expect(screen.queryByRole('button', { name: 'Recreate profile' })).toBeNull()
  })
})

function syntheticCatalogProps() {
  return {
    profiles: [
      {
        key: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
        id: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
        display_name: 'Alpha',
        provider_kind: 'monarch',
        status: 'ready',
      },
      {
        key: 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb',
        id: 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb',
        display_name: 'Beta',
        provider_kind: 'local',
        status: 'local_only',
      },
    ],
    loading: false,
    announcement: '',
    onopen: vi.fn(),
    onsetup: vi.fn(),
    onrecover: vi.fn(),
    oncreate: vi.fn(),
    ondemo: vi.fn(),
    onexit: vi.fn(),
  }
}
