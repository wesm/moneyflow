import { readdir, readFile } from 'node:fs/promises'
import { relative, resolve, sep } from 'node:path'

export const productionMarker = {
  schema_version: 1,
  kind: 'moneyflow-production',
  entry: 'index.html',
} as const

export interface ValidatedDistribution {
  files: string[]
  hashedAssets: string[]
}

interface ManifestEntry {
  file?: unknown
  css?: unknown
  assets?: unknown
  imports?: unknown
  dynamicImports?: unknown
  isEntry?: unknown
}

const requiredFiles = new Set(['index.html', '.vite/manifest.json', '.moneyflow-production.json'])
const safeName = /^(?:[A-Za-z0-9._-]+\/)*[A-Za-z0-9._-]+$/
const hashedAsset = /^assets\/(?:[A-Za-z0-9_-]+\/)*[A-Za-z0-9._-]+-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$/

async function listFiles(root: string, directory = root): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true })
  const files: string[] = []
  for (const entry of entries) {
    const path = resolve(directory, entry.name)
    if (entry.isSymbolicLink())
      throw new Error(`distribution contains a symbolic link: ${entry.name}`)
    if (entry.isDirectory()) files.push(...(await listFiles(root, path)))
    else if (entry.isFile()) files.push(relative(root, path).split(sep).join('/'))
    else throw new Error(`distribution contains an unsupported entry: ${entry.name}`)
  }
  return files.sort()
}

function parseObject(content: string, label: string): Record<string, unknown> {
  let value: unknown
  try {
    value = JSON.parse(content)
  } catch {
    throw new Error(`${label} is not valid JSON`)
  }
  if (value === null || Array.isArray(value) || typeof value !== 'object') {
    throw new Error(`${label} must be a JSON object`)
  }
  return value as Record<string, unknown>
}

function manifestReferences(manifest: Record<string, unknown>): Set<string> {
  const references = new Set<string>()
  const entries = Object.entries(manifest)
  if (entries.length === 0) throw new Error('Vite manifest must contain an entry')
  let entryCount = 0
  for (const [key, rawEntry] of entries) {
    if (rawEntry === null || Array.isArray(rawEntry) || typeof rawEntry !== 'object') {
      throw new Error(`Vite manifest entry ${key} is malformed`)
    }
    const entry = rawEntry as ManifestEntry
    if (entry.isEntry === true) entryCount += 1
    for (const field of ['file', 'css', 'assets'] as const) {
      const value = entry[field]
      const names = typeof value === 'string' ? [value] : Array.isArray(value) ? value : []
      if (value !== undefined && names.length === 0) {
        throw new Error(`Vite manifest ${field} field is malformed`)
      }
      for (const name of names) {
        if (typeof name !== 'string')
          throw new Error(`Vite manifest ${field} contains a non-string`)
        references.add(name)
      }
    }
    for (const field of ['imports', 'dynamicImports'] as const) {
      const value = entry[field]
      if (value === undefined) continue
      if (!Array.isArray(value) || value.some((name) => typeof name !== 'string')) {
        throw new Error(`Vite manifest ${field} field is malformed`)
      }
      for (const name of value as string[]) {
        if (!(name in manifest)) throw new Error(`Vite manifest references missing entry: ${name}`)
      }
    }
  }
  if (entryCount !== 1) throw new Error('Vite manifest must contain exactly one entry')
  return references
}

function validateIndex(content: string): void {
  for (const [placeholder, label] of [
    ['__MONEYFLOW_BASE_PATH__', 'base-path'],
    ['__MONEYFLOW_BASE_HREF__', 'base-href'],
    ['__MONEYFLOW_MUTATION_TOKEN__', 'mutation-token'],
    ['__MONEYFLOW_CANONICAL_URL__', 'canonical-URL'],
    ['__MONEYFLOW_ORIGIN_WARNING__', 'origin-warning'],
  ] as const) {
    if (content.split(placeholder).length - 1 !== 1) {
      throw new Error(`index.html must contain exactly one ${label} placeholder`)
    }
  }
  if (/\/(?:src|@vite)\//i.test(content)) {
    throw new Error('index.html is a development placeholder, not compiled production output')
  }
  if (/(?:src|href)\s*=\s*["']\/(?!\/)/i.test(content)) {
    throw new Error('index.html contains an absolute asset URL')
  }
  if (/<script\b(?![^>]*\bsrc=)[^>]*>/i.test(content) || /<style\b/i.test(content)) {
    throw new Error('index.html contains inline script or style')
  }
  if (/\sstyle\s*=|\son[a-z]+\s*=/i.test(content)) {
    throw new Error('index.html contains an inline style or event attribute')
  }
  if (/https?:\/\/|(?:src|href)\s*=\s*["']\/\//i.test(content)) {
    throw new Error('index.html contains a remote URL')
  }
}

export async function validateDistribution(root: string): Promise<ValidatedDistribution> {
  const files = await listFiles(root)
  const fileSet = new Set(files)
  for (const required of requiredFiles) {
    if (!fileSet.has(required)) throw new Error(`distribution is missing ${required}`)
  }
  for (const name of files) {
    if (!safeName.test(name) || name.includes('..'))
      throw new Error(`unsafe distribution filename: ${name}`)
    if (name.endsWith('.map')) throw new Error(`source maps are forbidden: ${name}`)
    const hidden = name.split('/').find((part) => part.startsWith('.'))
    if (hidden && name !== '.vite/manifest.json' && name !== '.moneyflow-production.json') {
      throw new Error(`hidden distribution file is forbidden: ${name}`)
    }
  }

  const marker = parseObject(
    await readFile(resolve(root, '.moneyflow-production.json'), 'utf8'),
    'production marker',
  )
  if (JSON.stringify(marker) !== JSON.stringify(productionMarker)) {
    throw new Error('production marker does not identify a Moneyflow production build')
  }

  const manifest = parseObject(
    await readFile(resolve(root, '.vite/manifest.json'), 'utf8'),
    'Vite manifest',
  )
  const references = manifestReferences(manifest)
  for (const name of references) {
    if (!hashedAsset.test(name))
      throw new Error(`manifest asset has a malformed content hash: ${name}`)
    if (!fileSet.has(name)) throw new Error(`missing manifest asset: ${name}`)
  }
  for (const name of files) {
    if (!requiredFiles.has(name) && !references.has(name)) {
      throw new Error(`unreferenced distribution asset: ${name}`)
    }
  }

  validateIndex(await readFile(resolve(root, 'index.html'), 'utf8'))
  return { files, hashedAssets: [...references].sort() }
}

if (import.meta.main) {
  const root = resolve(process.argv[2] ?? 'dist')
  await validateDistribution(root)
  process.stdout.write(`validated production distribution: ${root}\n`)
}
