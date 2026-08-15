import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AppShell from './AppShell.svelte'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import { testProjection } from '../test/projection'

describe('AppShell', () => {
  afterEach(cleanup)
  it('keeps the finance grid primary and routes the TUI cursor shortcut', async () => {
    const controller = stubController()
    render(AppShell, { controller })
    expect(screen.getByRole('main', { name: 'Moneyflow workspace' })).not.toBeNull()
    expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledWith(1)
    vi.mocked(controller.moveCursor).mockClear()
    await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledTimes(1)
    expect((screen.getByRole('switch', { name: 'Charts' }) as HTMLInputElement).checked).toBe(true)
  })

  it('keeps the table full width and opens charts in a labelled compact drawer', async () => {
    const original = window.matchMedia
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: (query: string) => ({
        matches: query.includes('640px'),
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }),
    })
    try {
      const controller = stubController()
      render(AppShell, { controller })
      expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
      expect(screen.queryByRole('complementary', { name: 'Visualizations' })).toBeNull()
      await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
      expect(screen.getByRole('dialog', { name: 'Moneyflow visualizations' })).not.toBeNull()
      await fireEvent.keyDown(window, { key: 'j' })
      expect(controller.moveCursor).not.toHaveBeenCalled()
    } finally {
      Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
    }
  })

  it('closes the compact chart scope when resizing to desktop', async () => {
    const original = window.matchMedia
    const target = new EventTarget()
    const media = Object.assign(target, {
      matches: true,
      media: '(max-width: 640px)',
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
    }) as MediaQueryList
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: () => media })
    try {
      const controller = stubController()
      render(AppShell, { controller })
      await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
      expect(screen.getByRole('dialog', { name: 'Moneyflow visualizations' })).not.toBeNull()

      Object.defineProperty(media, 'matches', { configurable: true, value: false })
      media.dispatchEvent(new Event('change'))
      await vi.waitFor(() => {
        expect(screen.queryByRole('dialog', { name: 'Moneyflow visualizations' })).toBeNull()
      })
      await fireEvent.keyDown(window, { key: 'j' })
      expect(controller.moveCursor).toHaveBeenCalledWith(1)
    } finally {
      Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
    }
  })
})

function stubController(): ViewController {
  return {
    projection: testProjection({
      capabilities: [
        {
          id: 'cursor.down',
          key_display: '↓/j',
          description: 'Move cursor down',
          category: '',
          available: true,
        },
      ],
    }),
    loading: false,
    announcement: '',
    cursorIdentity: 'row-0',
    cursorIndex: 0,
    problem: undefined,
    editing: {} as ViewController['editing'],
    review: {} as ViewController['review'],
    hydrate: vi.fn(),
    recheck: vi.fn(),
    moveCursor: vi.fn(),
    moveCursorTo: vi.fn(),
    moveHome: vi.fn(),
    apply: vi.fn(),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    restore: vi.fn(),
    retry: vi.fn(),
    reset: vi.fn(),
  }
}
