import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { createRawSnippet } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import BottomSheet from './BottomSheet.svelte'

function snippet(html: string) {
  return createRawSnippet(() => ({ render: () => html }))
}

function sheet(open = true) {
  const onclose = vi.fn()
  const opener = document.createElement('button')
  document.body.append(opener)
  opener.focus()
  const rendered = render(BottomSheet, {
    open,
    title: 'View',
    onclose,
    children: snippet('<div><button type="button">First</button></div>'),
    footer: snippet('<div><button type="button">Done</button></div>'),
  })
  return { onclose, opener, rendered }
}

describe('BottomSheet', () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('renders nothing while closed', () => {
    sheet(false)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('names the dialog by its title and moves focus into it', () => {
    sheet()
    const dialog = screen.getByRole('dialog', { name: 'View' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByRole('heading', { name: 'View' })).not.toBeNull()
    expect(screen.getByRole('button', { name: 'First' })).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Done' })).not.toBeNull()
    expect(dialog.contains(document.activeElement)).toBe(true)
  })

  it('closes on Escape, the close button, and a press on the scrim', async () => {
    const { onclose } = sheet()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onclose).toHaveBeenCalledTimes(2)
    const scrim = screen.getByRole('dialog', { name: 'View' }).parentElement!
    await fireEvent.pointerDown(scrim)
    expect(onclose).toHaveBeenCalledTimes(3)
    await fireEvent.pointerDown(screen.getByRole('button', { name: 'First' }))
    expect(onclose).toHaveBeenCalledTimes(3)
  })

  it('leaves an Escape that an inner control claimed', async () => {
    const { onclose } = sheet()
    const event = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    event.preventDefault()
    window.dispatchEvent(event)
    expect(onclose).not.toHaveBeenCalled()
  })

  it('returns focus to the opener when it closes', async () => {
    const { opener, rendered } = sheet()
    await rendered.rerender({ open: false })
    expect(document.activeElement).toBe(opener)
  })
})
