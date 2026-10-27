import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createMoneyflowClient } from '../api/client'
import { createViewController } from '../controller/view-controller.svelte'
import type { ViewStateAdapter } from '../controller/view-state'
import { appears } from '../../test/dom'
import { notices } from '../../test/notices'
import { testAggregateRow, testProjection } from '../../test/projection'
import { providerCapability, testProviderStatus } from '../../test/provider'
import { MoneyflowWorkspace, type ViewID } from './index'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'
const api = 'http://localhost:3000/embedded/moneyflow/api/v1/'

describe('MoneyflowWorkspace', () => {
  afterEach(() => {
    cleanup()
  })

  it('works through the host fetch and keeps its view in the host adapter', async () => {
    const capabilities = [
      { id: 'view.set-grouping', key_display: '1-5', description: 'Group by', category: 'Views' },
      {
        id: 'view.show-detail',
        key_display: 'd',
        description: 'All transactions',
        category: 'Views',
      },
    ].map((capability) => ({ ...capability, available: true }))
    const categories = testProjection({
      canonical_query: 'group=category&v=1',
      capabilities,
      view: { ...testProjection().view, mode: 'aggregate', grouping: 'category' },
      detail_rows: [],
      aggregate_rows: [testAggregateRow({ dimension: 'category', label: 'Example Category' })],
    })
    const requests: Request[] = []
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      requests.push(request)
      const path = request.url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') return Response.json(testProjection({ capabilities }))
      if (path === 'view/transition') return Response.json(categories)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const viewState = memoryViewState()
    const located = Promise.withResolvers<void>()
    const onlocation = vi.fn((view?: string) => {
      if (view === 'category') located.resolve()
    })
    const { component } = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState,
      onlocation,
    })
    // Asked for while the workspace loads, the view opens once it has loaded.
    component.open('category')

    await located.promise
    expect(onlocation).toHaveBeenLastCalledWith('category')
    expect(viewState.entries.map(({ query }) => query)).toEqual(['v=1', 'group=category&v=1'])
    expect(screen.queryByRole('main')).toBeNull()
    expect(requests.map((request) => request.url)).toContain(
      `${api}profiles/${profileID}/view/transition`,
    )
    for (const request of requests) {
      expect(request.url.startsWith(api)).toBe(true)
      expect(request.headers.get('X-Moneyflow-Mutation-Token')).toBeNull()
      expect(request.credentials).toBe('same-origin')
      expect(request.redirect).toBe('error')
    }
  })

  it('opens a view asked for before the workspace has mounted', async () => {
    const categories = testProjection({
      canonical_query: 'group=category&v=1',
      view: { ...testProjection().view, mode: 'aggregate', grouping: 'category' },
      detail_rows: [],
      aggregate_rows: [testAggregateRow({ dimension: 'category', label: 'Example Category' })],
    })
    const capabilities = [
      { id: 'view.set-grouping', key_display: '1-5', description: 'Group by', category: 'Views' },
    ].map((capability) => ({ ...capability, available: true }))
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') return Response.json(testProjection({ capabilities }))
      if (path === 'view/transition') return Response.json({ ...categories, capabilities })
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const located = Promise.withResolvers<void>()
    // A host that mounts the workspace itself can ask before Svelte has run any effect.
    const workspace = mount(MoneyflowWorkspace, {
      target: document.body.appendChild(document.createElement('div')),
      props: {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
        onlocation: (view?: string) => {
          if (view === 'category') located.resolve()
        },
      },
    })
    try {
      workspace.open('category')
      await located.promise
    } finally {
      await unmount(workspace)
    }
  })

  it('opens the review asked for before the workspace has mounted', async () => {
    const pending = { active_operations: 1, inactive_operations: 0, affected_transactions: 1 }
    const capabilities = [
      { id: 'changes.review', key_display: 'w', description: 'Review changes', category: 'Edit' },
    ].map((capability) => ({ ...capability, available: true }))
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') return Response.json(testProjection({ capabilities, pending }))
      if (path === 'review') {
        return Response.json({
          revision: '0',
          pending,
          active_operations: [
            {
              operation_id: 'operation-a',
              type: 'merchant.label',
              label: 'Rename merchant',
              active: true,
              affected_count: 1,
              before: 'Old Merchant',
              after: 'New Merchant',
            },
          ],
          inactive_operations: [],
        })
      }
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const workspace = mount(MoneyflowWorkspace, {
      target: document.body.appendChild(document.createElement('div')),
      props: {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
      },
    })
    try {
      workspace.review()
      const drawer = await appears(() =>
        screen.queryByRole('dialog', { name: 'Review pending changes' }),
      )
      await appears(() => within(drawer).queryByRole('button', { name: /^Rename merchant/ }))
    } finally {
      await unmount(workspace)
    }
  })

  it('shows a running write, not the review, when the write status answers after the view', async () => {
    const pending = { active_operations: 1, inactive_operations: 0, affected_transactions: 1 }
    const capabilities = [
      { id: 'changes.review', key_display: 'w', description: 'Review changes', category: 'Edit' },
    ].map((capability) => ({ ...capability, available: true }))
    const asked = Promise.withResolvers<void>()
    const answer = Promise.withResolvers<object>()
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') {
        return Response.json(testProjection({ capabilities, pending, profile_kind: 'monarch' }))
      }
      if (path === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
      if (path === 'provider/write-status') {
        asked.resolve()
        return Response.json(await answer.promise)
      }
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const workspace = mount(MoneyflowWorkspace, {
      target: document.body.appendChild(document.createElement('div')),
      props: {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
      },
    })
    try {
      workspace.review()
      await asked.promise
      await new Promise((resolve) => setTimeout(resolve))
      expect(screen.queryByRole('dialog', { name: 'Review pending changes' })).toBeNull()

      answer.resolve({ ...writing, actions: [] })

      await appears(() => screen.queryByRole('dialog', { name: 'Monarch write status' }))
      expect(screen.queryByRole('dialog', { name: 'Review pending changes' })).toBeNull()
    } finally {
      await unmount(workspace)
    }
  })

  describe('an initial view from the host', () => {
    const capabilities = [
      { id: 'view.set-grouping', key_display: '1-5', description: 'Group by', category: 'Views' },
      {
        id: 'view.show-detail',
        key_display: 'd',
        description: 'All transactions',
        category: 'Views',
      },
    ].map((capability) => ({ ...capability, available: true }))
    const categories = testProjection({
      canonical_query: 'group=category&v=1',
      capabilities,
      view: { ...testProjection().view, mode: 'aggregate', grouping: 'category' },
      detail_rows: [],
      aggregate_rows: [testAggregateRow({ dimension: 'category', label: 'Example Category' })],
    })

    /** Renders a workspace whose first view is all transactions, and records its requests. */
    function renderWorkspace(initialView: ViewID, viewState = memoryViewState()) {
      const paths: string[] = []
      const onlocation = vi.fn<(view?: ViewID) => void>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
        paths.push(path)
        if (path === 'view') return Response.json(testProjection({ capabilities }))
        if (path === 'view/transition') return Response.json(categories)
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
        initialView,
        onlocation,
      })
      return { paths, onlocation }
    }

    it('opens when the history entry has no view', async () => {
      const { paths, onlocation } = renderWorkspace('category')

      await vi.waitFor(() => expect(onlocation).toHaveBeenLastCalledWith('category'))
      expect(paths.filter((path) => path === 'view/transition')).toHaveLength(1)
    })

    it('opens in the busiest currency of a profile whose first view has none', async () => {
      // The first view has no currency, so the workspace scopes it to the currency most
      // transactions use. The server answers each change from the view it starts from.
      const currencies = [
        { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' },
      ]
      const scoped = (projection: ReturnType<typeof testProjection>) => ({
        ...projection,
        canonical_query: `cur=USD%3A2&${projection.canonical_query}`,
        view: { ...projection.view, currency: { currency: 'USD', scale: 2 } },
      })
      const first = testProjection({ capabilities, currencies })
      const grouped = { ...categories, currencies }
      const changes: string[] = []
      const onlocation = vi.fn<(view?: ViewID) => void>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const request = new Request(input, init)
        const path = request.url.replace(`${api}profiles/${profileID}/`, '')
        if (path === 'view') return Response.json(first)
        if (path !== 'view/transition') {
          return Response.json(
            { status: 404, code: 'not_found', detail: 'Not found.' },
            { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
          )
        }
        const { action, query } = (await request.json()) as { action: string; query: string }
        changes.push(`${action} ${query}`)
        if (action === 'view.set-currency') {
          // A slow answer checks that the host's view change waits for the currency.
          await new Promise((resolve) => setTimeout(resolve, 20))
          return Response.json(scoped(first))
        }
        return Response.json(query.includes('cur=') ? scoped(grouped) : grouped)
      })
      render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
        initialView: 'category',
        onlocation,
      })

      await vi.waitFor(() => expect(changes).toHaveLength(2))
      await new Promise((resolve) => setTimeout(resolve, 50))
      expect(changes).toEqual(['view.set-currency v=1', 'view.set-grouping cur=USD%3A2&v=1'])
      expect(onlocation).toHaveBeenLastCalledWith('category')
    })

    it('sends no view change when the first view is that view', async () => {
      const { paths, onlocation } = renderWorkspace('detail')

      await vi.waitFor(() => expect(onlocation).toHaveBeenCalledWith('detail'))
      await new Promise((resolve) => setTimeout(resolve))
      expect(paths).not.toContain('view/transition')
    })

    it('leaves a history entry that has a view alone', async () => {
      const viewState = memoryViewState()
      viewState.entries[0] = { state: null, query: 'v=1' }
      const { paths, onlocation } = renderWorkspace('category', viewState)

      await vi.waitFor(() => expect(onlocation).toHaveBeenCalledWith('detail'))
      await new Promise((resolve) => setTimeout(resolve))
      expect(paths).not.toContain('view/transition')
    })

    it('gives way to Back taken while the first view loads', async () => {
      const viewState = memoryViewState()
      viewState.entries.unshift({ state: null, query: 'v=1' })
      viewState.go(1)
      const firstView = Promise.withResolvers<void>()
      const paths: string[] = []
      const onlocation = vi.fn<(view?: ViewID) => void>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const request = new Request(input, init)
        const path = request.url.replace(`${api}profiles/${profileID}/`, '')
        paths.push(path)
        if (path === 'view') {
          const { query } = (await request.json()) as { query: string }
          if (query === '') await firstView.promise
          return Response.json(testProjection({ capabilities }))
        }
        if (path === 'view/transition') return Response.json(categories)
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
        initialView: 'category',
        onlocation,
      })
      await vi.waitFor(() => expect(paths).toContain('view'))

      viewState.go(-1)
      for (const listener of viewState.listeners) listener()
      firstView.resolve()

      await vi.waitFor(() => expect(onlocation).toHaveBeenCalledWith('detail'))
      await new Promise((resolve) => setTimeout(resolve))
      expect(paths).not.toContain('view/transition')
      expect(viewState.entries).toHaveLength(2)
    })

    // A maintain view opens over the grid, so opening it before the asked-for view would cover it.
    it.each([
      { when: 'before the workspace has mounted', mounted: false },
      { when: 'while the first view loads', mounted: true },
    ])('gives way to a view the host asks for $when', async ({ mounted }) => {
      const manage = [
        ...capabilities,
        {
          id: 'category.manage',
          key_display: 'C',
          description: 'Manage categories',
          category: 'Edit',
          available: true,
        },
      ]
      const firstView = Promise.withResolvers<void>()
      const located = Promise.withResolvers<void>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
        if (path === 'view') {
          await firstView.promise
          return Response.json(testProjection({ capabilities: manage }))
        }
        if (path === 'view/transition')
          return Response.json({ ...categories, capabilities: manage })
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      const workspace = mount(MoneyflowWorkspace, {
        target: document.body.appendChild(document.createElement('div')),
        props: {
          apiBase: '/embedded/moneyflow/',
          fetch: hostFetch,
          profileID,
          viewState: memoryViewState(),
          initialView: 'categories',
          onlocation: (view?: ViewID) => {
            if (view === 'category') located.resolve()
          },
        },
      })
      try {
        if (mounted) flushSync()
        workspace.open('category')
        firstView.resolve()

        await located.promise
        await new Promise((resolve) => setTimeout(resolve))
        expect(screen.queryByRole('dialog', { name: 'Manage categories' })).toBeNull()
      } finally {
        await unmount(workspace)
      }
    })
  })

  it.each([
    {
      type: 'about:blank',
      title: 'Service Unavailable',
      status: 503,
      code: 'profile_busy',
      detail: 'The profile is busy. Try again shortly.',
    },
    {
      type: 'about:blank',
      title: 'Not Found',
      status: 404,
      code: 'not_found',
      detail: 'The profile does not exist.',
    },
  ])('shows the server detail when the first view answers $code', async (problem) => {
    const hostFetch = vi.fn<typeof fetch>(async () =>
      Response.json(problem, {
        status: problem.status,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    )
    render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState: memoryViewState(),
    })

    const detail = await appears(() => screen.queryByText(problem.detail))
    const heading = screen.getByRole('heading', { level: 2, name: "Couldn't load this profile" })
    expect(detail.closest('section')).toBe(heading.closest('section'))
    expect(screen.queryByText('Profile problem')).toBeNull()
  })

  it('shows the current history entry when it mounts again with a kept controller', async () => {
    const capabilities = [
      { id: 'view.set-grouping', key_display: '1-5', description: 'Group by', category: 'Views' },
    ].map((capability) => ({ ...capability, available: true }))
    const categories = testProjection({
      canonical_query: 'group=category&v=1',
      capabilities,
      view: { ...testProjection().view, mode: 'aggregate', grouping: 'category' },
      detail_rows: [],
      aggregate_rows: [testAggregateRow({ dimension: 'category', label: 'Example Category' })],
    })
    let asked = Promise.withResolvers<string>()
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      const path = request.url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') {
        asked.resolve(((await request.json()) as { query: string }).query)
        return Response.json(testProjection({ capabilities }))
      }
      if (path === 'view/transition') return Response.json(categories)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const viewState = memoryViewState()
    // Standalone moneyflow keeps its controller while setup replaces the workspace.
    const controller = createViewController({
      client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
      viewState,
      recheckDebounceMillis: 0,
    })
    const props = { apiBase: '/embedded/moneyflow/', fetch: hostFetch, profileID, viewState }
    const located = Promise.withResolvers<void>()
    const first = render(MoneyflowWorkspace, {
      ...props,
      controller,
      onlocation: (view?: string) => {
        if (view === 'category') located.resolve()
      },
    })
    first.component.open('category')
    await located.promise
    first.unmount()

    // Back while setup shows, then setup closes.
    viewState.go(-1)
    asked = Promise.withResolvers<string>()
    render(MoneyflowWorkspace, { ...props, controller })

    expect(await asked.promise).toBe('v=1')
  })

  it('leaves history to the host once the host removes it', async () => {
    const asked = Promise.withResolvers<AbortSignal>()
    const answer = Promise.withResolvers<Response>()
    // A host fetch that answers even after its request was canceled.
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      asked.resolve(new Request(input, init).signal)
      return await answer.promise
    })
    const viewState = memoryViewState()
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState,
    })
    const signal = await asked.promise
    workspace.unmount()

    // The host shows another page, and then the first view answers. With more rows than one
    // window, a view that loaded would go on to read the next window.
    viewState.push({ page: 'host' }, '')
    answer.resolve(Response.json(testProjection({ total_rows: 400 })))
    await new Promise((resolve) => setTimeout(resolve))

    expect({
      aborted: signal.aborted,
      entries: viewState.entries,
      requests: hostFetch.mock.calls.length,
    }).toEqual({
      aborted: true,
      entries: [
        { state: null, query: '' },
        { state: { page: 'host' }, query: '' },
      ],
      requests: 1,
    })
  })

  it('stops following its refresh once the host removes it', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    try {
      const capabilities = [providerCapability(true)]
      const asked = Promise.withResolvers<void>()
      const answer = Promise.withResolvers<Response>()
      let statusChecks = 0
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const request = new Request(input, init)
        const path = request.url.replace(`${api}profiles/${profileID}/`, '')
        if (path === 'view') return Response.json(testProjection({ capabilities }))
        if (path === 'provider/status') {
          statusChecks += 1
          return Response.json(testProviderStatus({ revision: '0', refresh_due: 'recent' }))
        }
        if (path === 'provider/refresh') {
          asked.resolve()
          return await answer.promise
        }
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      const viewState = memoryViewState()
      const workspace = render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
        autoRefresh: true,
      })
      await asked.promise
      workspace.unmount()

      // The host shows another page while the refresh runs, and then the refresh answers.
      const shown = [...viewState.entries]
      viewState.push({ page: 'host' }, '')
      const checked = statusChecks
      await vi.advanceTimersByTimeAsync(1_000)
      const refreshed = testProjection({ revision: '1', capabilities })
      answer.resolve(
        Response.json({
          version: '1',
          revision: '1',
          generation: '2',
          status: testProviderStatus({ revision: '1', generation: '2' }),
          projection: refreshed,
          selection: { kind: 'preserved', value: refreshed.selection },
        }),
      )
      await new Promise((resolve) => setTimeout(resolve))

      expect({ statusChecks, entries: viewState.entries }).toEqual({
        statusChecks: checked,
        entries: [...shown, { state: { page: 'host' }, query: '' }],
      })
    } finally {
      vi.useRealTimers()
    }
  })

  // A host's server runs a refresh on after its request ends, so Cancel could not stop it.
  describe('a refresh in a workspace that starts due refreshes', () => {
    function embedded(status: Parameters<typeof testProviderStatus>[0]) {
      const capabilities = [providerCapability(true)]
      const refreshes: AbortSignal[] = []
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const request = new Request(input, init)
        const path = request.url.replace(`${api}profiles/${profileID}/`, '')
        if (path === 'view') {
          return Response.json(testProjection({ profile_kind: 'monarch', capabilities }))
        }
        if (path === 'provider/status') {
          return Response.json(testProviderStatus({ provider_kind: 'monarch', ...status }))
        }
        if (path === 'provider/write-status') return Response.json(idle)
        if (path === 'provider/refresh') {
          refreshes.push(request.signal)
          return await canceled(request.signal)
        }
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
        autoRefresh: true,
      })
      return { refreshes }
    }

    it('offers Close, not Cancel, and the refresh runs on in the status strip', async () => {
      const { refreshes } = embedded({ refresh_due: 'recent' })
      const dialog = await appears(() =>
        screen.queryByRole('dialog', { name: 'Refreshing from Monarch' }),
      )

      expect(within(dialog).queryByRole('button', { name: /^Cancel\b/ })).toBeNull()
      await fireEvent.click(within(dialog).getByRole('button', { name: 'Close Esc' }))

      expect(screen.queryByRole('dialog')).toBeNull()
      expect(document.body.textContent).not.toContain('Stopping the refresh…')
      expect(refreshes.map(({ aborted }) => aborted)).toEqual([false])
      expect(screen.getByRole('button', { name: 'Refreshing…' })).not.toBeNull()
    })

    it('follows another interface’s refresh in the status strip, without a dialog', async () => {
      const { refreshes } = embedded({
        refreshing: true,
        refresh_due: 'recent',
        owner_renderer: 'web',
      })
      await appears(() => screen.queryByRole('button', { name: 'Refreshing…' }))

      expect(screen.queryByRole('dialog')).toBeNull()
      expect(refreshes).toEqual([])
    })
  })

  it('leaves a provider write alone once the host removes it', async () => {
    const asked = Promise.withResolvers<void>()
    const answer = Promise.withResolvers<Response>()
    const paths: string[] = []
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
      paths.push(path)
      if (path === 'view') return Response.json(testProjection())
      if (path === 'provider/write-status') {
        asked.resolve()
        return await answer.promise
      }
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState: memoryViewState(),
    })
    await asked.promise
    workspace.unmount()

    // The status check answers after the host removed the workspace, with a write to resume.
    answer.resolve(Response.json(writing))
    await new Promise((resolve) => setTimeout(resolve))

    expect(paths).not.toContain('provider/write/resume')
  })

  it('keeps a controller it was given running after it unmounts', async () => {
    const paths: string[] = []
    let writeStarted = false
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
      paths.push(path)
      if (path === 'view') return Response.json(testProjection())
      if (path === 'view/transition') {
        return Response.json(testProjection({ canonical_query: 'group=category&v=1' }))
      }
      if (path === 'provider/write-status' && writeStarted) return Response.json(writing)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const viewState = memoryViewState()
    // Standalone moneyflow keeps its controller while setup replaces the workspace.
    const controller = createViewController({
      client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
      viewState,
    })
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState,
      controller,
    })
    await appears(() => document.getElementById('moneyflow-row-row-0'))
    workspace.unmount()

    await controller.apply({ action: 'view.set-grouping', dimension: 'category' })
    expect(viewState.entries.map(({ query }) => query)).toEqual(['v=1', 'group=category&v=1'])
    // A provider write that starts later still goes on.
    writeStarted = true
    await controller.providerWrite.poll()
    expect(paths).toContain('provider/write/resume')
  })

  describe('an edit key waiting behind a view change when the host removes it', () => {
    /**
     * Presses g, and then h while the grouping change is on its way. The change fails once it is
     * canceled, as a browser's request does, or answers when `regroup` is called.
     */
    async function hideWaiting(kept: boolean) {
      const hide = { id: 'transaction.toggle-hidden', key_display: 'h', description: 'Hide' }
      const capabilities = [grouping, { ...hide, category: 'Edit', available: true }]
      const paths: string[] = []
      const changing = Promise.withResolvers<void>()
      const regrouped = Promise.withResolvers<Response>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const request = new Request(input, init)
        const path = request.url.replace(`${api}profiles/${profileID}/`, '')
        paths.push(path)
        if (path === 'view') return Response.json(testProjection({ capabilities }))
        if (path === 'view/transition') {
          changing.resolve()
          return await Promise.race([regrouped.promise, canceled(request.signal)])
        }
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      const viewState = memoryViewState()
      // Standalone moneyflow keeps its controller while setup replaces the workspace.
      const controller = kept
        ? createViewController({
            client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
            viewState,
          })
        : undefined
      const workspace = render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
        controller,
      })
      await appears(() => document.getElementById('moneyflow-row-row-0'))
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      await fireEvent.keyDown(grid, { key: 'g' })
      await changing.promise
      await fireEvent.keyDown(grid, { key: 'h' })
      const regroup = () =>
        regrouped.resolve(
          Response.json(testProjection({ capabilities, canonical_query: 'group=category&v=1' })),
        )
      return { workspace, controller, paths, regroup }
    }

    it('sends no edit after the workspace is gone', async () => {
      const { workspace, paths } = await hideWaiting(false)
      const sent = paths.length

      workspace.unmount()
      await new Promise((resolve) => setTimeout(resolve))

      expect(paths.slice(sent)).toEqual([])
    })

    it('drops the edit for a kept controller too, which still takes a dialog edit', async () => {
      const { workspace, controller, paths, regroup } = await hideWaiting(true)
      const sent = paths.length

      workspace.unmount()
      regroup()
      await controller!.settled()
      await new Promise((resolve) => setTimeout(resolve))
      // A dialog's edit, as after setup closes and the workspace shows again.
      await controller!.editing.submit({ action: 'category.manage', input: {} })

      expect(paths.slice(sent)).toEqual(['mutations'])
    })
  })

  it.each([
    { owner: 'its own controller', kept: false, sent: [] },
    // The dialog's edit after the removal is the one sent.
    { owner: 'a kept controller', kept: true, sent: ['mutations'] },
  ])(
    'sends no edit for a key whose turn came as the host removed it, with $owner',
    async ({ kept, sent }) => {
      const hide = { id: 'transaction.toggle-hidden', key_display: 'h', description: 'Hide' }
      const capabilities = [{ ...hide, category: 'Edit', available: true }]
      const paths: string[] = []
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
        paths.push(path)
        if (path === 'view') return Response.json(testProjection({ capabilities }))
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      const viewState = memoryViewState()
      const controller = kept
        ? createViewController({
            client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
            viewState,
          })
        : undefined
      const workspace = render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
        controller,
      })
      await appears(() => document.getElementById('moneyflow-row-row-0'))
      const before = paths.length

      void fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'h' })
      // With no view change queued, the key's turn comes in the next microtask, and its edit
      // waits there for the shell to draw. The host removes the workspace in between.
      await Promise.resolve()
      workspace.unmount()
      await new Promise((resolve) => setTimeout(resolve))
      await controller?.editing.submit({ action: 'category.manage', input: {} })

      expect(paths.slice(before)).toEqual(sent)
    },
  )

  it('sends no view change still waiting its turn once the host removes it', async () => {
    const capabilities = [grouping]
    const currencies = [
      { currency: 'USD', scale: 2, count: 1, first: '2024-01-02', latest: '2024-01-02' },
    ]
    const paths: string[] = []
    const opening = Promise.withResolvers<void>()
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      const path = request.url.replace(`${api}profiles/${profileID}/`, '')
      paths.push(path)
      if (path === 'view') return Response.json(testProjection({ capabilities, currencies }))
      if (path === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
      if (path === 'provider/write-status') return Response.json(idle)
      if (path !== 'view/transition') {
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      }
      const { action } = (await request.json()) as { action: string }
      // Were g's change sent, its answer would show a warning and read more rows.
      if (action !== 'view.set-currency') {
        return Response.json(
          testProjection({
            canonical_query: 'group=category&v=1',
            total_rows: 400,
            warnings: [{ code: 'example_warning', detail: 'Example warning.' }],
          }),
        )
      }
      opening.resolve()
      return await canceled(request.signal)
    })
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState: memoryViewState(),
    })
    // The first view opens in its currency, and g waits for that.
    await opening.promise
    await appears(() => document.getElementById('moneyflow-row-row-0'))
    await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'g' })
    const sent = paths.length

    workspace.unmount()
    await new Promise((resolve) => setTimeout(resolve))

    expect({ requests: paths.slice(sent), ...notices() }).toEqual({
      requests: [],
      banners: [],
      status: '',
    })
  })

  it('applies no period pick still being decided to a controller kept while setup shows', async () => {
    const base = testProjection()
    const dated = {
      currencies: [
        { currency: 'USD', scale: 2, count: 1, first: '2026-01-02', latest: '2026-01-02' },
      ],
      view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
    }
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      const path = request.url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') return Response.json(testProjection(dated))
      if (path === 'view/transition') {
        return Response.json(testProjection({ ...dated, canonical_query: 'period=2026-01&v=1' }))
      }
      if (path === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
      if (path === 'provider/write-status') return Response.json(idle)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const viewState = memoryViewState()
    // Standalone moneyflow keeps its controller while setup replaces the workspace.
    const controller = createViewController({
      client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
      viewState,
    })
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState,
      controller,
    })
    await appears(() => document.getElementById('moneyflow-row-row-0'))
    // Held work, such as a search still being applied after focus left it, makes a Month pick
    // wait to decide from the view that work leaves.
    const held = Promise.withResolvers<void>()
    void controller.hold(held.promise)
    await fireEvent.click(screen.getByRole('radio', { name: 'Month' }))

    workspace.unmount()
    held.resolve()
    await controller.settled()
    // The pick decides in the microtasks after the held work; then any change it sent finishes.
    await new Promise((resolve) => setTimeout(resolve))
    await controller.settled()

    expect({
      view: controller.projection?.canonical_query,
      location: viewState.entries.map(({ query }) => query),
    }).toEqual({ view: 'v=1', location: ['v=1'] })
  })

  it('applies no search left mid-preview to a controller kept while setup shows', async () => {
    const previewing = Promise.withResolvers<void>()
    const answer = Promise.withResolvers<void>()
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      const path = request.url.replace(`${api}profiles/${profileID}/`, '')
      if (path === 'view') return Response.json(testProjection())
      if (path === 'view/transition') {
        previewing.resolve()
        await answer.promise
        const view = { ...testProjection().view, search: 'noodle' }
        return Response.json(testProjection({ canonical_query: 'search=noodle&v=1', view }))
      }
      if (path === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
      if (path === 'provider/write-status') return Response.json(idle)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })
    const viewState = memoryViewState()
    // Standalone moneyflow keeps its controller while setup replaces the workspace.
    const controller = createViewController({
      client: createMoneyflowClient('/embedded/moneyflow/', profileID, hostFetch),
      viewState,
    })
    const workspace = render(MoneyflowWorkspace, {
      apiBase: '/embedded/moneyflow/',
      fetch: hostFetch,
      profileID,
      viewState,
      controller,
    })
    await appears(() => document.getElementById('moneyflow-row-row-0'))
    const field = screen.getByRole('searchbox', { name: 'Search transactions' })
    field.focus()
    await fireEvent.input(field, { target: { value: 'noodle' } })
    await previewing.promise
    // Leaving the field applies its search once the preview answers; the leave holds the view.
    await fireEvent.focusOut(field, { relatedTarget: document.body })

    workspace.unmount()
    answer.resolve()
    await controller.settled()

    expect({
      view: controller.projection?.canonical_query,
      location: viewState.entries.map(({ query }) => query),
    }).toEqual({ view: 'v=1', location: ['v=1'] })
  })

  describe('an answer that arrives after the host removed the workspace', () => {
    const busy = () =>
      Response.json(
        {
          type: 'about:blank',
          title: 'Service Unavailable',
          status: 503,
          code: 'store_busy',
          detail: 'The profile is busy. Try again.',
        },
        { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
      )
    const refused = (code: string) =>
      Response.json(
        { type: 'about:blank', title: 'Conflict', status: 409, code, detail: 'It changed.' },
        { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
      )
    const stale = () => refused('selection_stale')
    const merchant = { id: 'transaction.edit-merchant', key_display: 'm' }
    const answered = testProjection({
      revision: '1',
      total_rows: 400,
      warnings: [{ code: 'example_warning', detail: 'Example warning.' }],
    })

    it.each([
      {
        request: 'an edit that is saved',
        key: { key: 'h' },
        capability: { id: 'transaction.toggle-hidden', key_display: 'h' },
        path: 'mutations',
        answer: () =>
          Response.json({
            version: '1',
            revision: '1',
            canonical_query: answered.canonical_query,
            projection: answered,
            pending: answered.pending,
            selection: { kind: 'preserved', value: answered.selection },
          }),
      },
      {
        request: 'an edit the server refuses',
        key: { key: 'h' },
        capability: { id: 'transaction.toggle-hidden', key_display: 'h' },
        path: 'mutations',
        answer: busy,
      },
      {
        request: 'an export preview that fails',
        key: { key: 'E', shiftKey: true },
        capability: { id: 'transactions.export', key_display: 'E' },
        path: 'export/preview',
        answer: busy,
      },
      {
        // While the workspace stands, this answer has the preview sent once more.
        request: 'a merchant preview whose selection went stale',
        key: { key: 'm' },
        capability: merchant,
        path: 'merchant-preview',
        answer: stale,
      },
    ])(
      'shows nothing and sends nothing for $request',
      async ({ key, capability, path, answer }) => {
        const capabilities = [
          { ...capability, description: 'Key', category: 'Edit', available: true },
        ]
        const paths: string[] = []
        const asked = Promise.withResolvers<void>()
        const late = Promise.withResolvers<Response>()
        const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
          const route = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
          paths.push(route)
          if (route === 'view') return Response.json(testProjection({ capabilities }))
          if (route === 'provider/status')
            return Response.json(testProviderStatus({ revision: '0' }))
          if (route === 'provider/write-status') return Response.json(idle)
          if (route !== path) {
            return Response.json(
              { status: 404, code: 'not_found', detail: 'Not found.' },
              { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
            )
          }
          asked.resolve()
          return await late.promise
        })
        const workspace = render(MoneyflowWorkspace, {
          apiBase: '/embedded/moneyflow/',
          fetch: hostFetch,
          profileID,
          viewState: memoryViewState(),
        })
        await appears(() => document.getElementById('moneyflow-row-row-0'))
        await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), key)
        await asked.promise
        const sent = paths.length

        workspace.unmount()
        late.resolve(answer())
        await new Promise((resolve) => setTimeout(resolve))

        expect({ requests: paths.slice(sent), ...notices() }).toEqual({
          requests: [],
          banners: [],
          status: '',
        })
      },
    )

    it('sends no merchant preview again when the reread after a stale one answers late', async () => {
      const capabilities = [{ ...merchant, description: 'Key', category: 'Edit', available: true }]
      const paths: string[] = []
      let refused = false
      const rereading = Promise.withResolvers<void>()
      const late = Promise.withResolvers<void>()
      const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
        const route = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
        paths.push(route)
        if (route === 'merchant-preview') {
          refused = true
          return stale()
        }
        if (route === 'view' && refused) {
          rereading.resolve()
          await late.promise
        }
        if (route === 'view') return Response.json(testProjection({ capabilities }))
        if (route === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
        if (route === 'provider/write-status') return Response.json(idle)
        return Response.json(
          { status: 404, code: 'not_found', detail: 'Not found.' },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        )
      })
      const workspace = render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState: memoryViewState(),
      })
      await appears(() => document.getElementById('moneyflow-row-row-0'))
      await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), {
        key: 'm',
      })
      // The stale answer has the view reread before the preview is sent again.
      await rereading.promise
      const sent = paths.length

      workspace.unmount()
      late.resolve()
      await new Promise((resolve) => setTimeout(resolve))

      expect({ requests: paths.slice(sent), ...notices() }).toEqual({
        requests: [],
        banners: [],
        status: '',
      })
    })

    it.each([
      { code: 'revision_conflict', reread: 'answers' },
      { code: 'selection_stale', reread: 'answers' },
      { code: 'revision_conflict', reread: 'fails' },
    ])(
      'shows nothing from a $code edit whose reread $reread after the host replaced it',
      async ({ code, reread }) => {
        const hide = { id: 'transaction.toggle-hidden', key_display: 'h', description: 'Hide' }
        const capabilities = [{ ...hide, category: 'Edit', available: true }]
        const otherProfileID = 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb'
        let edited = false
        const rereading = Promise.withResolvers<void>()
        const late = Promise.withResolvers<void>()
        const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
          const path = new Request(input, init).url.replace(`${api}profiles/`, '')
          const route = path.slice(path.indexOf('/') + 1)
          if (route === 'mutations') {
            edited = true
            return refused(code)
          }
          // The refused edit rereads the first profile's view.
          if (route === 'view' && edited && path.startsWith(profileID)) {
            rereading.resolve()
            await late.promise
            if (reread === 'fails') throw new TypeError('Failed to fetch')
          }
          if (route === 'view') return Response.json(testProjection({ capabilities }))
          if (route === 'provider/status')
            return Response.json(testProviderStatus({ revision: '0' }))
          if (route === 'provider/write-status') return Response.json(idle)
          return Response.json(
            { status: 404, code: 'not_found', detail: 'Not found.' },
            { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
          )
        })
        const workspace = render(MoneyflowWorkspace, {
          apiBase: '/embedded/moneyflow/',
          fetch: hostFetch,
          profileID,
          viewState: memoryViewState(),
        })
        await appears(() => document.getElementById('moneyflow-row-row-0'))
        await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), {
          key: 'h',
        })
        await rereading.promise

        await workspace.rerender({ profileID: otherProfileID })
        await appears(() => document.getElementById('moneyflow-row-row-0'))
        late.resolve()
        await new Promise((resolve) => setTimeout(resolve))

        // Banners and the status bar belong to whatever workspace the host shows now.
        expect(notices()).toEqual({ banners: [], status: '' })
      },
    )
  })

  describe('a new identity from the host', () => {
    const otherProfileID = 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb'
    // What a write-status check answers; `writing` is a write a writable tab resumes.
    let write: object = idle
    const paths: string[] = []
    const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
      const path = new Request(input, init).url.replace(`${api}profiles/`, '')
      paths.push(path)
      const route = path.slice(path.indexOf('/') + 1)
      if (route === 'view') return Response.json(testProjection())
      if (route === 'provider/status') return Response.json(testProviderStatus({ revision: '0' }))
      if (route === 'provider/write-status') return Response.json(write)
      return Response.json(
        { status: 404, code: 'not_found', detail: 'Not found.' },
        { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
      )
    })

    beforeEach(() => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
      write = idle
      paths.length = 0
    })
    afterEach(() => vi.useRealTimers())

    /** A writable workspace for the first profile, once its view and status checks answered. */
    async function shown() {
      const viewState = memoryViewState()
      const workspace = render(MoneyflowWorkspace, {
        apiBase: '/embedded/moneyflow/',
        fetch: hostFetch,
        profileID,
        viewState,
      })
      await appears(() => document.getElementById('moneyflow-row-row-0'))
      await new Promise((resolve) => setTimeout(resolve))
      paths.length = 0
      // The workspace and its search field each follow history.
      return { viewState, workspace, listening: viewState.listeners.size }
    }

    it('starts over for another profile, and the first profile hears no more', async () => {
      const { viewState, workspace, listening } = await shown()

      await workspace.rerender({ profileID: otherProfileID })
      // Every status check comes due at least once.
      await vi.advanceTimersByTimeAsync(60_000)

      expect(paths).toContain(`${otherProfileID}/view`)
      expect(paths.filter((path) => !path.startsWith(`${otherProfileID}/`))).toEqual([])
      expect(viewState.listeners.size).toBe(listening)
    })

    it('starts over when it turns read-only, and sends no change from then on', async () => {
      const { viewState, workspace, listening } = await shown()

      write = writing
      await workspace.rerender({ readOnly: true })
      await vi.advanceTimersByTimeAsync(60_000)

      const routes = paths.map((path) => path.replace(`${profileID}/`, ''))
      expect(routes).toContain('view')
      expect(
        routes.filter((route) => route !== 'view' && route !== 'provider/write-status'),
      ).toEqual([])
      expect(viewState.listeners.size).toBe(listening)
    })
  })

  it('writes a cursor move into its own entry when it unmounts in place', async () => {
    const { viewState, workspace } = await workspaceWithMovedCursor()
    workspace.unmount()

    expect(viewState.entries).toEqual([
      {
        query: 'v=1',
        state: expect.objectContaining({ cursorIdentity: 'row-1', cursorIndex: 1 }),
      },
    ])
  })

  it('leaves the entry of the page a host moved on to alone when it unmounts', async () => {
    const { viewState, workspace } = await workspaceWithMovedCursor()
    // A host that changes its address first and then removes the workspace, within the pause.
    viewState.push({ page: 'host' }, '')
    const entries = viewState.entries.map((entry) => ({ ...entry }))
    workspace.unmount()

    expect(viewState.entries).toEqual(entries)
  })
})

/** No provider write. */
const idle = {
  version: '1',
  revision: '0',
  generation: '0',
  total: 0,
  completed: 0,
  failed: 0,
  remaining: 0,
  overrides: 0,
  actions: [],
}

/** A provider write that this tab resumes, as it does for a write that no tab drives. */
const writing = {
  version: '1',
  revision: '1',
  generation: '1',
  batch_version: '2',
  phase: 'writing',
  total: 2,
  completed: 0,
  failed: 0,
  remaining: 2,
  overrides: 0,
  actions: ['resume'],
}

/** A host's workspace whose cursor moved down a row, in the pause before history records it. */
async function workspaceWithMovedCursor() {
  const first = testProjection().detail_rows![0]!
  const shown = testProjection({
    total_rows: 2,
    window: { offset: 0, limit: 200, count: 2 },
    detail_rows: [first, { ...first, index: 1, identity: 'row-1' }],
  })
  const hostFetch = vi.fn<typeof fetch>(async (input, init) => {
    const path = new Request(input, init).url.replace(`${api}profiles/${profileID}/`, '')
    if (path === 'view') return Response.json(shown)
    return Response.json(
      { status: 404, code: 'not_found', detail: 'Not found.' },
      { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
    )
  })
  const viewState = memoryViewState()
  const workspace = render(MoneyflowWorkspace, {
    apiBase: '/embedded/moneyflow/',
    fetch: hostFetch,
    profileID,
    viewState,
  })
  await appears(() => document.getElementById('moneyflow-row-row-1'))
  await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), {
    key: 'ArrowDown',
  })
  await appears(() => document.querySelector('#moneyflow-row-row-1.mf-table__row--active'))
  return { viewState, workspace }
}

/** The g key: the next grouping. */
const grouping = {
  id: 'view.cycle-grouping',
  key_display: 'g',
  description: 'Next grouping',
  category: 'Views',
  available: true,
}

/** Fails once `signal` is aborted, as a browser's request does when it is canceled. */
function canceled(signal: AbortSignal): Promise<never> {
  return new Promise((_, reject) =>
    signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
  )
}

function memoryViewState(): ViewStateAdapter & {
  entries: Array<{ state: unknown; query: string }>
  listeners: Set<() => void>
} {
  const entries: Array<{ state: unknown; query: string }> = [{ state: null, query: '' }]
  const listeners = new Set<() => void>()
  let index = 0
  return {
    entries,
    listeners,
    get state() {
      return entries[index]?.state ?? null
    },
    get query() {
      return entries[index]?.query ?? ''
    },
    push(state, query) {
      entries.splice(index + 1, entries.length, { state, query })
      index += 1
    },
    replace(state, query) {
      entries[index] = { state, query }
    },
    go(delta) {
      index += delta
    },
    listen(listener) {
      const held = () => listener()
      listeners.add(held)
      return () => listeners.delete(held)
    },
  }
}
