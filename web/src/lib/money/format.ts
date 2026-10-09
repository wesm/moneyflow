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
