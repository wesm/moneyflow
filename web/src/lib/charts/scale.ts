import { parseMinor } from '../money/format'

const basisPoints = 10000n

/** The absolute value of a minor-unit amount. */
export function magnitude(minor: string): bigint {
  const value = parseMinor(minor)
  return value < 0n ? -value : value
}

/**
 * The share `|value| / |max|` in basis points, floored with exact integer math and at most
 * 10000; 0 when `max` is 0. Chart geometry uses this so money never becomes a JavaScript number.
 */
export function ratioBasisPoints(value: string, max: string): number {
  const limit = magnitude(max)
  if (limit === 0n) return 0
  const ratio = (magnitude(value) * basisPoints) / limit
  // eslint-disable-next-line no-restricted-syntax -- a basis-point ratio of at most 10000
  return Number(ratio < basisPoints ? ratio : basisPoints)
}

/** The largest absolute value among minor-unit amounts, as a minor string; `'0'` for none. */
export function maxMagnitude(values: readonly string[]): string {
  let largest = 0n
  for (const value of values) {
    const candidate = magnitude(value)
    if (candidate > largest) largest = candidate
  }
  return largest.toString()
}

export interface Point {
  readonly x: number
  readonly y: number
}

/**
 * Where a line from `from` to `to` meets a ring of radius `distance` drawn at `to`, so the line
 * leaves a hollow dot empty. A line shorter than the radius collapses to `from`.
 */
export function stopShort(from: Point, to: Point, distance: number): Point {
  const length = Math.hypot(to.x - from.x, to.y - from.y)
  if (length <= distance) return from
  const kept = (length - distance) / length
  return { x: from.x + (to.x - from.x) * kept, y: from.y + (to.y - from.y) * kept }
}
