import { MoneyflowProblem, type MoneyflowClient, type ViewProjection } from '../api/client'

export const READ_ONLY_REASON = 'Read-only mode'

type Capability = NonNullable<ViewProjection['capabilities']>[number]

// Every capability behind an edit, a review or commit, a refresh, an export or a maintenance view.
const changing = new Set([
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
  'category.manage',
  'category-group.manage',
  'changes.undo',
  'changes.redo',
  'changes.review',
  'provider.refresh',
  'provider.refresh-history',
  'transactions.export',
  'view.find-duplicates',
])

function restrict(capability: Capability): Capability {
  if (!changing.has(capability.id)) return capability
  return { ...capability, available: false, reason: READ_ONLY_REASON }
}

/** The projection with every capability that changes data, or leads to a change, unavailable. */
export function restrictCapabilities(projection: ViewProjection): ViewProjection {
  return { ...projection, capabilities: projection.capabilities?.map(restrict) ?? null }
}

/**
 * A client whose projections offer nothing that changes data, and which sends no change request.
 * It never asks for provider status either: a read-only host refuses that request, so the client
 * refuses it first and sends nothing.
 */
export function readOnlyClient(client: MoneyflowClient): MoneyflowClient {
  return {
    ...client,
    mutations: { request: refuse, upload: refuse },
    view: async (body, signal) => restrictCapabilities(await client.view(body, signal)),
    transition: async (body, signal) => restrictCapabilities(await client.transition(body, signal)),
    providerStatus: refuse,
  }
}

async function refuse(): Promise<never> {
  throw new MoneyflowProblem({
    type: 'about:blank',
    title: READ_ONLY_REASON,
    status: 403,
    code: 'read_only',
    detail: READ_ONLY_REASON,
  })
}
