import type { SelectionValue } from '../api/client'

export interface MoneyflowHistoryState {
  owner: 'moneyflow-web-v1'
  instance: string
  sequence: number
  query: string
  cursorIdentity?: string
  cursorIndex: number
  scrollTop: number
  selection: SelectionValue
}

export class OwnedHistoryLedger {
  readonly #instance: string
  readonly #entries = new Map<number, MoneyflowHistoryState>()

  constructor(instance: string) {
    this.#instance = instance
  }

  read(value: unknown): MoneyflowHistoryState | undefined {
    if (!isRecord(value)) return undefined
    if (
      value.owner !== 'moneyflow-web-v1' ||
      value.instance !== this.#instance ||
      !Number.isSafeInteger(value.sequence) ||
      (value.sequence as number) < 0 ||
      typeof value.query !== 'string' ||
      (value.cursorIdentity !== undefined && typeof value.cursorIdentity !== 'string') ||
      !Number.isSafeInteger(value.cursorIndex) ||
      (value.cursorIndex as number) < 0 ||
      !Number.isSafeInteger(value.scrollTop) ||
      (value.scrollTop as number) < 0 ||
      typeof value.selection !== 'string'
    ) {
      return undefined
    }
    return value as unknown as MoneyflowHistoryState
  }

  record(value: unknown): MoneyflowHistoryState | undefined {
    const state = this.read(value)
    if (state) this.#entries.set(state.sequence, state)
    return state
  }

  deltaTo(query: string, selection: SelectionValue, currentValue: unknown): number | undefined {
    const current = this.read(currentValue)
    if (!current || this.#entries.get(current.sequence)?.query !== current.query) return undefined
    for (let sequence = current.sequence - 1; sequence >= 0; sequence -= 1) {
      const entry = this.#entries.get(sequence)
      if (!entry) return undefined
      if (entry.query === query && entry.selection === selection) return sequence - current.sequence
    }
    return undefined
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
