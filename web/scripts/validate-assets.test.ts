import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { validateDistribution } from './validate-assets'

const marker = { schema_version: 1, kind: 'moneyflow-production', entry: 'index.html' }
const manifest = {
  'index.html': {
    file: 'assets/index-Ab12_cd3.js',
    css: ['assets/index-Zy98_Xw7.css'],
    isEntry: true,
    src: 'index.html',
  },
}
const index = `<!doctype html><html><head><meta name="moneyflow-base-path" content="__MONEYFLOW_BASE_PATH__"><link rel="stylesheet" href="./assets/index-Zy98_Xw7.css"></head><body><div id="app"></div><script type="module" src="./assets/index-Ab12_cd3.js"></script></body></html>`

describe('production asset validation', () => {
  const directories: string[] = []

  afterEach(async () => {
    await Promise.all(directories.splice(0).map((directory) => rm(directory, { recursive: true })))
  })

  async function distribution(
    changes: Partial<Record<string, string | null>> = {},
  ): Promise<string> {
    const directory = await mkdtemp(join(tmpdir(), 'moneyflow-assets-test-'))
    directories.push(directory)
    const files: Record<string, string> = {
      'index.html': index,
      '.vite/manifest.json': JSON.stringify(manifest),
      '.moneyflow-production.json': JSON.stringify(marker),
      'assets/index-Ab12_cd3.js': 'console.log("compiled")',
      'assets/index-Zy98_Xw7.css': 'body{color:#123}',
    }
    for (const [name, value] of Object.entries(changes)) {
      if (value === null) delete files[name]
      else if (value !== undefined) files[name] = value
    }
    for (const [name, value] of Object.entries(files)) {
      await mkdir(join(directory, name, '..'), { recursive: true })
      await writeFile(join(directory, name), value)
    }
    return directory
  }

  it('accepts one complete, self-identifying production distribution', async () => {
    const result = await validateDistribution(await distribution())
    expect(result.files).toEqual([
      '.moneyflow-production.json',
      '.vite/manifest.json',
      'assets/index-Ab12_cd3.js',
      'assets/index-Zy98_Xw7.css',
      'index.html',
    ])
    expect(result.hashedAssets).toEqual(['assets/index-Ab12_cd3.js', 'assets/index-Zy98_Xw7.css'])
  })

  it.each([
    ['index.html', { 'index.html': null }],
    ['manifest', { '.vite/manifest.json': null }],
    ['production marker', { '.moneyflow-production.json': null }],
  ])('rejects a missing %s', async (_label, changes) => {
    await expect(validateDistribution(await distribution(changes))).rejects.toThrow()
  })

  it('rejects a compilation stub and a forged production marker', async () => {
    await expect(
      validateDistribution(
        await distribution({
          'index.html': '<script type="module" src="/src/main.ts"></script>',
        }),
      ),
    ).rejects.toThrow(/placeholder|compiled production/i)
    await expect(
      validateDistribution(
        await distribution({
          '.moneyflow-production.json': JSON.stringify({ ...marker, kind: 'placeholder' }),
        }),
      ),
    ).rejects.toThrow(/production marker/i)
  })

  it('rejects missing and unreferenced assets', async () => {
    await expect(
      validateDistribution(await distribution({ 'assets/index-Ab12_cd3.js': null })),
    ).rejects.toThrow(/missing manifest asset/i)
    await expect(
      validateDistribution(await distribution({ 'assets/extra-Ab12_cd3.js': 'unused' })),
    ).rejects.toThrow(/unreferenced/i)
  })

  it.each([
    ['absolute asset URL', index.replace('./assets/', '/assets/')],
    ['inline script', index.replace('<div id="app"></div>', '<script>alert(1)</script>')],
    ['inline style', index.replace('<body>', '<body style="color:red">')],
    ['inline event handler', index.replace('<body>', '<body onload="alert(1)">')],
    ['remote URL', index.replace('<body>', '<body><img src="https://example.invalid/a.png">')],
    [
      'duplicate placeholder',
      index.replace('</head>', '<meta content="__MONEYFLOW_BASE_PATH__"></head>'),
    ],
  ])('rejects %s in index.html', async (_label, content) => {
    await expect(
      validateDistribution(await distribution({ 'index.html': content })),
    ).rejects.toThrow()
  })

  it.each([
    ['unsafe filename', { 'assets/bad name-Ab12_cd3.js': 'bad' }],
    ['source map', { 'assets/index-Ab12_cd3.js.map': '{}' }],
    ['malformed content hash', { 'assets/nohash.js': 'bad' }],
  ])('rejects a %s', async (_label, additions) => {
    const changedManifest = structuredClone(manifest)
    const filename = Object.keys(additions)[0]!
    changedManifest['index.html'].file = filename
    await expect(
      validateDistribution(
        await distribution({
          '.vite/manifest.json': JSON.stringify(changedManifest),
          'assets/index-Ab12_cd3.js': null,
          ...additions,
        }),
      ),
    ).rejects.toThrow()
  })
})
