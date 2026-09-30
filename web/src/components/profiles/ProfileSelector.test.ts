import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProfileSelector from './ProfileSelector.svelte'

describe('profile selector', () => {
  afterEach(cleanup)

  it('supports provider shortcut keys including YNAB', async () => {
    const props = syntheticCatalogProps()
    render(ProfileSelector, { props })

    await fireEvent.keyDown(window, { key: 'a' })
    expect(screen.getByRole('heading', { name: 'Choose a provider' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'y' })
    expect(screen.getByRole('heading', { name: 'Name this profile' })).not.toBeNull()
    expect(screen.getByText('YNAB')).not.toBeNull()
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

  it.each(['local_only', 'setup_incomplete'])(
    'opens local profiles with status %s offline',
    async (status) => {
      const props = syntheticCatalogProps()
      props.profiles[2]!.status = status
      render(ProfileSelector, { props })

      await fireEvent.click(screen.getByRole('button', { name: /Beta/ }))
      expect(screen.getByRole('heading', { name: 'Open this profile offline?' })).not.toBeNull()
      await fireEvent.click(screen.getByRole('button', { name: 'Open Offline' }))
      expect(props.onopen).toHaveBeenCalledWith('profile_bbbbbbbbbbbbbbbbbbbbbbbbbb')
    },
  )

  it('offers unlock or offline open for a locally ready YNAB profile', async () => {
    const props = syntheticCatalogProps()
    props.profiles = [
      {
        ...props.profiles[0]!,
        display_name: 'Example YNAB',
        provider_kind: 'ynab',
        status: 'ready',
      },
    ]
    render(ProfileSelector, { props })

    await fireEvent.click(screen.getByRole('button', { name: /Example YNAB/ }))
    expect(screen.getByRole('heading', { name: 'Unlock YNAB or open offline?' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Open Offline' }))
    expect(props.onopen).toHaveBeenCalledWith('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa')
    cleanup()
    render(ProfileSelector, { props })
    await fireEvent.click(screen.getByRole('button', { name: /Example YNAB/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Unlock YNAB' }))
    expect(props.onsetup).toHaveBeenCalledWith('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa')
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

  it('focuses the first loaded profile after an asynchronous catalog load', async () => {
    const props = syntheticCatalogProps()
    const rendered = render(ProfileSelector, { props: { ...props, profiles: [], loading: true } })

    await rendered.rerender({ ...props, loading: false })

    await vi.waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('button', { name: /Alpha/ })),
    )
  })

  it('routes an incomplete YNAB profile to YNAB setup', async () => {
    const props = syntheticCatalogProps()
    render(ProfileSelector, { props })

    await fireEvent.click(screen.getByRole('button', { name: /YNAB Profile/ }))

    expect(props.onsetup).toHaveBeenCalledWith('profile_cccccccccccccccccccccccccc')
  })

  it.each([
    ['needs_recovery', 'monarch'],
    ['requires_newer_moneyflow', 'monarch'],
    ['manifest_unsupported', ''],
    ['setup_incomplete', 'local'],
    ['setup_incomplete', ''],
  ])('starts fresh from %s / %s without recreating the old profile', async (status, provider) => {
    const props = syntheticCatalogProps()
    props.profiles = [{ ...props.profiles[0]!, status, provider_kind: provider }]
    props.oncreate.mockResolvedValue({ ...props.profiles[0], id: 'profile_new' })
    render(ProfileSelector, { props })

    await fireEvent.click(screen.getByRole('button', { name: /Alpha/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Start fresh' }))
    await fireEvent.keyDown(window, { key: 'y' })
    await fireEvent.input(screen.getByLabelText('Profile name'), { target: { value: 'Fresh' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create profile' }))

    expect(props.oncreate).toHaveBeenCalledWith('Fresh', 'ynab')
    expect(props.onsetup).toHaveBeenCalledExactlyOnceWith('profile_new')
    expect(props.onrecover).not.toHaveBeenCalledWith(props.profiles[0]!.id, true)
  })

  it('offers fresh setup after recreating a profile without a provider', async () => {
    const props = syntheticCatalogProps()
    props.profiles = [{ ...props.profiles[0]!, provider_kind: 'local', status: 'needs_recovery' }]
    const rendered = render(ProfileSelector, {
      props: {
        ...props,
        recovery: {
          version: '1',
          recreated: false,
          plan: {
            backup_path: '/synthetic/backup',
            profile_key: props.profiles[0]!.key,
            profile_id: props.profiles[0]!.id,
            started_at: '2026-01-01T00:00:00Z',
            in_progress: false,
            original_code: 'schema_incompatible',
          },
        },
      },
    })
    props.onrecover.mockImplementation(async (_id: string, confirmed: boolean) => {
      if (confirmed) {
        await rendered.rerender({
          profiles: [{ ...props.profiles[0]!, status: 'setup_incomplete' }],
        })
      }
    })

    await fireEvent.click(screen.getByRole('button', { name: /Alpha/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Recreate profile' }))
    await vi.waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Recreate profile' })).toBeNull(),
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Start fresh' }))

    expect(screen.getByRole('heading', { name: 'Choose a provider' })).not.toBeNull()
    expect(props.onsetup).not.toHaveBeenCalled()
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
        key: 'profile_cccccccccccccccccccccccccc',
        id: 'profile_cccccccccccccccccccccccccc',
        display_name: 'YNAB Profile',
        provider_kind: 'ynab',
        status: 'setup_incomplete',
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
