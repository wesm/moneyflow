import { vi } from 'vitest'

import type { ProviderStatus } from '../lib/api/client'
import type { ProviderController } from '../lib/controller/provider'

export function providerCapability(available: boolean, reason?: string) {
  return {
    id: 'provider.refresh',
    key_display: 'r',
    description: 'Refresh provider data',
    category: 'System',
    available,
    ...(reason === undefined ? {} : { reason }),
  }
}

export function testProviderStatus(overrides: Partial<ProviderStatus> = {}): ProviderStatus {
  return {
    version: '1',
    revision: '1',
    generation: '1',
    refresh_due: 'none',
    manual_refresh: 'full',
    refreshing: false,
    progress: { fetched: 0, total: 0, verifying: false, percent: 0 },
    summary: {
      imported_accounts: 0,
      imported_merchants: 0,
      imported_groups: 0,
      imported_categories: 0,
      imported_transactions: 0,
      removed_transactions: 0,
      removed_operations: 0,
      removed_targets: 0,
      retained_operations: 0,
      rebased_hide_targets: 0,
      discarded_redo_operations: 0,
    },
    capability: providerCapability(true),
    ...overrides,
  }
}

export function stubProvider(
  available: boolean,
  state: Partial<ProviderController['state']> = {},
): ProviderController {
  return {
    state: {
      phase: 'idle',
      announcement: '',
      capability: providerCapability(
        available,
        available ? undefined : 'Connect a provider before refreshing.',
      ),
      ...state,
    },
    cancelable: true,
    sync: vi.fn(),
    poll: vi.fn(async () => undefined),
    reread: vi.fn(async () => undefined),
    refresh: vi.fn(async () => true),
    confirm: vi.fn(async () => true),
    dismissConfirmation: vi.fn(),
    cancel: vi.fn(),
    hide: vi.fn(),
    continueStale: vi.fn(),
    stop: vi.fn(),
  }
}
