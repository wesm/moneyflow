import { spawnSync } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

// Share executables across workers, but keep each invocation's build and profiles separate.
export default async function prepareE2EBinaries(): Promise<() => Promise<void>> {
  const directory = await mkdtemp(join(tmpdir(), 'moneyflow-e2e-bin-'))
  try {
    const build = spawnSync(
      'go',
      [
        'build',
        '-o',
        directory,
        './cmd/moneyflow',
        './internal/tools/webtestserver',
        './internal/tools/seedprofile',
      ],
      { cwd: resolve(import.meta.dirname, '../..'), encoding: 'utf8' },
    )
    if (build.error) throw build.error
    if (build.status !== 0) throw new Error(`Build E2E binaries: ${build.stderr}`)
    process.env.MONEYFLOW_E2E_BIN_DIR = directory
  } catch (error) {
    await rm(directory, { recursive: true, force: true })
    throw error
  }
  return async () => {
    delete process.env.MONEYFLOW_E2E_BIN_DIR
    await rm(directory, { recursive: true, force: true })
  }
}

export function e2eBinary(name: 'moneyflow' | 'webtestserver' | 'seedprofile'): string {
  const directory = process.env.MONEYFLOW_E2E_BIN_DIR
  if (!directory) throw new Error('Prepare E2E binaries before starting a test server.')
  return join(directory, process.platform === 'win32' ? `${name}.exe` : name)
}
