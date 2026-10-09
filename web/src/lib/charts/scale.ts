import { parseMinor } from '../money/format'

const basisPoints = 10000n

function magnitude(minor: string): bigint {
  const value = parseMinor(minor)
  return value < 0n ? -value : value
}

/**
 * The share `|value| / |max|` in basis points, floored with exact integer math; 0 when `max` is
 * 0. Chart geometry uses this so money never becomes a JavaScript number.
 */
export function ratioBasisPoints(value: string, max: string): number {
  const limit = magnitude(max)
  if (limit === 0n) return 0
  // eslint-disable-next-line no-restricted-syntax -- a basis-point ratio, not money
  return Number((magnitude(value) * basisPoints) / limit)
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
