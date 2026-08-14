import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App.svelte'
import type { ViewController } from './lib/controller/view-controller.svelte'

describe('Moneyflow application scaffold', () => {
  afterEach(cleanup)

  it('mounts the shared chrome around a fixture loading state', () => {
    render(App, { basePath: '/moneyflow/', controller: stubController() })

    expect(screen.getByRole('main', { name: 'Moneyflow' })).not.toBeNull()
    expect(screen.getByRole('heading', { name: 'Loading financial view…' })).not.toBeNull()
    expect(screen.getByRole('status').textContent).toBe('Loading fixture data…')
    expect(screen.getByRole('button', { name: /change theme/i })).not.toBeNull()
  })

  it('renders a safe invalid-link shell with a reset action', async () => {
    const controller = stubController({
      loading: false,
      problem: { kind: 'invalid-view', code: 'invalid_view_state' },
    })
    render(App, { basePath: '/moneyflow/', controller })

    expect(
      screen.getByRole('heading', { name: 'This Moneyflow link cannot be opened' }),
    ).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset view' }))
    expect(controller.reset).toHaveBeenCalledTimes(1)
  })
})

function stubController(overrides: Partial<ViewController> = {}): ViewController {
  return {
    projection: undefined,
    loading: true,
    announcement: '',
    cursorIdentity: undefined,
    cursorIndex: 0,
    problem: undefined,
    hydrate: vi.fn(async () => undefined),
    moveCursor: vi.fn(async () => undefined),
    moveCursorTo: vi.fn(async () => undefined),
    moveHome: vi.fn(async () => undefined),
    apply: vi.fn(async () => undefined),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(async () => true),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    restore: vi.fn(async () => undefined),
    retry: vi.fn(async () => undefined),
    reset: vi.fn(async () => undefined),
    ...overrides,
  }
}
