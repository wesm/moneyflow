import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { assertArtifactCurrent, canonicalNewline } from './generate-api'

describe('generated API artifact checks', () => {
  const directories: string[] = []

  afterEach(async () => {
    await Promise.all(directories.splice(0).map((directory) => rm(directory, { recursive: true })))
  })

  async function artifact(content: string): Promise<string> {
    const directory = await mkdtemp(join(tmpdir(), 'moneyflow-generate-test-'))
    directories.push(directory)
    const path = join(directory, 'artifact')
    await writeFile(path, content)
    return path
  }

  it.each(['canonical OpenAPI document', 'generated browser API schema'])(
    'fails after editing the %s',
    async (label) => {
      const path = await artifact('edited\n')
      await expect(
        assertArtifactCurrent(path, canonicalNewline('generated'), label),
      ).rejects.toThrow(`${label} is stale`)
    },
  )

  it('accepts canonical bytes and exactly one trailing newline', async () => {
    const generated = canonicalNewline('generated\n\n')
    const path = await artifact('generated\n')
    await expect(assertArtifactCurrent(path, generated, 'artifact')).resolves.toBeUndefined()
  })
})
