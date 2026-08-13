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
})

function stubController(): ViewController {
  return {
    projection: testProjection({
      capabilities: [{ id: 'cursor.down', key_display: '↓/j', description: 'Move cursor down' }],
    }),
    loading: false,
    announcement: '',
    cursorIdentity: 'row-0',
    cursorIndex: 0,
    problem: undefined,
    hydrate: vi.fn(),
    moveCursor: vi.fn(),
    moveHome: vi.fn(),
    apply: vi.fn(),
    restore: vi.fn(),
    retry: vi.fn(),
    reset: vi.fn(),
  }
}
