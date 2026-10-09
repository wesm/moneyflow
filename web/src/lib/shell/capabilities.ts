import type { ViewProjection } from '../api/client'

const genericReason = 'Not available in this view'

/** Lists the IDs of the capabilities the current view offers. */
export function availableCapabilities(projection: ViewProjection): Set<string> {
  const available = new Set<string>()
  for (const capability of projection.capabilities ?? []) {
    if (capability.available) available.add(capability.id)
  }
  return available
}

/** Explains why a capability is unavailable, with the server's reason when it gives one. */
export function unavailableReason(projection: ViewProjection, id: string): string {
  return (
    projection.capabilities?.find((capability) => capability.id === id)?.reason ?? genericReason
  )
}
