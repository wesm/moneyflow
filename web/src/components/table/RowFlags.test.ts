import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import RowFlags from './RowFlags.svelte'
import type { components } from '../../lib/api/schema'
import { testProjection } from '../../test/projection'

type DetailRow = components['schemas']['DetailRow']

function row(overrides: Partial<DetailRow> = {}): DetailRow {
  return { ...testProjection().detail_rows![0]!, ...overrides }
}

const match = (confidence: string) => ({
  class: 'exact_order',
  confidence,
  first_product: 'Example Product',
  total_matches: 1,
})

describe('RowFlags', () => {
  afterEach(cleanup)

  it('marks pending edits with a gold dot and a committed hidden row with a Hidden tag', () => {
    render(RowFlags, { row: row({ flags: { selected: false, hidden: true, pending: true } }) })
    expect(screen.getByLabelText('Pending edits')).not.toBeNull()
    expect(screen.getByText('Hidden')).not.toBeNull()
  })

  it('shows nothing for a committed visible row without edits', () => {
    const view = render(RowFlags, {
      row: row({ flags: { selected: false, hidden: false, pending: false } }),
    })
    expect(view.container.textContent?.trim()).toBe('')
  })

  it('marks strong Amazon matches with ✓ and likely ones with ~, both opening Info', async () => {
    const oninformation = vi.fn()
    render(RowFlags, { row: row({ amazon_match: match('medium') }), oninformation })
    const strong = screen.getByRole('button', { name: 'Amazon match: Example Product, medium' })
    expect(strong.textContent?.trim()).toBe('✓')
    // The grid is one Tab stop; i and Enter reach Info from the keyboard.
    expect(strong.getAttribute('tabindex')).toBe('-1')
    await fireEvent.click(strong)
    expect(oninformation).toHaveBeenCalledWith('row-0')
    cleanup()

    render(RowFlags, { row: row({ amazon_match: match('likely') }), oninformation })
    expect(
      screen
        .getByRole('button', { name: 'Amazon match: Example Product, likely' })
        .textContent?.trim(),
    ).toBe('~')
  })

  it('shows a gold tag for each staged visibility change and for a staged deletion', () => {
    const staged = (values: Partial<NonNullable<DetailRow['staged']>>) =>
      row({ staged: { deleted: false, ...values } })
    const tag = (name: string) => screen.getByText(name).closest('.kit-chip')
    const cases: [DetailRow, string][] = [
      [staged({ hidden: true }), 'Hidden'],
      [
        { ...staged({ hidden: false }), flags: { selected: false, hidden: true, pending: true } },
        'Visible',
      ],
      [staged({ deleted: true }), 'Delete'],
    ]
    for (const [value, name] of cases) {
      render(RowFlags, { row: value })
      expect(tag(name)?.classList.contains('kit-chip--tone-warning')).toBe(true)
      cleanup()
    }
  })

  it('replaces the committed Hidden tag with the staged Visible tag', () => {
    render(RowFlags, {
      row: row({
        flags: { selected: false, hidden: true, pending: true },
        staged: { deleted: false, hidden: false },
      }),
    })
    expect(screen.queryByText('Hidden')).toBeNull()
    expect(screen.getByText('Visible')).not.toBeNull()
  })
})
