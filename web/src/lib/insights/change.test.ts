import { describe, expect, it } from 'vitest'

import type { components } from '../api/schema'
import { priorChange } from './change'

type Change = components['schemas']['InsightsChange']

// The server rounds the change half up to tenths of a percent, so 0.04% arrives as 0 tenths
// and 0.05% as 1. `grew` and `favorable` follow the unrounded change.
function percent(percent_tenths: number, grew: boolean, favorable: boolean): Change {
  return { kind: 'percent', percent_tenths, grew, favorable }
}

const shown = (change: Change) => priorChange(change, 2, 'vs Aug')

describe('priorChange', () => {
  it('shows an unchanged spending or income row as a neutral 0.0% with no arrow', () => {
    // The server marks equal magnitudes favorable, for spending and income alike.
    expect(shown(percent(0, false, true))).toEqual({
      text: '0.0%',
      spoken: '0.0% vs Aug',
      tone: 'neutral',
    })
  })

  it('shows a change that rounds to 0.0% as neutral, for spending and income rows', () => {
    // Spending that grew 0.04% and income that shrank 0.04%: unfavorable before rounding.
    expect(shown(percent(0, true, false))).toEqual({
      text: '0.0%',
      spoken: '0.0% vs Aug',
      tone: 'neutral',
    })
    expect(shown(percent(0, false, false))).toEqual({
      text: '0.0%',
      spoken: '0.0% vs Aug',
      tone: 'neutral',
    })
    // Income that grew 0.04%: favorable before rounding. Spending that shrank 0.04% arrives
    // exactly like the unchanged row above.
    expect(shown(percent(0, true, true))?.tone).toBe('neutral')
  })

  it('colors a change that rounds to 0.1% and gives it an arrow', () => {
    // Spending that grew 0.05% is red; income that shrank 0.05% is red.
    expect(shown(percent(1, true, false))).toEqual({
      text: '▲ 0.1%',
      spoken: 'up 0.1% vs Aug',
      tone: 'unfavorable',
    })
    expect(shown(percent(1, false, false))).toEqual({
      text: '▼ 0.1%',
      spoken: 'down 0.1% vs Aug',
      tone: 'unfavorable',
    })
    // Spending that shrank 0.05% is green; income that grew 0.05% is green.
    expect(shown(percent(1, false, true))).toEqual({
      text: '▼ 0.1%',
      spoken: 'down 0.1% vs Aug',
      tone: 'favorable',
    })
    expect(shown(percent(1, true, true))).toEqual({
      text: '▲ 0.1%',
      spoken: 'up 0.1% vs Aug',
      tone: 'favorable',
    })
  })
})
