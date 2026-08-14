import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RefinementBar from './RefinementBar.svelte'
import { testProjection } from '../test/projection'

describe('RefinementBar', () => {
  afterEach(cleanup)
  it('renders server labels and routes clear through the shared action', async () => {
    const onaction = vi.fn()
    render(RefinementBar, {
      projection: testProjection({
        breadcrumbs: [{ dimension: 'merchant', label: 'Example Housing' }],
        breadcrumb_text: 'M: Example Housing > (by Category)',
        view: {
          mode: 'detail',
          grouping: 'merchant',
          time_granularity: 'year',
          search: 'coffee',
          sort_field: 'amount',
          sort_direction: 'desc',
        },
      }),
      onaction,
    })
    expect(screen.getByText('M: Example Housing > (by Category)')).not.toBeNull()
    expect(screen.queryByText('Example Housing')).toBeNull()
    expect(screen.getByText('Search: coffee')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /Clear/ }))
    expect(onaction).toHaveBeenCalledWith('view.back')
  })
})
