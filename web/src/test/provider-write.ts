import { vi } from 'vitest'

import type { ProviderWriteController } from '../lib/controller/provider-write'

export function testProviderWriteController(
  overrides: Partial<ProviderWriteController> = {},
): ProviderWriteController {
  return {
    state: {
      phase: 'active',
      announcement: 'Writing pending changes to Monarch.',
      status: {
        version: '1',
        revision: '3',
        generation: '1',
        batch_version: '2',
        phase: 'writing',
        total: 5,
        completed: 2,
        failed: 0,
        remaining: 3,
        overrides: 0,
        actions: ['pause'],
      },
    },
    install: vi.fn(),
    poll: vi.fn(async () => undefined),
    pause: vi.fn(async () => true),
    resume: vi.fn(async () => true),
    reconcile: vi.fn(async () => true),
    confirm: vi.fn(async () => true),
    can: vi.fn((action: string) => action === 'pause'),
    ...overrides,
  }
}
