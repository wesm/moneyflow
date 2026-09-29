import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { embedDistribution } from './embed-assets'

const marker = '{"schema_version":1,"kind":"moneyflow-production","entry":"index.html"}'
const manifest = JSON.stringify({
  'index.html': {
    file: 'assets/index-Ab12_cd3.js',
    isEntry: true,
    src: 'index.html',
  },
})
const index =
  '<base href="__MONEYFLOW_BASE_HREF__"><meta name="moneyflow-base-path" content="__MONEYFLOW_BASE_PATH__"><meta name="moneyflow-mutation-token" content="__MONEYFLOW_MUTATION_TOKEN__"><link rel="canonical" href="__MONEYFLOW_CANONICAL_URL__"><body>__MONEYFLOW_ORIGIN_WARNING__<script src="./assets/index-Ab12_cd3.js"></script>'

describe('production asset embedding', () => {
  const directories: string[] = []

  afterEach(async () => {
    await Promise.all(directories.splice(0).map((directory) => rm(directory, { recursive: true })))
  })

  async function fixture(): Promise<{ source: string; target: string }> {
    const root = await mkdtemp(join(tmpdir(), 'moneyflow-embed-test-'))
    directories.push(root)
    const source = join(root, 'source')
    const target = join(root, 'target')
    await mkdir(join(source, '.vite'), { recursive: true })
    await mkdir(join(source, 'assets'), { recursive: true })
    await writeFile(join(source, '.moneyflow-production.json'), marker)
    await writeFile(join(source, '.vite/manifest.json'), manifest)
    await writeFile(join(source, 'index.html'), index)
    await writeFile(join(source, 'assets/index-Ab12_cd3.js'), 'compiled')
    return { source, target }
  }

  it('replaces the target with the validated source bytes', async () => {
    const { source, target } = await fixture()
    await mkdir(target, { recursive: true })
    await writeFile(join(target, 'stale.txt'), 'stale')

    await embedDistribution(source, target, false)

    await expect(readFile(join(target, 'stale.txt'))).rejects.toThrow()
    await expect(readFile(join(target, 'assets/index-Ab12_cd3.js'), 'utf8')).resolves.toBe(
      'compiled',
    )
  })

  it('check mode compares every name and byte without writing', async () => {
    const { source, target } = await fixture()
    await embedDistribution(source, target, false)
    await expect(embedDistribution(source, target, true)).resolves.toBeUndefined()

    await writeFile(join(target, 'index.html'), 'edited')
    await expect(embedDistribution(source, target, true)).rejects.toThrow(/stale/i)
    await expect(readFile(join(target, 'index.html'), 'utf8')).resolves.toBe('edited')
  })
})
