import { brotliCompressSync, constants, gzipSync } from 'node:zlib'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

export type AssetKind = 'javascript' | 'css'
export type CompressionMode = 'brotli' | 'gzip'

export interface AssetBudget {
  name: string
  manifest_entry: string
  kind: AssetKind
  compression: CompressionMode
  max_bytes: number
}

export interface InteractionBudget {
  name: string
  percentile: 'p95'
  samples: 100
  max_ms: number
}

export interface BudgetConfig {
  schema_version: 1
  assets: AssetBudget[]
  interactions: InteractionBudget[]
}

export interface InteractionMeasurement {
  percentile: 'p95'
  samples: number
  value_ms: number
}

export type InteractionMeasurements = Record<string, InteractionMeasurement>

export interface BudgetResult {
  name: string
  actual: number
  maximum: number
  unit: 'bytes' | 'ms'
}

interface ManifestEntry {
  file?: unknown
  css?: unknown
  imports?: unknown
}

function objectValue(value: unknown, label: string): Record<string, unknown> {
  if (value === null || Array.isArray(value) || typeof value !== 'object') {
    throw new Error(`${label} must be a JSON object`)
  }
  return value as Record<string, unknown>
}

function positiveInteger(value: unknown, label: string): number {
  if (!Number.isSafeInteger(value) || (value as number) < 1) {
    throw new Error(`${label} must be a positive integer`)
  }
  return value as number
}

export function parseBudgetConfig(value: unknown): BudgetConfig {
  const raw = objectValue(value, 'budget file')
  if (raw.schema_version !== 1) throw new Error('budget file must use schema version 1')
  if (!Array.isArray(raw.assets) || !Array.isArray(raw.interactions)) {
    throw new Error('budget file must contain asset and interaction arrays')
  }

  const names = new Set<string>()
  const assets = raw.assets.map((value, index): AssetBudget => {
    const asset = objectValue(value, `asset budget ${index}`)
    if (typeof asset.name !== 'string' || asset.name === '' || names.has(asset.name)) {
      throw new Error(`asset budget ${index} must have a unique non-empty name`)
    }
    names.add(asset.name)
    if (typeof asset.manifest_entry !== 'string' || asset.manifest_entry === '') {
      throw new Error(`asset budget ${asset.name} must name a manifest entry`)
    }
    if (asset.kind !== 'javascript' && asset.kind !== 'css') {
      throw new Error(`asset budget ${asset.name} has an unsupported kind`)
    }
    if (asset.compression !== 'brotli' && asset.compression !== 'gzip') {
      throw new Error(`asset budget ${asset.name} has an unsupported compression mode`)
    }
    return {
      name: asset.name,
      manifest_entry: asset.manifest_entry,
      kind: asset.kind,
      compression: asset.compression,
      max_bytes: positiveInteger(asset.max_bytes, `asset budget ${asset.name} max_bytes`),
    }
  })

  const interactions = raw.interactions.map((value, index): InteractionBudget => {
    const interaction = objectValue(value, `interaction budget ${index}`)
    if (
      typeof interaction.name !== 'string' ||
      interaction.name === '' ||
      names.has(interaction.name)
    ) {
      throw new Error(`interaction budget ${index} must have a unique non-empty name`)
    }
    names.add(interaction.name)
    if (interaction.percentile !== 'p95') {
      throw new Error(`interaction budget ${interaction.name} must use p95`)
    }
    if (interaction.samples !== 100) {
      throw new Error(`interaction budget ${interaction.name} must use 100 samples`)
    }
    return {
      name: interaction.name,
      percentile: 'p95',
      samples: 100,
      max_ms: positiveInteger(interaction.max_ms, `interaction budget ${interaction.name} max_ms`),
    }
  })

  return { schema_version: 1, assets, interactions }
}

function parseManifest(value: unknown): Record<string, ManifestEntry> {
  const raw = objectValue(value, 'Vite manifest')
  const manifest: Record<string, ManifestEntry> = {}
  for (const [name, value] of Object.entries(raw)) {
    manifest[name] = objectValue(value, `Vite manifest entry ${name}`) as ManifestEntry
  }
  return manifest
}

function initialAssetFiles(
  manifest: Record<string, ManifestEntry>,
  rootEntry: string,
  kind: AssetKind,
): string[] {
  if (!(rootEntry in manifest)) throw new Error(`Vite manifest entry is missing: ${rootEntry}`)
  const visited = new Set<string>()
  const files = new Set<string>()
  const visit = (name: string): void => {
    if (visited.has(name)) return
    visited.add(name)
    const entry = manifest[name]
    if (entry === undefined) throw new Error(`Vite manifest entry is missing: ${name}`)

    if (kind === 'javascript') {
      if (typeof entry.file !== 'string') {
        throw new Error(`Vite manifest entry ${name} has no JavaScript file`)
      }
      if (entry.file.endsWith('.js')) files.add(entry.file)
    } else if (entry.css !== undefined) {
      if (!Array.isArray(entry.css) || entry.css.some((file) => typeof file !== 'string')) {
        throw new Error(`Vite manifest entry ${name} has malformed CSS files`)
      }
      for (const file of entry.css as string[]) files.add(file)
    }

    if (entry.imports === undefined) return
    if (!Array.isArray(entry.imports) || entry.imports.some((entry) => typeof entry !== 'string')) {
      throw new Error(`Vite manifest entry ${name} has malformed imports`)
    }
    for (const imported of entry.imports as string[]) visit(imported)
  }
  visit(rootEntry)
  if (files.size === 0)
    throw new Error(`Vite manifest entry ${rootEntry} has no initial ${kind} assets`)
  return [...files].sort()
}

function compressedBytes(content: Buffer, mode: CompressionMode): number {
  if (mode === 'brotli') {
    return brotliCompressSync(content, {
      params: { [constants.BROTLI_PARAM_QUALITY]: 11 },
    }).byteLength
  }
  return gzipSync(content, { level: 9 }).byteLength
}

async function measureAsset(
  root: string,
  manifest: Record<string, ManifestEntry>,
  budget: AssetBudget,
): Promise<number> {
  const files = initialAssetFiles(manifest, budget.manifest_entry, budget.kind)
  let total = 0
  for (const file of files) {
    let content: Buffer
    try {
      content = await readFile(resolve(root, file))
    } catch {
      throw new Error(`budget asset is missing: ${file}`)
    }
    total += compressedBytes(content, budget.compression)
  }
  return total
}

function exceeded(name: string, actual: number, maximum: number, unit: 'byte' | 'ms'): Error {
  const difference = actual - maximum
  const suffix = unit === 'byte' && difference !== 1 ? 'bytes' : unit
  return new Error(`${name} exceeds its budget by ${difference} ${suffix}`)
}

export async function checkBudgets(
  root: string,
  value: unknown,
  measurements?: InteractionMeasurements,
): Promise<BudgetResult[]> {
  const config = parseBudgetConfig(value)
  let manifestValue: unknown
  try {
    manifestValue = JSON.parse(await readFile(resolve(root, '.vite/manifest.json'), 'utf8'))
  } catch {
    throw new Error('Vite manifest is missing or is not valid JSON')
  }
  const manifest = parseManifest(manifestValue)
  const results: BudgetResult[] = []

  for (const budget of config.assets) {
    const actual = await measureAsset(root, manifest, budget)
    if (actual > budget.max_bytes) throw exceeded(budget.name, actual, budget.max_bytes, 'byte')
    results.push({ name: budget.name, actual, maximum: budget.max_bytes, unit: 'bytes' })
  }

  if (measurements !== undefined) {
    for (const budget of config.interactions) {
      const measurement = measurements[budget.name]
      if (measurement === undefined) {
        throw new Error(`interaction measurement is missing: ${budget.name}`)
      }
      if (measurement.percentile !== budget.percentile) {
        throw new Error(`interaction ${budget.name} must measure ${budget.percentile}`)
      }
      if (measurement.samples !== budget.samples) {
        throw new Error(`interaction ${budget.name} must use ${budget.samples} samples`)
      }
      if (!Number.isFinite(measurement.value_ms) || measurement.value_ms < 0) {
        throw new Error(`interaction ${budget.name} has an invalid measurement`)
      }
      if (measurement.value_ms > budget.max_ms) {
        throw exceeded(budget.name, measurement.value_ms, budget.max_ms, 'ms')
      }
      results.push({
        name: budget.name,
        actual: measurement.value_ms,
        maximum: budget.max_ms,
        unit: 'ms',
      })
    }
  }

  return results.sort((left, right) => left.name.localeCompare(right.name))
}

async function readJSON(path: string, label: string): Promise<unknown> {
  try {
    return JSON.parse(await readFile(path, 'utf8'))
  } catch {
    throw new Error(`${label} is missing or is not valid JSON`)
  }
}

if (import.meta.main) {
  const root = resolve(process.argv[2] ?? 'dist')
  const budgetPath = resolve(process.argv[3] ?? 'budgets.json')
  const results = await checkBudgets(root, await readJSON(budgetPath, 'budget file'))
  for (const result of results) {
    process.stdout.write(
      `${result.name}: ${result.actual} ${result.unit} (budget ${result.maximum} ${result.unit})\n`,
    )
  }
}
