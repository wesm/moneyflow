import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import axe from 'axe-core'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import EditorComboboxHarness from '../../test/EditorComboboxHarness.svelte'
import type { EditorChoice } from '../../lib/editing/options'

function choice(id: string, label: string, count: number, parent?: string): EditorChoice {
  return {
    id,
    label,
    protected: false,
    transaction_count: count,
    ...(parent ? { parent_id: parent } : {}),
  }
}

const merchants = [
  choice('m-1', 'Corner Grocery', 12),
  choice('m-2', 'Example Coffee', 3),
  choice('m-3', 'Grocery Outlet', 7),
]

function setup(props: { value?: string; valid?: boolean; choices?: EditorChoice[] } = {}) {
  const onaccept = vi.fn()
  const onkeyaction = vi.fn()
  render(EditorComboboxHarness, {
    props: { choices: merchants, onaccept, onkeyaction, ...props },
  })
  const input = screen.getByRole('combobox', { name: 'Merchant' }) as HTMLInputElement
  input.focus()
  return { input, onaccept, onkeyaction }
}

async function type(input: HTMLInputElement, text: string): Promise<void> {
  await fireEvent.keyDown(input, { key: text.at(-1) ?? 'Backspace' })
  await fireEvent.input(input, { target: { value: text } })
}

function optionNames(): string[] {
  return screen.queryAllByRole('option').map((option) => option.textContent?.trim() ?? '')
}

function activeOption(input: HTMLInputElement): HTMLElement | null {
  const id = input.getAttribute('aria-activedescendant')
  return id ? document.getElementById(id) : null
}

describe('EditorCombobox', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
  })
  afterEach(cleanup)

  it('wires the combobox to its listbox popup', async () => {
    const { input } = setup()
    expect(input.getAttribute('aria-expanded')).toBe('false')
    expect(input.getAttribute('aria-autocomplete')).toBe('list')
    expect(input.hasAttribute('aria-activedescendant')).toBe(false)
    const list = document.getElementById(input.getAttribute('aria-controls') ?? '')
    expect(list?.getAttribute('role')).toBe('listbox')
    expect(list?.hidden).toBe(true)

    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByRole('listbox', { name: 'Merchant' })).toBe(list)
    expect(activeOption(input)?.getAttribute('aria-selected')).toBe('true')
    expect(screen.getAllByRole('option').filter((o) => o.ariaSelected === 'true')).toHaveLength(1)
  })

  it('names each option by its label, group, and count, separated', async () => {
    const groups = [choice('g-food', 'Food', 0)]
    render(EditorComboboxHarness, {
      props: {
        choices: [choice('c-1', 'Groceries', 9, 'g-food'), choice('c-2', 'Gifts', 1)],
        groups,
        onaccept: vi.fn(),
        onkeyaction: vi.fn(),
      },
    })
    const input = screen.getByRole('combobox')
    await type(input as HTMLInputElement, 'G')
    expect(screen.getByRole('option', { name: 'Groceries, Food, 9 transactions' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Gifts, 1 transaction' })).toBeTruthy()
    await type(input as HTMLInputElement, 'Gas')
    expect(screen.getByRole('option', { name: 'Create "Gas"' })).toBeTruthy()
  })

  it('opens the list when typing and filters it as the text changes', async () => {
    const { input } = setup()
    await fireEvent.keyDown(input, { key: 'g' })
    expect(input.getAttribute('aria-expanded')).toBe('true')
    await fireEvent.input(input, { target: { value: 'gro' } })
    expect(optionNames()).toEqual([
      expect.stringContaining('Corner Grocery'),
      expect.stringContaining('Grocery Outlet'),
      'Create "gro"',
    ])
  })

  it('opens the list on input that has no key press, such as a paste', async () => {
    const { input } = setup()
    await fireEvent.input(input, { target: { value: 'Coffee' } })
    expect(input.getAttribute('aria-expanded')).toBe('true')
  })

  it('offers Create only when the text matches no label exactly, ignoring case', async () => {
    const { input } = setup()
    await type(input, 'New Bakery')
    expect(optionNames()).toEqual(['Create "New Bakery"'])
    await type(input, 'example coffee')
    expect(optionNames()).toEqual([expect.stringContaining('Example Coffee')])
  })

  it('moves the active option with the arrow keys, wrapping at the ends', async () => {
    const { input } = setup()
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(activeOption(input)?.textContent).toContain('Corner Grocery')
    expect(await fireEvent.keyDown(input, { key: 'ArrowDown' })).toBe(false)
    expect(activeOption(input)?.textContent).toContain('Example Coffee')
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(activeOption(input)?.textContent).toContain('Grocery Outlet')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(activeOption(input)?.textContent).toContain('Corner Grocery')
  })

  it('scrolls the active option into view', async () => {
    const { input } = setup()
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    const scrolled = vi.mocked(Element.prototype.scrollIntoView).mock.contexts.at(-1)
    expect(scrolled).toBe(activeOption(input))
    expect(activeOption(input)?.textContent).toContain('Grocery Outlet')
  })

  it('opens the list with the up arrow too', async () => {
    const { input } = setup()
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(input.getAttribute('aria-expanded')).toBe('true')
  })

  it('accepts the active option into the field on Enter and closes the list', async () => {
    const { input, onaccept, onkeyaction } = setup()
    await type(input, 'outlet')
    expect(await fireEvent.keyDown(input, { key: 'Enter' })).toBe(false)
    expect(input.value).toBe('Grocery Outlet')
    expect(input.getAttribute('aria-expanded')).toBe('false')
    expect(onaccept).toHaveBeenCalledWith(expect.objectContaining({ id: 'm-3', create: false }))
    expect(onkeyaction).not.toHaveBeenCalled()
  })

  it('accepts Create with the trimmed text', async () => {
    const { input, onaccept } = setup()
    await type(input, '  New Bakery ')
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(input.value).toBe('New Bakery')
    expect(onaccept).toHaveBeenCalledWith(
      expect.objectContaining({ id: '', label: 'New Bakery', create: true }),
    )
  })

  it('stages on Enter with the list closed when the value is valid', async () => {
    const { input, onkeyaction } = setup({ value: 'Corner Grocery' })
    expect(await fireEvent.keyDown(input, { key: 'Enter' })).toBe(false)
    expect(onkeyaction).toHaveBeenCalledWith('stage')
  })

  it('does nothing on Enter with the list closed when the value is invalid', async () => {
    const { input, onkeyaction } = setup({ valid: false })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(onkeyaction).not.toHaveBeenCalled()
  })

  it.each([{ ctrlKey: true }, { metaKey: true }])(
    'accepts the active option and stages on %o+Enter with the list open',
    async (modifier) => {
      const { input, onaccept, onkeyaction } = setup()
      await type(input, 'coffee')
      await fireEvent.keyDown(input, { key: 'Enter', ...modifier })
      expect(input.value).toBe('Example Coffee')
      expect(onaccept).toHaveBeenCalledWith(expect.objectContaining({ id: 'm-2' }))
      expect(onkeyaction).toHaveBeenCalledWith('accept-and-stage')
      expect(onaccept.mock.invocationCallOrder[0]).toBeLessThan(
        onkeyaction.mock.invocationCallOrder[0] ?? 0,
      )
    },
  )

  it.each([{ ctrlKey: true }, { metaKey: true }])(
    'stages on %o+Enter with the list closed',
    async (modifier) => {
      const { input, onaccept, onkeyaction } = setup({ value: 'Corner Grocery' })
      await fireEvent.keyDown(input, { key: 'Enter', ...modifier })
      expect(onkeyaction).toHaveBeenCalledWith('stage')
      expect(onaccept).not.toHaveBeenCalled()
    },
  )

  it('claims Escape to close an open list and keeps the typed text', async () => {
    const { input, onkeyaction } = setup()
    await type(input, 'gro')
    const host = screen.getByTestId('editor-host')
    const hostEscape = vi.fn()
    host.addEventListener('keydown', hostEscape)
    expect(await fireEvent.keyDown(input, { key: 'Escape' })).toBe(false)
    expect(input.getAttribute('aria-expanded')).toBe('false')
    expect(input.value).toBe('gro')
    expect(hostEscape).not.toHaveBeenCalled()
    expect(onkeyaction).not.toHaveBeenCalled()
  })

  it('lets Escape with the list closed reach the host as a cancel', async () => {
    const { input, onkeyaction } = setup({ value: 'gro' })
    const host = screen.getByTestId('editor-host')
    const hostEscape = vi.fn()
    host.addEventListener('keydown', hostEscape)
    expect(await fireEvent.keyDown(input, { key: 'Escape' })).toBe(true)
    expect(hostEscape).toHaveBeenCalledTimes(1)
    expect(onkeyaction).toHaveBeenCalledWith('cancel')
    expect(input.value).toBe('gro')
  })

  it('leaves Tab and Shift+Tab to the browser focus order', async () => {
    const { input, onkeyaction } = setup()
    await type(input, 'gro')
    expect(await fireEvent.keyDown(input, { key: 'Tab' })).toBe(true)
    expect(await fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })).toBe(true)
    expect(onkeyaction).not.toHaveBeenCalled()
  })

  it('closes the list when focus leaves the combobox, keeping the text', async () => {
    const { input } = setup()
    await type(input, 'gro')
    await fireEvent.focusOut(input)
    expect(input.getAttribute('aria-expanded')).toBe('false')
    expect(input.value).toBe('gro')
  })

  it('treats a list with no options as closed', async () => {
    const { input, onkeyaction } = setup({ choices: [] })
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input.getAttribute('aria-expanded')).toBe('false')
    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(onkeyaction).toHaveBeenCalledWith('cancel')
  })

  it('accepts an option chosen with the pointer without moving focus', async () => {
    const { input, onaccept } = setup()
    await type(input, 'coffee')
    const option = screen.getByRole('option', { name: /Example Coffee/ })
    expect(await fireEvent.mouseDown(option)).toBe(false)
    expect(input.value).toBe('Example Coffee')
    expect(onaccept).toHaveBeenCalledWith(expect.objectContaining({ id: 'm-2' }))
  })

  it('keeps the list open when the list itself is pressed, such as its scrollbar', async () => {
    const { input } = setup()
    await type(input, 'gro')
    const list = screen.getByRole('listbox')
    // A default-prevented press does not move focus, so the input never blurs.
    expect(await fireEvent.mouseDown(list)).toBe(false)
    expect(document.activeElement).toBe(input)
    expect(input.getAttribute('aria-expanded')).toBe('true')
  })

  it('gives each instance its own list and option ids', async () => {
    const props = { choices: merchants, onaccept: vi.fn(), onkeyaction: vi.fn() }
    render(EditorComboboxHarness, { props })
    render(EditorComboboxHarness, { props })
    const [first, second] = screen.getAllByRole('combobox') as HTMLInputElement[]
    await fireEvent.keyDown(first!, { key: 'ArrowDown' })
    await fireEvent.keyDown(second!, { key: 'ArrowDown' })
    expect(first!.getAttribute('aria-controls')).not.toBe(second!.getAttribute('aria-controls'))
    expect(activeOption(first!)).not.toBe(activeOption(second!))
    const ids = Array.from(document.querySelectorAll('[id]'), (element) => element.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('leaves keys alone during an input method composition', async () => {
    const { input, onaccept, onkeyaction } = setup()
    await type(input, 'coffee')
    expect(await fireEvent.keyDown(input, { key: 'Enter', isComposing: true })).toBe(true)
    expect(await fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 })).toBe(true)
    expect(await fireEvent.keyDown(input, { key: 'Escape', isComposing: true })).toBe(true)
    expect(input.getAttribute('aria-expanded')).toBe('true')
    expect(onaccept).not.toHaveBeenCalled()
    expect(onkeyaction).not.toHaveBeenCalled()
  })

  it('leaves shortcuts such as Ctrl+A and Cmd+ArrowUp to the input', async () => {
    const { input } = setup({ value: 'Corner' })
    expect(await fireEvent.keyDown(input, { key: 'a', ctrlKey: true })).toBe(true)
    expect(await fireEvent.keyDown(input, { key: 'ArrowUp', metaKey: true })).toBe(true)
    expect(input.getAttribute('aria-expanded')).toBe('false')
  })

  it('accepts Create and stages on Ctrl+Enter', async () => {
    const { input, onaccept, onkeyaction } = setup()
    await type(input, ' New Bakery ')
    await fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    expect(input.value).toBe('New Bakery')
    expect(onaccept).toHaveBeenCalledWith(
      expect.objectContaining({ id: '', label: 'New Bakery', create: true }),
    )
    expect(onkeyaction).toHaveBeenCalledWith('accept-and-stage')
  })

  it('has no automated accessibility violations with the list open', async () => {
    const { input } = setup()
    await type(input, 'gro')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    const result = await axe.run(document.body, {
      rules: { 'color-contrast': { enabled: false }, region: { enabled: false } },
    })
    expect(result.violations).toEqual([])
  })
})
