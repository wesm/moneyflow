import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import {
  checkBudgets,
  type BudgetConfig,
  type InitialAssetBudget,
  type InteractionMeasurements,
} from './check-budgets'

const manifest = {
  'index.html': {
    file: 'assets/index-Ab12_cd3.js',
    css: ['assets/index-Zy98_Xw7.css'],
    imports: ['_shared.js'],
    isEntry: true,
  },
  '_shared.js': { file: 'assets/shared-Qw34_er5.js' },
  'src/Later.svelte': { file: 'assets/later-Lm56_ab7.js', isDynamicEntry: true },
}

const interactions: InteractionMeasurements = {
  chart_cursor: { percentile: 'p95', samples: 100, value_ms: 10 },
  chart_drill: { percentile: 'p95', samples: 100, value_ms: 20 },
}

function config(maxBytes = 1_000_000, maxMilliseconds = 1_000): BudgetConfig {
  return {
    schema_version: 1,
    assets: [
      {
        name: 'initial_javascript_brotli',
        scope: 'initial',
        manifest_entry: 'index.html',
        kind: 'javascript',
        compression: 'brotli',
        max_bytes: maxBytes,
      },
    ],
    interactions: [
      {
        name: 'chart_cursor',
        percentile: 'p95',
        samples: 100,
        max_ms: maxMilliseconds,
      },
    ],
  }
}

describe('web performance budgets', () => {
  const directories: string[] = []

  afterEach(async () => {
    await Promise.all(directories.splice(0).map((directory) => rm(directory, { recursive: true })))
  })

  async function distribution(): Promise<string> {
    const directory = await mkdtemp(join(tmpdir(), 'moneyflow-budget-test-'))
    directories.push(directory)
    const files: Record<string, string> = {
      '.vite/manifest.json': JSON.stringify(manifest),
      'assets/index-Ab12_cd3.js': 'export const application = "moneyflow"'.repeat(30),
      'assets/shared-Qw34_er5.js': 'export const shared = true'.repeat(20),
      'assets/later-Lm56_ab7.js': 'export const later = "loaded on demand"'.repeat(40),
      'assets/index-Zy98_Xw7.css': '.moneyflow { color: #123456; }'.repeat(20),
    }
    for (const [name, value] of Object.entries(files)) {
      await mkdir(join(directory, name, '..'), { recursive: true })
      await writeFile(join(directory, name), value)
    }
    return directory
  }

  it('accepts exact byte and millisecond boundaries', async () => {
    const root = await distribution()
    const measured = await checkBudgets(root, config(), interactions)
    const bytes = measured.find((result) => result.name === 'initial_javascript_brotli')!.actual
    const results = await checkBudgets(root, config(bytes, 10), interactions)
    expect(results.map(({ name, actual }) => ({ name, actual }))).toEqual([
      { name: 'chart_cursor', actual: 10 },
      { name: 'initial_javascript_brotli', actual: bytes },
    ])
  })

  it('rejects a one-byte or one-millisecond regression', async () => {
    const root = await distribution()
    const measured = await checkBudgets(root, config(), interactions)
    const bytes = measured.find((result) => result.name === 'initial_javascript_brotli')!.actual
    await expect(checkBudgets(root, config(bytes - 1), interactions)).rejects.toThrow(/1 byte/i)
    await expect(checkBudgets(root, config(1_000_000, 9), interactions)).rejects.toThrow(/1 ms/i)
  })

  it('rejects a missing manifest entry and unsupported compression', async () => {
    const root = await distribution()
    const missing = config()
    ;(missing.assets[0] as InitialAssetBudget).manifest_entry = 'missing.html'
    await expect(checkBudgets(root, missing, interactions)).rejects.toThrow(/manifest entry/i)

    const invalid = config() as unknown as { assets: Array<{ compression: string }> }
    invalid.assets[0]!.compression = 'deflate'
    await expect(
      checkBudgets(root, invalid as unknown as BudgetConfig, interactions),
    ).rejects.toThrow(/compression/i)
  })

  it('counts every chunk, including ones the entry does not import, in a total budget', async () => {
    const root = await distribution()
    const budgets = config()
    budgets.assets.push({
      name: 'total_javascript_brotli',
      scope: 'all',
      kind: 'javascript',
      compression: 'brotli',
      max_bytes: 1_000_000,
    })
    const measured = await checkBudgets(root, budgets, interactions)
    const initial = measured.find((result) => result.name === 'initial_javascript_brotli')!.actual
    const total = measured.find((result) => result.name === 'total_javascript_brotli')!.actual
    expect(total).toBeGreaterThan(initial)

    budgets.assets[1]!.max_bytes = total - 1
    await expect(checkBudgets(root, budgets, interactions)).rejects.toThrow(
      /total_javascript_brotli exceeds its budget by 1 byte/i,
    )
  })

  it('requires an initial budget to name its manifest entry and rejects an unknown scope', async () => {
    const root = await distribution()
    const noEntry = config() as unknown as { assets: Array<Record<string, unknown>> }
    delete noEntry.assets[0]!.manifest_entry
    await expect(
      checkBudgets(root, noEntry as unknown as BudgetConfig, interactions),
    ).rejects.toThrow(/manifest entry/i)

    const unknown = config() as unknown as { assets: Array<Record<string, unknown>> }
    unknown.assets[0]!.scope = 'lazy'
    await expect(
      checkBudgets(root, unknown as unknown as BudgetConfig, interactions),
    ).rejects.toThrow(/scope must be "initial" or "all"/)

    delete unknown.assets[0]!.scope
    await expect(
      checkBudgets(root, unknown as unknown as BudgetConfig, interactions),
    ).rejects.toThrow(/scope must be "initial" or "all"/)
  })

  it('rejects counting every chunk of a kind other than javascript', async () => {
    const root = await distribution()
    const css = config() as unknown as { assets: Array<Record<string, unknown>> }
    css.assets[0] = {
      name: 'total_css_brotli',
      scope: 'all',
      kind: 'css',
      compression: 'brotli',
      max_bytes: 1_000_000,
    }
    await expect(checkBudgets(root, css as unknown as BudgetConfig, interactions)).rejects.toThrow(
      /total_css_brotli can count every chunk only for javascript/,
    )
  })

  it('rejects malformed schema and mismatched interaction measurements', async () => {
    const root = await distribution()
    await expect(
      checkBudgets(
        root,
        { ...config(), schema_version: 2 } as unknown as BudgetConfig,
        interactions,
      ),
    ).rejects.toThrow(/schema version 1/i)
    await expect(
      checkBudgets(root, config(), {
        chart_cursor: { percentile: 'p95', samples: 99, value_ms: 10 },
      }),
    ).rejects.toThrow(/100 samples/i)
  })

  it('reports asset and interaction results in stable name order', async () => {
    const root = await distribution()
    const budgets = config()
    budgets.assets[0]!.name = 'z_asset'
    budgets.interactions = [
      { name: 'chart_drill', percentile: 'p95', samples: 100, max_ms: 1_000 },
      { name: 'chart_cursor', percentile: 'p95', samples: 100, max_ms: 1_000 },
    ]
    const results = await checkBudgets(root, budgets, interactions)
    expect(results.map((result) => result.name)).toEqual(['chart_cursor', 'chart_drill', 'z_asset'])
  })
})
