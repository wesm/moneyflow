import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { mkdir, rm, writeFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'

import { availablePort, waitForApplication } from './e2e-server'

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
  const profileRoot = join(root, 'home')
  await mkdir(profileRoot, { mode: 0o700 })
  await writeFile(join(profileRoot, '.moneyflow-webtest-root'), token, { mode: 0o600 })
  await writeFile(join(profileRoot, 'scenario.json'), JSON.stringify({ fixture: 'editing-v1' }), {
    mode: 0o600,
  })
  const binary = join(root, process.platform === 'win32' ? 'webtestserver.exe' : 'webtestserver')
  const repository = resolve(process.cwd(), '..')
  const build = spawnSync('go', ['build', '-o', binary, './internal/tools/webtestserver'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) throw new Error(`Build scenario server: ${build.stderr}`)
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
  try {
    await start()
  } catch (error) {
    await stop()
    await writeFile(join(root, 'server.log'), logs, { mode: 0o600 })
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
    async finish(failed) {
      await stop()
      // Binaries are reproducible and large; keep only synthetic state and evidence.
      await rm(binary, { force: true })
      if (failed) await writeFile(join(root, 'server.log'), logs, { mode: 0o600 })
      else await rm(root, { recursive: true, force: true })
    },
  }
}
