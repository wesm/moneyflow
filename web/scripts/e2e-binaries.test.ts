import { spawn, type ChildProcess } from 'node:child_process'
import { once } from 'node:events'
import { access, mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { createE2ERun, removeE2ERun } from './e2e-binaries'

// The worker runs the real recorder under Bun, which loads TypeScript directly, and is then
// killed the way a timed-out Playwright worker is.
const workerSource = `
const { spawn } = await import('node:child_process')
const { recordE2EServer } = await import(process.env.RECORDER)
const child = spawn(process.env.MONEYFLOW_E2E_BIN_DIR + '/moneyflow',
  ['-e', 'setInterval(() => {}, 1000)'],
  { detached: process.env.DETACHED === '1', stdio: 'ignore' })
recordE2EServer(child)
console.log(child.pid)
setInterval(() => {}, 1000)
`

describe.skipIf(process.platform === 'win32')('E2E run cleanup', () => {
  const parents: string[] = []
  const pids: number[] = []

  afterEach(async () => {
    for (const pid of pids.splice(0)) {
      if (alive(pid)) process.kill(pid, 'SIGKILL')
    }
    await Promise.all(parents.splice(0).map((parent) => rm(parent, { recursive: true })))
  })

  async function parentDirectory(): Promise<string> {
    const parent = await mkdtemp(join(tmpdir(), 'moneyflow-e2e-run-test-'))
    parents.push(parent)
    return parent
  }

  async function orphanServer(run: string, detached: boolean): Promise<number> {
    await symlink(process.execPath, join(run, 'moneyflow'))
    const worker = spawn('bun', ['-e', workerSource], {
      env: {
        ...process.env,
        DETACHED: detached ? '1' : '0',
        MONEYFLOW_E2E_BIN_DIR: run,
        RECORDER: resolve(import.meta.dirname, 'e2e-binaries.ts'),
      },
      stdio: ['ignore', 'pipe', 'inherit'],
    })
    const [output] = (await once(worker.stdout, 'data')) as [Buffer]
    const server = Number(output.toString().trim())
    pids.push(server)
    await killAndWait(worker)
    expect(alive(server), 'the server outlives its killed worker').toBe(true)
    return server
  }

  it('teardown kills a detached server whose worker was killed and removes the run', async () => {
    const run = await createE2ERun(await parentDirectory())
    const server = await orphanServer(run, true)

    await removeE2ERun(run)

    await expectExit(server)
    await expect(access(run)).rejects.toThrow()
  })

  it('a new run removes abandoned runs with their servers and keeps other runs', async () => {
    const parent = await parentDirectory()
    const abandoned = await createE2ERun(parent)
    const server = await orphanServer(abandoned, false)
    await writeFile(join(abandoned, '.moneyflow-e2e-owner'), String(await exitedPID()))
    const live = await createE2ERun(parent)
    const unmarked = join(parent, 'moneyflow-e2e-bin-unmarked')
    await mkdir(unmarked)

    const created = await createE2ERun(parent)

    await expectExit(server)
    await expect(access(abandoned)).rejects.toThrow()
    await access(live)
    await access(unmarked)
    await access(created)
  })

  it('teardown leaves a recorded PID alone when it is not a server from the run', async () => {
    const run = await createE2ERun(await parentDirectory())
    const unrelated = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'])
    if (unrelated.pid === undefined) throw new Error('Could not start the unrelated process.')
    pids.push(unrelated.pid)
    await writeFile(join(run, 'servers', String(unrelated.pid)), '')

    await removeE2ERun(run)

    expect(alive(unrelated.pid)).toBe(true)
    await killAndWait(unrelated)
  })
})

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

async function killAndWait(child: ChildProcess): Promise<void> {
  const exited = once(child, 'exit')
  child.kill('SIGKILL')
  await exited
}

async function exitedPID(): Promise<number> {
  const child = spawn(process.execPath, ['-e', ''])
  await once(child, 'exit')
  if (child.pid === undefined) throw new Error('Could not start a short-lived process.')
  return child.pid
}

async function expectExit(pid: number): Promise<void> {
  const deadline = Date.now() + 5_000
  while (alive(pid) && Date.now() < deadline) {
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  expect(alive(pid), `process ${pid} exited`).toBe(false)
}
