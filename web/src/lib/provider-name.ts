/** Names the provider a profile writes to; every profile without YNAB reads as Monarch. */
export function providerName(kind: string | undefined): 'YNAB' | 'Monarch' {
  return kind === 'ynab' ? 'YNAB' : 'Monarch'
}
