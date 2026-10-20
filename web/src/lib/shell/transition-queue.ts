import type { ViewProjection } from '../api/client'
import type { TransitionAction, ViewController } from '../controller/view-controller.svelte'

/**
 * A view change, or one decided from the view that the changes queued before it return, such
 * as a period segment that anchors on the period shown when its turn comes. Returning
 * undefined sends nothing.
 */
export type ViewChange =
  TransitionAction | ((current: ViewProjection) => TransitionAction | undefined)

/**
 * Runs view changes from keys, the table and the command bar in the order the user made them.
 * A key such as Space reads the row under the cursor only when its turn comes, so a change from
 * any control must wait here instead of going straight to the controller, or it would run
 * first and the key would act on the view that change produced.
 */
export class TransitionQueue {
  #tail: Promise<unknown> = Promise.resolve()
  #stopped = false
  #controller: () => Pick<ViewController, 'apply' | 'settled' | 'projection'>

  constructor(controller: () => Pick<ViewController, 'apply' | 'settled' | 'projection'>) {
    this.#controller = controller
  }

  /**
   * Runs `work` after the changes queued before it, even if one of them failed. Once the queue
   * has stopped, work whose turn comes runs `dropped` instead.
   */
  run(work: () => unknown, dropped: () => unknown = () => undefined): Promise<unknown> {
    const turn = () => (this.#stopped ? dropped() : work())
    this.#tail = this.#tail.then(turn, turn)
    return this.#tail
  }

  /**
   * Drops the work whose turn has not come, as when the shell that queued it is gone. A change
   * queued before that work may never land, so the work would act on a view it was not queued
   * for. It also drops a change still being decided, which a controller that outlives the shell
   * would otherwise apply.
   */
  stop(): void {
    this.#stopped = true
  }

  /** Whether the queue has stopped, so that work whose turn comes runs `dropped`. */
  get stopped(): boolean {
    return this.#stopped
  }

  /** Queues one controller transition. */
  readonly apply = (change: ViewChange): Promise<unknown> =>
    typeof change === 'function'
      ? this.run(async () => {
          const action = await this.#decide(change)
          if (action && !this.#stopped) await this.#controller().apply(action)
        })
      : this.run(() => this.#controller().apply(change))

  /** Settles once the queued changes, and the controller transitions they started, finish. */
  readonly settled = (): Promise<void> =>
    this.#tail.then(
      () => this.#controller().settled(),
      () => this.#controller().settled(),
    )

  // The controller can still be loading a view, such as an Esc return; decide from that view.
  async #decide(change: (current: ViewProjection) => TransitionAction | undefined) {
    await this.#controller().settled()
    const current = this.#controller().projection
    return current ? change(current) : undefined
  }
}
