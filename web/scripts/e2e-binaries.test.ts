import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { once } from 'node:events'
import { access, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { createE2ERun, removeE2ERun } from './e2e-binaries'
import { startE2EServer, startOnboardingE2EServer } from './e2e-server'

const recorder = resolve(import.meta.dirname, 'e2e-binaries.ts')

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

const runnerSource = `
const { createE2ERun } = await import(process.env.RECORDER)
console.log(await createE2ERun(process.env.PARENT))
`

describe.skipIf(process.platform === 'win32')('E2E run cleanup', () => {
  const parents: string[] = []
  const pids: number[] = []
  const groups: number[] = []

  afterEach(async () => {
    for (const pid of pids.splice(0)) {
      if (alive(pid)) process.kill(pid, 'SIGKILL')
    }
    for (const group of groups.splice(0)) {
      if (alive(group)) process.kill(-group, 'SIGKILL')
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
        RECORDER: recorder,
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

  // The run's owner exits without teardown, as a runner killed with SIGKILL does.
  async function abandonedRun(parent: string): Promise<string> {
    const runner = spawn('bun', ['-e', runnerSource], {
      env: { ...process.env, PARENT: parent, RECORDER: recorder },
      stdio: ['ignore', 'pipe', 'inherit'],
    })
    let output = ''
    runner.stdout.setEncoding('utf8')
    runner.stdout.on('data', (chunk: string) => (output += chunk))
    const [code] = (await once(runner, 'exit')) as [number | null]
    expect(code, 'the abandoned runner exits normally').toBe(0)
    return output.trim()
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
    const abandoned = await abandonedRun(parent)
    const server = await orphanServer(abandoned, false)
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

  // PID 1 stands in for a reused PID: it is running, but it started before the dead owner.
  it('a new run removes an abandoned run whose owner PID now names another process', async () => {
    const parent = await parentDirectory()
    const abandoned = await abandonedRun(parent)
    const server = await orphanServer(abandoned, true)
    const owner = join(abandoned, '.moneyflow-e2e-owner')
    const [, start] = (await readFile(owner, 'utf8')).split('\n')
    await writeFile(owner, `1\n${start}\n`)

    await createE2ERun(parent)

    await expectExit(server)
    await expect(access(abandoned)).rejects.toThrow()
  })

  it.each([
    ['web', startE2EServer, 'moneyflow'],
    ['onboarding', startOnboardingE2EServer, 'webtestserver'],
  ] as const)(
    'the %s helper stops its server when the server cannot be recorded',
    async (_, start, binary) => {
      const run = await createE2ERun(await parentDirectory())
      await writeFile(join(run, binary), '#!/bin/sh\nsleep 600\n', { mode: 0o755 })
      await rm(join(run, 'servers'), { recursive: true })
      const previous = process.env.MONEYFLOW_E2E_BIN_DIR
      process.env.MONEYFLOW_E2E_BIN_DIR = run
      try {
        await expect(start()).rejects.toThrow(/ENOENT/)
      } finally {
        process.env.MONEYFLOW_E2E_BIN_DIR = previous
        groups.push(...processesFrom(run))
      }
      expect(processesFrom(run)).toEqual([])
    },
  )

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

function processesFrom(run: string): number[] {
  const ps = spawnSync('ps', ['-Ao', 'pid=,command='], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  })
  if (ps.error) throw new Error(`List processes: ${ps.error.message}`)
  if (ps.status !== 0) throw new Error(`List processes: ${ps.stderr}`)
  return ps.stdout
    .split('\n')
    .filter((line) => line.includes(`${run}/`))
    .map((line) => Number.parseInt(line, 10))
}

async function expectExit(pid: number): Promise<void> {
  const deadline = Date.now() + 5_000
  while (alive(pid) && Date.now() < deadline) {
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  expect(alive(pid), `process ${pid} exited`).toBe(false)
}
