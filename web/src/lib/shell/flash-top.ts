/**
 * Reports where a toast should sit: the top edge of the element, which is the bottom of the
 * command bar, so toasts cover the workspace and leave the period and view controls free. The
 * measure follows the viewport, so it already includes a device's top safe-area inset. The
 * command bar wraps to more rows as the window narrows, so it re-measures on every resize.
 */
export function trackTop(node: HTMLElement, report: (top: number) => void): { destroy(): void } {
  const measure = () => report(Math.round(node.getBoundingClientRect().top))
  const observer = new ResizeObserver(measure)
  observer.observe(node)
  measure()
  return { destroy: () => observer.disconnect() }
}
