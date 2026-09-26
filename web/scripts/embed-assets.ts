import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'

import { validateDistribution } from './validate-assets'

export async function embedDistribution(
  sourceRoot: string,
  targetRoot: string,
  check: boolean,
): Promise<void> {
  const source = await validateDistribution(sourceRoot)
  if (check) {
    let target
    try {
      target = await validateDistribution(targetRoot)
    } catch (error) {
      throw new Error(`embedded web distribution is stale: ${String(error)}`)
    }
    if (JSON.stringify(source.files) !== JSON.stringify(target.files)) {
      throw new Error('embedded web distribution is stale: filenames differ')
    }
    for (const name of source.files) {
      const [sourceBytes, targetBytes] = await Promise.all([
        readFile(resolve(sourceRoot, name)),
        readFile(resolve(targetRoot, name)),
      ])
      if (!sourceBytes.equals(targetBytes)) {
        throw new Error(`embedded web distribution is stale: ${name} differs`)
      }
    }
    return
  }

  await rm(targetRoot, { recursive: true, force: true })
  for (const name of source.files) {
    const target = resolve(targetRoot, name)
    await mkdir(dirname(target), { recursive: true })
    await writeFile(target, await readFile(resolve(sourceRoot, name)))
  }
}

if (import.meta.main) {
  const check = process.argv.includes('--check')
  await embedDistribution(resolve('dist'), resolve('../internal/web/dist'), check)
  process.stdout.write(`${check ? 'checked' : 'updated'} embedded web distribution\n`)
}
