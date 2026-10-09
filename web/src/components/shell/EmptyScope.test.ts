import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EmptyScope from './EmptyScope.svelte'

const scope = { label: 'Nov 2026', jump: { start: '2026-10-01', end: '2026-10-31' } }

describe('EmptyScope', () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('takes focus that the replaced table dropped', () => {
    render(EmptyScope, { scope, onjump: vi.fn() })
    expect(screen.getByText('No transactions in Nov 2026')).not.toBeNull()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Jump to latest' }))
  })

  it('leaves focus on the control that changed the period', () => {
    const next = document.createElement('button')
    document.body.append(next)
    next.focus()
    render(EmptyScope, { scope, onjump: vi.fn() })
    expect(document.activeElement).toBe(next)
  })
})
