import { loadRail, type RailComponent } from './rail'

export const RAIL_FAILURE = 'Charts could not load. Reload the page.'
export const RAIL_LOADING = 'Loading charts…'

/** Loads the chart rail once; after a failure, the next `load()` tries again. */
export class RailLoader {
  component = $state.raw<RailComponent | undefined>()
  status = $state<'idle' | 'loading' | 'failed'>('idle')

  /** The loading or failure text, empty once the rail is ready. */
  get message(): string {
    if (this.status === 'failed') return RAIL_FAILURE
    return this.status === 'loading' ? RAIL_LOADING : ''
  }

  load(): void {
    if (this.component || this.status === 'loading') return
    this.status = 'loading'
    loadRail().then(
      (component) => {
        this.component = component
        this.status = 'idle'
      },
      (error: unknown) => {
        console.error('moneyflow: chart rail failed to load', error)
        this.status = 'failed'
      },
    )
  }
}
