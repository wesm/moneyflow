import { createSubscriber } from 'svelte/reactivity'

import type { CatalogClient, ProfileSummary, RecoveryResponse } from '../api/catalog-client'

export interface CatalogState {
  profiles: ProfileSummary[]
  loading: boolean
  announcement: string
  problem?: string | undefined
  recovery?: RecoveryResponse | undefined
}

export interface CatalogController {
  readonly state: CatalogState
  load(): Promise<void>
  canonicalID(
    profile: ProfileSummary,
    providerKind?: 'monarch' | 'amazon' | 'local',
  ): Promise<string>
  create(
    displayName: string,
    providerKind: 'monarch' | 'amazon' | 'local',
  ): Promise<ProfileSummary | undefined>
  cancelNew(profileID: string): Promise<boolean>
  recovery(profileID: string, confirmed: boolean): Promise<RecoveryResponse | undefined>
  announce(message: string): void
}

export function createCatalogController(options: { client: CatalogClient }): CatalogController {
  let state: CatalogState = { profiles: [], loading: false, announcement: '' }
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => (notify = () => undefined)
  })

  function setState(next: CatalogState): void {
    state = next
    notify()
  }

  async function load(): Promise<void> {
    setState({ ...state, loading: true, problem: undefined })
    try {
      const response = await options.client.list()
      const profiles = [...(response.profiles ?? [])].sort((left, right) =>
        left.display_name.localeCompare(right.display_name, undefined, { sensitivity: 'base' }),
      )
      setState({ ...state, profiles, loading: false, announcement: catalogAnnouncement(profiles) })
    } catch {
      setState({
        ...state,
        loading: false,
        problem: 'The profile catalog could not be loaded.',
        announcement: 'Profile catalog unavailable.',
      })
    }
  }

  async function canonicalID(
    profile: ProfileSummary,
    providerKind?: 'monarch' | 'amazon' | 'local',
  ): Promise<string> {
    if (profile.id) return profile.id
    const activated = await options.client.activate(profile.key, providerKind)
    await load()
    if (!activated.id) throw new Error('The activated profile identity is invalid.')
    return activated.id
  }

  async function create(
    displayName: string,
    providerKind: 'monarch' | 'amazon' | 'local',
  ): Promise<ProfileSummary | undefined> {
    setState({ ...state, loading: true, problem: undefined })
    try {
      const profile = await options.client.create(displayName, providerKind)
      await load()
      setState({ ...state, announcement: `${profile.display_name} is ready for setup.` })
      return profile
    } catch {
      setState({
        ...state,
        loading: false,
        problem: 'The profile could not be created.',
        announcement: 'Profile creation failed.',
      })
      return undefined
    }
  }

  async function recovery(
    profileID: string,
    confirmed: boolean,
  ): Promise<RecoveryResponse | undefined> {
    setState({ ...state, loading: true, problem: undefined })
    try {
      const body = {
        version: '1',
        confirmed,
        ...(confirmed && state.recovery ? { plan: state.recovery.plan } : {}),
      }
      const response = await options.client.recovery(profileID, body)
      if (confirmed) {
        await load()
        setState({
          ...state,
          recovery: response,
          announcement: 'The recovered profile is ready for setup.',
        })
      } else {
        setState({
          ...state,
          loading: false,
          recovery: response,
          announcement: 'Recovery plan ready.',
        })
      }
      return response
    } catch {
      setState({
        ...state,
        loading: false,
        problem: 'The profile could not be recovered safely.',
        announcement: 'Profile recovery failed.',
      })
      return undefined
    }
  }

  async function cancelNew(profileID: string): Promise<boolean> {
    try {
      const removed = await options.client.cancelNew(profileID)
      await load()
      setState({
        ...state,
        announcement: removed ? 'Profile setup canceled.' : 'Profile setup state was retained.',
      })
      return removed
    } catch {
      setState({
        ...state,
        problem: 'The incomplete profile could not be removed safely.',
        announcement: 'Profile setup state was retained.',
      })
      return false
    }
  }

  return {
    get state() {
      subscribe()
      return state
    },
    load,
    canonicalID,
    create,
    cancelNew,
    recovery,
    announce(message) {
      setState({ ...state, announcement: message })
    },
  }
}

function catalogAnnouncement(profiles: ProfileSummary[]): string {
  if (profiles.length === 0) return 'No persistent profiles. Add one to begin.'
  return `${profiles.length.toLocaleString()} profile${profiles.length === 1 ? '' : 's'} available.`
}
