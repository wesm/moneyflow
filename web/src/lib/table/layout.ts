/** Desktop row height in pixels. CSS reads it as `--mf-row-height`, so virtualization agrees. */
export const ROW_HEIGHT = 30
/** Two-line phone row height in pixels, at 640 px and narrower. */
export const COMPACT_ROW_HEIGHT = 52

export function rowHeight(compact: boolean): number {
  return compact ? COMPACT_ROW_HEIGHT : ROW_HEIGHT
}
