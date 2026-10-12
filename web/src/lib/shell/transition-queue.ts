import type { TransitionAction, ViewController } from '../controller/view-controller.svelte'

/**
 * Runs view changes from keys, the table and the command bar in the order the user made them.
 * A key such as Space reads the row under the cursor only when its turn comes, so a change from
 * any control must wait here instead of going straight to the controller, or it would run
 * first and the key would act on the view that change produced.
 */
export class TransitionQueue {
  #tail: Promise<void> = Promise.resolve()
  #controller: () => Pick<ViewController, 'apply' | 'settled'>

  constructor(controller: () => Pick<ViewController, 'apply' | 'settled'>) {
    this.#controller = controller
  }

  /** Runs `work` after the changes queued before it, even if one of them failed. */
  run(work: () => Promise<unknown>): Promise<void> {
    this.#tail = this.#tail.then(work, work).then(() => undefined)
    return this.#tail
  }

  /** Queues one controller transition. */
  readonly apply = (action: TransitionAction): Promise<void> =>
    this.run(() => this.#controller().apply(action))

  /** Settles once the queued changes, and the controller transitions they started, finish. */
  readonly settled = (): Promise<void> =>
    this.#tail.then(
      () => this.#controller().settled(),
      () => this.#controller().settled(),
    )

  /** Settles once the changes queued so far finish. */
  idle(): Promise<void> {
    return this.#tail
  }
}
