import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { describe, expect, it, vi } from 'vitest'

import {
  MoneyflowProblem,
  type DuplicateResponse,
  type MoneyflowClient,
  type MutationFetch,
  type Problem,
  type SelectionValue,
  type ViewProjection,
} from '../api/client'
import { testDuplicates as duplicates } from '../../test/duplicates'
import { testProjection } from '../../test/projection'
import { createDuplicateController, type DuplicateHost } from './duplicates'

describe('DuplicateController', () => {
  it('keeps selection transient and toggles against a derived detail query', async () => {
    const current = testProjection({
      revision: '3',
      canonical_query: 'group=category&subgroup=merchant&v=1',
    })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(duplicates())
      .mockResolvedValueOnce(duplicates({ selection: selection('selected'), selection_count: 1 }))
    vi.mocked(client.transition).mockResolvedValue(
      testProjection({ selection: selection('selected'), selection_count: 1 }),
    )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })

    await expect(controller.open()).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ query: current.canonical_query, expected_revision: '3' }),
      undefined,
    )
    await expect(controller.toggleFocused()).resolves.toBe(true)
    expect(client.transition).toHaveBeenCalledWith(
      expect.objectContaining({
        query: 'group=category&mode=detail&v=1',
        action: 'selection.toggle',
        target: { kind: 'transaction', identity: 'txn-1' },
      }),
    )
    expect(controller.state.projection?.selection_count).toBe(1)
    expect(current.selection_count).toBe(0)
    expect(location.search).toBe('')
  })

  it('locks interaction while a selection transition is pending', async () => {
    let release!: (value: ViewProjection) => void
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockResolvedValue(duplicates())
    vi.mocked(client.transition).mockImplementation(
      async () => await new Promise<ViewProjection>((resolve) => (release = resolve)),
    )
    const current = testProjection()
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })
    await controller.open()

    const pending = controller.toggleFocused()
    expect(controller.state.phase).toBe('loading')
    controller.requestDelete()
    expect(controller.state.phase).toBe('loading')
    release(testProjection({ selection: selection('selected'), selection_count: 1 }))

    await expect(pending).resolves.toBe(true)
    expect(controller.state.phase).toBe('ready')
  })

  it('stages deletion only after confirmation and reloads remote projections', async () => {
    let current = testProjection({ revision: '3' })
    const next = testProjection({ revision: '4' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(duplicates({ selection: selection('selected'), selection_count: 1 }))
      .mockResolvedValueOnce(
        duplicates({ revision: '4', total_groups: 0, total_transactions: 0, groups: [] }),
      )
    const transport = mutationTransport(
      Response.json({
        version: '1',
        revision: '4',
        canonical_query: 'mode=detail&v=1',
        projection: next,
        pending: { active_operations: 1, inactive_operations: 0, affected_transactions: 1 },
        selection: { kind: 'cleared', value: selection('empty') },
      }),
    )
    const recheck = vi.fn(async () => {
      current = next
      return next
    })
    const controller = createDuplicateController({
      client,
      mutations: transport,
      host: { current: () => current, recheck },
    })

    await controller.open()
    controller.requestDelete()
    expect(controller.state.phase).toBe('confirming')
    expect(vi.mocked(transport.request)).not.toHaveBeenCalled()
    await expect(controller.confirmDelete()).resolves.toBe(true)
    expect(transport.request).toHaveBeenCalledTimes(1)
    const body = JSON.parse(vi.mocked(transport.request).mock.calls[0]?.[1] ?? '{}') as {
      action?: string
      selection?: string
      query?: string
    }
    expect(body).toMatchObject({
      action: 'transaction.delete',
      selection: selection('selected'),
      query: 'mode=detail&v=1',
    })
    expect(recheck).toHaveBeenCalledWith(current.selection, expect.any(Function))
    expect(controller.state.projection?.total_groups).toBe(0)
    expect(controller.state.announcement).toContain('Staged deletion')
  })

  it('never replays a stale deletion automatically', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockResolvedValue(duplicates())
    const transport = mutationTransport(
      Response.json(
        {
          type: 'about:blank',
          title: 'Conflict',
          status: 409,
          detail: 'The profile changed.',
          code: 'revision_conflict',
          current_revision: '4',
        },
        { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
      ),
    )
    const recheck = vi.fn(async () => testProjection({ revision: '4' }))
    const controller = createDuplicateController({
      client,
      mutations: transport,
      host: { current: () => current, recheck },
    })

    await controller.open()
    controller.requestDelete()
    await expect(controller.confirmDelete()).resolves.toBe(false)
    expect(transport.request).toHaveBeenCalledTimes(1)
    expect(recheck).toHaveBeenCalled()
    expect(controller.state.phase).toBe('conflict')
    expect(controller.state.announcement).toContain('Invoke deletion again')
  })

  it('clears a stale duplicate selection and reloads the read projection once', async () => {
    const current = testProjection({ revision: '4' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockRejectedValueOnce(
        new MoneyflowProblem({
          type: 'about:blank',
          title: 'Conflict',
          status: 409,
          detail: 'The selection changed and must be reviewed.',
          code: 'selection_stale',
          current_revision: '4',
          selection: { kind: 'cleared', value: selection('empty') },
        } satisfies Problem),
      )
      .mockResolvedValueOnce(duplicates({ revision: '4' }))
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })

    await expect(controller.open()).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenCalledTimes(2)
    expect(client.projectDuplicates).toHaveBeenLastCalledWith(
      expect.objectContaining({ selection: selection('empty') }),
      undefined,
    )
    expect(controller.state.phase).toBe('ready')
  })

  it('advances the group window after exhausting rows in the current groups', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          total_transactions: 402,
          group_window: { offset: 0, limit: 200, count: 200 },
          row_window: { offset: 0, limit: 200, count: 2 },
          window_transactions: 2,
        }),
      )
      .mockResolvedValueOnce(
        duplicates({
          group_window: { offset: 200, limit: 200, count: 1 },
          row_window: { offset: 0, limit: 200, count: 2 },
        }),
      )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })

    await controller.open()
    await expect(controller.page(1)).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_window: { offset: 200, limit: 200 },
        row_window: { offset: 0, limit: 200 },
      }),
      undefined,
    )
  })

  it('advances without presenting an empty page when a row window is exactly full', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          group_window: { offset: 0, limit: 200, count: 200 },
          row_window: { offset: 0, limit: 200, count: 200 },
          window_transactions: 200,
        }),
      )
      .mockResolvedValueOnce(
        duplicates({
          group_window: { offset: 200, limit: 200, count: 1 },
          row_window: { offset: 0, limit: 200, count: 2 },
          window_transactions: 2,
        }),
      )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })

    await controller.open()
    await expect(controller.page(1)).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_window: { offset: 200, limit: 200 },
        row_window: { offset: 0, limit: 200 },
      }),
      undefined,
    )
  })

  it('returns to the terminal row page of the previous group window', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          group_window: { offset: 200, limit: 200, count: 1 },
          row_window: { offset: 0, limit: 200, count: 2 },
          window_transactions: 2,
        }),
      )
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          group_window: { offset: 0, limit: 200, count: 200 },
          row_window: { offset: 0, limit: 200, count: 200 },
          window_transactions: 400,
        }),
      )
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          group_window: { offset: 0, limit: 200, count: 200 },
          row_window: { offset: 200, limit: 200, count: 200 },
          window_transactions: 400,
        }),
      )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: vi.fn(async () => current),
      },
    })

    await controller.open()
    await expect(controller.page(-1)).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_window: { offset: 0, limit: 200 },
        row_window: { offset: 200, limit: 200 },
      }),
      undefined,
    )
  })

  it('permits only one mutation while a duplicate submission is pending', async () => {
    let resolveRequest!: (response: Response) => void
    const request = vi.fn(
      async () => await new Promise<Response>((resolve) => (resolveRequest = resolve)),
    )
    const current = testProjection({ revision: '3' })
    const next = testProjection({ revision: '4' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(duplicates())
      .mockResolvedValueOnce(duplicates({ revision: '4' }))
    const controller = createDuplicateController({
      client,
      mutations: { request },
      host: {
        current: () => current,
        recheck: vi.fn(async () => next),
      },
    })

    await controller.open()
    controller.requestDelete()
    const pending = controller.confirmDelete()
    await vi.waitFor(() => expect(controller.state.phase).toBe('submitting'))
    await expect(controller.confirmDelete()).resolves.toBe(false)
    await expect(controller.hideFocused()).resolves.toBe(false)
    await expect(controller.toggleFocused()).resolves.toBe(false)
    controller.requestDelete()
    expect(request).toHaveBeenCalledTimes(1)

    resolveRequest(Response.json({}))
    await expect(pending).resolves.toBe(true)
  })
})

describe('DuplicateController after a failed reload', () => {
  const conflict = () =>
    Response.json(
      {
        type: 'about:blank',
        title: 'Conflict',
        status: 409,
        detail: 'The profile changed.',
        code: 'revision_conflict',
        current_revision: '4',
      },
      { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
    )

  function controllerWith(transport: MutationFetch, recheck: () => Promise<unknown>) {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockResolvedValue(duplicates())
    return createDuplicateController({
      client,
      mutations: transport,
      host: {
        current: () => current,
        recheck: vi.fn(recheck) as DuplicateHost['recheck'],
      },
    })
  }

  it('leaves the confirmation when a conflict’s reload fails', async () => {
    const controller = controllerWith(mutationTransport(conflict()), async () => undefined)
    await controller.open()
    controller.requestDelete()

    await expect(controller.confirmDelete()).resolves.toBe(false)

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.confirmationCount).toBe(0)
    expect(controller.state.announcement).toBe(
      'The profile changed, and the duplicate list could not be refreshed. Press Retry.',
    )
  })

  it('leaves a closed review alone when a conflict’s reread ends after it closed', async () => {
    let answerReread!: (next: ViewProjection | undefined) => void
    const controller = controllerWith(
      mutationTransport(conflict()),
      () => new Promise((resolve) => (answerReread = resolve)),
    )
    await controller.open()
    controller.requestDelete()

    const deleting = controller.confirmDelete()
    await vi.waitFor(() => expect(answerReread).toBeDefined())
    controller.close()
    answerReread(testProjection({ revision: '4' }))
    await expect(deleting).resolves.toBe(false)

    expect(controller.state.phase).toBe('idle')
    expect(getFlashes().map(({ message }) => message)).toEqual([
      'The duplicate change could not be saved. The profile changed.',
    ])
    dismissFlash()
  })

  it('reports a staged hide whose reload failed as saved, not failed to save', async () => {
    const controller = controllerWith(mutationTransport(), async () => undefined)
    await controller.open()

    await expect(controller.hideFocused()).resolves.toBe(true)

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.announcement).toBe(
      'Staged visibility change for 1 transaction. The duplicate list could not be refreshed. ' +
        'Press Retry.',
    )
  })

  it('retries the reload and returns to the list', async () => {
    let attempts = 0
    const controller = controllerWith(mutationTransport(), async () => {
      attempts += 1
      if (attempts === 1) return undefined
      return testProjection({ revision: '4' })
    })
    await controller.open()
    await controller.hideFocused()

    await expect(controller.retry()).resolves.toBe(true)

    expect(controller.state.phase).toBe('ready')
    expect(controller.state.announcement).toBe('')
  })

  it('cancels the confirmation while the deletion is still being sent', async () => {
    let answer!: (response: Response) => void
    const transport: MutationFetch = {
      request: vi.fn(() => new Promise<Response>((resolve) => (answer = resolve))),
    }
    const controller = controllerWith(transport, async () => testProjection({ revision: '4' }))
    await controller.open()
    controller.requestDelete()

    const pending = controller.confirmDelete()
    controller.cancelDelete()
    expect(controller.state.confirmationCount).toBe(0)
    answer(Response.json({}))

    await expect(pending).resolves.toBe(true)
    expect(controller.state.phase).toBe('ready')
  })

  it('keeps a selection made after closing review while the deletion was sent', async () => {
    let shown = testProjection({ revision: '3', selection: selection('before') })
    let answer!: (response: Response) => void
    const transport: MutationFetch = {
      request: vi.fn(() => new Promise<Response>((resolve) => (answer = resolve))),
    }
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockResolvedValue(duplicates())
    // Like the view controller, a recheck rereads the view with the selection it is given.
    const recheck = vi.fn(async (requested?: SelectionValue) => {
      shown = testProjection({ revision: '4', selection: requested ?? shown.selection })
      return shown
    })
    const controller = createDuplicateController({
      client,
      mutations: transport,
      host: { current: () => shown, recheck },
    })
    await controller.open()
    controller.requestDelete()

    const pending = controller.confirmDelete()
    controller.cancelDelete()
    controller.close()
    shown = testProjection({ revision: '3', selection: selection('newer') })
    answer(Response.json({}))

    await expect(pending).resolves.toBe(true)
    expect(recheck).toHaveBeenCalledOnce()
    expect(shown.revision).toBe('4')
    expect(shown.selection).toBe(selection('newer'))
  })

  it('names the server’s reason when the change could not be saved', async () => {
    const busy = Response.json(
      {
        type: 'about:blank',
        title: 'Service Unavailable',
        status: 503,
        detail: 'The profile is busy.',
        code: 'store_busy',
      },
      { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
    )
    const controller = controllerWith(mutationTransport(busy), async () => undefined)
    await controller.open()

    await expect(controller.hideFocused()).resolves.toBe(false)

    expect(controller.state.announcement).toBe(
      'The pending change could not be saved. The profile is busy.',
    )
  })
})

describe('DuplicateController after review closes or reopens', () => {
  /** A controller whose duplicate lists and view rereads each wait until the test answers. */
  function deferredController() {
    const lists: ((response: DuplicateResponse) => void)[] = []
    const rereads: ((next: ViewProjection | undefined) => void)[] = []
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockImplementation(
      () => new Promise<DuplicateResponse>((resolve) => lists.push(resolve)),
    )
    const current = testProjection({ revision: '3' })
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: {
        current: () => current,
        recheck: () => new Promise((resolve) => rereads.push(resolve)),
      },
    })
    return { controller, lists, rereads, client }
  }

  /** A controller in the failed phase after a staged hide whose reread failed. */
  async function failedController() {
    const deferred = deferredController()
    const opening = deferred.controller.open()
    deferred.lists.shift()!(duplicates())
    await opening
    const hiding = deferred.controller.hideFocused()
    await vi.waitFor(() => expect(deferred.rereads).toHaveLength(1))
    deferred.rereads.shift()!(undefined)
    await hiding
    expect(deferred.controller.state.phase).toBe('failed')
    return deferred
  }

  it('leaves a closed review idle when its retry reread fails late', async () => {
    const { controller, rereads } = await failedController()

    const retrying = controller.retry()
    await vi.waitFor(() => expect(rereads).toHaveLength(1))
    controller.close()
    rereads.shift()!(undefined)

    await expect(retrying).resolves.toBe(false)
    expect(controller.state.phase).toBe('idle')
  })

  it('leaves a closed review idle when its retry list arrives late', async () => {
    const { controller, lists, rereads } = await failedController()

    const retrying = controller.retry()
    await vi.waitFor(() => expect(rereads).toHaveLength(1))
    rereads.shift()!(testProjection({ revision: '4' }))
    await vi.waitFor(() => expect(lists).toHaveLength(1))
    controller.close()
    lists.shift()!(duplicates({ total_groups: 9 }))

    await expect(retrying).resolves.toBe(false)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.projection).toBeUndefined()
  })

  it('leaves a closed review idle when a selection toggle fails late', async () => {
    const { controller, lists, client } = deferredController()
    const opening = controller.open()
    lists.shift()!(duplicates())
    await opening
    let refuse!: (error: unknown) => void
    vi.mocked(client.transition).mockImplementationOnce(
      () => new Promise((_, reject) => (refuse = reject)),
    )

    const toggling = controller.toggleFocused()
    controller.close()
    refuse(new TypeError('Failed to fetch'))

    await expect(toggling).resolves.toBe(false)
    expect(controller.state.phase).toBe('idle')
  })

  it.each([
    ['before', true],
    ['after', false],
  ])('keeps a reopened review when the first list arrives %s the new one', async (_, lateFirst) => {
    const { controller, lists } = deferredController()
    const first = controller.open()
    controller.close()
    const second = controller.open()
    expect(lists).toHaveLength(2)
    const [answerFirst, answerSecond] = [lists[0]!, lists[1]!]

    if (lateFirst) {
      answerFirst(duplicates({ total_groups: 9 }))
      await expect(first).resolves.toBe(false)
      expect(controller.state.phase).toBe('loading')
    }
    answerSecond(duplicates())
    await expect(second).resolves.toBe(true)
    if (!lateFirst) {
      answerFirst(duplicates({ total_groups: 9 }))
      await expect(first).resolves.toBe(false)
    }

    expect(controller.state.phase).toBe('ready')
    expect(controller.state.projection?.total_groups).toBe(1)
  })
})

function duplicateClient(): MoneyflowClient {
  return {
    mutations: mutationTransport(),
    view: vi.fn(),
    transition: vi.fn(),
    insights: vi.fn(),
    projectDuplicates: vi.fn(),
    previewExport: vi.fn(),
    downloadExport: vi.fn(),
    providerStatus: vi.fn(),
    providerWriteStatus: vi.fn(),
  }
}

function mutationTransport(response = Response.json({})): MutationFetch {
  return { request: vi.fn(async () => response.clone()) }
}

function selection(value: string): SelectionValue {
  return `mfsel1.${value}` as SelectionValue
}
