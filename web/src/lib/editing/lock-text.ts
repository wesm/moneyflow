/** Words the banner that tells people why editing is locked, by the provider write's phase. */
export function lockText(providerName: string, phase: string): string {
  if (phase === 'paused') {
    return `${providerName} write paused · Editing is locked until it resumes or is reconciled`
  }
  if (
    phase === 'attention_required' ||
    phase === 'reconcile_confirmation_required' ||
    phase === 'reconnect_required'
  ) {
    return `${providerName} write needs attention · Editing is locked`
  }
  return `Writing to ${providerName} · Editing is locked until the write finishes`
}
