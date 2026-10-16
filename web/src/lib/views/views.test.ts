import { describe, expect, it } from 'vitest'

import { navItems } from '../shell/nav-items'
import { testProjection } from '../../test/projection'
import { viewsFor, type ProviderKind } from './index'

describe('viewsFor', () => {
  it.each<ProviderKind>(['local', 'amazon'])(
    'lists the sidebar entries of a %s profile',
    (kind) => {
      const { explore, maintain } = navItems(testProjection({ profile_kind: kind }))
      const sidebar = [
        ...explore.map(({ id, label }) => ({ id, label, group: 'explore' })),
        ...maintain.map(({ id, label }) => ({ id, label, group: 'maintain' })),
      ]

      expect(viewsFor(kind)).toEqual(sidebar)
    },
  )

  it('leaves the Amazon import out of a Monarch profile', () => {
    expect(viewsFor('monarch').map((view) => view.id)).not.toContain('amazon')
  })
})
