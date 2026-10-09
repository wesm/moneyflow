import { cleanup, render } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import StagedValue from './StagedValue.svelte'

describe('StagedValue', () => {
  afterEach(cleanup)

  it('shows the committed value alone without a staged change', () => {
    const view = render(StagedValue, { committed: 'Example Merchant' })
    expect(view.container.textContent?.trim()).toBe('Example Merchant')
    expect(view.container.querySelector('s')).toBeNull()
  })

  it('strikes the committed value and shows the staged one in the pending color', () => {
    const view = render(StagedValue, { committed: 'Example Merchant', staged: 'Renamed Merchant' })
    const staged = view.container.querySelector('.mf-staged')
    expect(staged?.querySelector('s')?.textContent).toBe('Example Merchant')
    expect(staged?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      'Example Merchant → Renamed Merchant',
    )
  })
})
