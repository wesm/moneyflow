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
    expect(screen.getByText('All transactions')).not.toBeNull()
    expect(screen.getByText('Search: coffee')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /Clear/ }))
    expect(onaction).toHaveBeenCalledWith('view.back')
  })
})
