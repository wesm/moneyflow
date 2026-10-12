const minorPattern = /^(0|-?[1-9]\d*)$/

/**
 * Reads an exact minor-unit amount, such as the server's `"-584418"`. Throws on anything else,
 * including the empty string `BigInt` would read as zero.
 */
export function parseMinor(minor: string): bigint {
  if (!minorPattern.test(minor)) {
    throw new Error(`${JSON.stringify(minor)} is not a minor-unit amount.`)
  }
  return BigInt(minor)
}

/**
 * Formats a minor-unit amount like the server's `display`: an ASCII sign, comma groups of three,
 * and `scale` decimals, with no currency symbol. `formatMinor('-584418', 2)` is `-5,844.18`, and
 * zero is `+0.00`.
 */
export function formatMinor(minor: string, scale: number): string {
  parseMinor(minor)
  if (!Number.isSafeInteger(scale) || scale < 0) {
    throw new Error(`${scale} is not a currency scale.`)
  }
  const negative = minor.startsWith('-')
  const digits = (negative ? minor.slice(1) : minor).padStart(scale + 1, '0')
  const point = digits.length - scale
  const integer = digits.slice(0, point).replace(/\B(?=(\d{3})+$)/g, ',')
  const fraction = scale === 0 ? '' : `.${digits.slice(point)}`
  return `${negative ? '-' : '+'}${integer}${fraction}`
}

const zeroDisplay = /^[+-]?0(\.0+)?$/

/**
 * In and Out show a zero as a dash, as the table does. `display` comes from `formatMinor` or
 * the server; accessible names keep it as it is.
 */
export function dashZero(display: string): string {
  return zeroDisplay.test(display) ? '—' : display
}

/** Net shows a zero without the `+` that `formatMinor` and the server give it, such as `0.00`. */
export function unsignedZero(display: string): string {
  return zeroDisplay.test(display) ? display.replace(/^[+-]/, '') : display
}

/** Formats an integer count of tenths of a percent, such as 62 as `6.2%`. */
export function tenthsPercent(tenths: number): string {
  const sign = tenths < 0 ? '-' : ''
  const magnitude = Math.abs(tenths)
  return `${sign}${Math.trunc(magnitude / 10)}.${magnitude % 10}%`
}
