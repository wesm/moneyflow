import { spawn, type ChildProcess } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { cp, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { availablePort, waitForApplication } from './e2e-server'
import { e2eBinary, recordE2EServer } from './e2e-binaries'

export interface Observation {
  rows: Record<
    string,
    { date: string; merchant: string; category: string; minor: number; hidden: boolean }
  >
  targets: string[]
  pending: number
  redo: number
  snapshot_calls: number
  calls: Array<{ method: string; target?: string; outcome: string }>
  audit: string
}

export interface ScenarioServer {
  url: string
  root: string
  observe(): Promise<Observation>
  fault(fault: 'block' | 'block-after' | 'reject' | 'unknown'): Promise<void>
  /** Arms the next provider snapshot read: wait for release, fail, or omit five rows. */
  refreshFault(fault: 'block' | 'fail' | 'remove'): Promise<void>
  /** Holds or releases the refresh lease as another interface, such as the TUI, would. */
  otherRefresh(renderer: string, hold: boolean): Promise<void>
  release(): Promise<void>
  advance(seconds: number): Promise<void>
  restart(crash?: boolean): Promise<void>
  stop(): Promise<void>
  finish(failed: boolean): Promise<void>
}

// A thrown checkpoint must retain evidence even before Playwright records failure.
export async function withScenarioServer(
  root: string,
  run: (server: ScenarioServer) => Promise<void>,
): Promise<void> {
  const server = await startScenarioServer(root)
  let failed = true
  try {
    await run(server)
    failed = false
  } finally {
    await server.finish(failed)
  }
}

// The caller owns the private run directory. Stopping a process never deletes its
// profile or simulated remote account; finish retains everything after failure.
export async function startScenarioServer(requestedRoot: string): Promise<ScenarioServer> {
  const root = resolve(requestedRoot)
  await mkdir(root, { recursive: true, mode: 0o700 })
  const token = randomUUID()
  // Worktrees may have writable ancestors; live profiles use the private TMPDIR.
  const profileRoot = await mkdtemp(join(tmpdir(), 'moneyflow-scenario-'))
  await writeFile(join(profileRoot, '.moneyflow-webtest-root'), token, { mode: 0o600 })
  await writeFile(join(profileRoot, 'scenario.json'), JSON.stringify({ fixture: 'editing-v1' }), {
    mode: 0o600,
  })
  const binary = e2eBinary('webtestserver')
  const repository = resolve(process.cwd(), '..')
  const port = await availablePort()
  const origin = `http://127.0.0.1:${port}`
  let child: ChildProcess | undefined
  let logs = ''
  let url = ''
  async function start(): Promise<void> {
    child = spawn(
      binary,
      [
        '--home',
        profileRoot,
        '--root-token',
        token,
        '--scenario',
        join(profileRoot, 'scenario.json'),
        '--listen',
        `127.0.0.1:${port}`,
      ],
      {
        cwd: repository,
        stdio: ['ignore', 'ignore', 'pipe'],
      },
    )
    recordE2EServer(child)
    child.stderr?.setEncoding('utf8')
    child.stderr?.on('data', (chunk: string) => {
      logs += chunk
    })
    url = await waitForApplication(child, `${origin}/`, () => logs)
  }
  async function stop(crash = false): Promise<void> {
    const running = child
    child = undefined
    if (!running || running.exitCode !== null || running.signalCode !== null) return
    await new Promise<void>((resolveExit) => {
      const timer = setTimeout(() => running.kill('SIGKILL'), 6_000)
      running.once('exit', () => {
        clearTimeout(timer)
        resolveExit()
      })
      running.kill(crash ? 'SIGKILL' : 'SIGTERM')
    })
  }
  async function control(path: string, body?: unknown): Promise<unknown> {
    const response = await fetch(
      `${origin}/__moneyflow_scenario/${path}`,
      body === undefined
        ? {}
        : {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          },
    )
    if (!response.ok)
      throw new Error(`Scenario ${path}: ${response.status} ${await response.text()}`)
    return await response.json()
  }
  async function finish(failed: boolean): Promise<void> {
    await stop()
    try {
      if (failed) {
        await cp(profileRoot, join(root, 'home'), { recursive: true })
        await writeFile(join(root, 'server.log'), logs, { mode: 0o600 })
      } else {
        await rm(root, { recursive: true, force: true })
      }
    } finally {
      await rm(profileRoot, { recursive: true, force: true })
    }
  }
  try {
    await start()
  } catch (error) {
    await finish(true)
    throw error
  }
  return {
    get url() {
      return url
    },
    root,
    async observe() {
      return (await control('observe')) as Observation
    },
    async fault(fault) {
      await control('fault', { fault })
    },
    async refreshFault(fault) {
      await control('refresh-fault', { fault })
    },
    async otherRefresh(renderer, hold) {
      await control('other-refresh', { renderer, hold })
    },
    async release() {
      await control('release', {})
    },
    async advance(seconds) {
      await control('advance', { seconds })
    },
    async restart(crash = false) {
      await stop(crash)
      await start()
    },
    stop,
    finish,
  }
}
