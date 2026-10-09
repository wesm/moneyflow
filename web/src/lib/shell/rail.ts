export type RailComponent = typeof import('../../components/VisualizationRail.svelte').default

/** Loads the chart rail in its own chunk so the chart library stays out of the first download. */
export async function loadRail(): Promise<RailComponent> {
  const module = await import('../../components/VisualizationRail.svelte')
  return module.default
}
