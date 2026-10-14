import { spawnSync, type ChildProcess } from 'node:child_process'
import { readdirSync, rmSync, writeFileSync } from 'node:fs'
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve, sep } from 'node:path'

const runPrefix = 'moneyflow-e2e-bin-'
const ownerFile = '.moneyflow-e2e-owner'
const serversDirectory = 'servers'

// Share executables across workers, but keep each invocation's build and profiles separate.
export default async function prepareE2EBinaries(): Promise<() => Promise<void>> {
  const directory = await createE2ERun(tmpdir())
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
    await removeE2ERun(directory)
    throw error
  }
  return async () => {
    delete process.env.MONEYFLOW_E2E_BIN_DIR
    await removeE2ERun(directory)
  }
}

export function e2eBinary(name: 'moneyflow' | 'webtestserver' | 'seedprofile'): string {
  return join(e2eRunDirectory(), process.platform === 'win32' ? `${name}.exe` : name)
}

/**
 * Creates a run directory owned by this process. A runner killed with SIGKILL skips global
 * teardown, so this first removes earlier runs whose owner has exited, with their servers.
 */
export async function createE2ERun(parent: string): Promise<string> {
  await removeAbandonedE2ERuns(parent)
  const directory = await mkdtemp(join(parent, runPrefix))
  await mkdir(join(directory, serversDirectory))
  await writeFile(join(directory, ownerFile), `${process.pid}\n${processStart(process.pid)}\n`)
  return directory
}

/** Kills servers that outlived the worker that started them, then deletes the run. */
export async function removeE2ERun(directory: string): Promise<void> {
  killRecordedServers(directory)
  await rm(directory, { recursive: true, force: true })
}

/**
 * Records a server so global teardown can kill it if its worker dies without stopping it.
 * The record disappears when the server exits.
 */
export function recordE2EServer(child: ChildProcess): void {
  if (child.pid === undefined) return
  const record = join(e2eRunDirectory(), serversDirectory, String(child.pid))
  writeFileSync(record, '')
  child.once('exit', () => rmSync(record, { force: true }))
}

function e2eRunDirectory(): string {
  const directory = process.env.MONEYFLOW_E2E_BIN_DIR
  if (!directory) throw new Error('Prepare E2E binaries before starting a test server.')
  return directory
}

async function removeAbandonedE2ERuns(parent: string): Promise<void> {
  for (const entry of await readdir(parent)) {
    if (!entry.startsWith(runPrefix)) continue
    const directory = join(parent, entry)
    const owner = await readOwner(directory)
    if (owner === undefined || ownerRunning(owner)) continue
    await removeE2ERun(directory)
  }
}

interface Owner {
  pid: number
  start: string
}

// A directory without a readable owner belongs to another user, an older harness, or a run
// that is still being created, so it is left alone.
async function readOwner(directory: string): Promise<Owner | undefined> {
  let text: string
  try {
    text = await readFile(join(directory, ownerFile), 'utf8')
  } catch (error) {
    if (hasCode(error, 'ENOENT', 'ENOTDIR', 'EACCES')) return undefined
    throw new Error(`Read E2E run owner in ${directory}: ${String(error)}`, { cause: error })
  }
  const [pidText = '', start = ''] = text.split('\n')
  const pid = parsePID(pidText)
  return pid === undefined || start === '' ? undefined : { pid, start }
}

// The kernel may give a dead owner's PID to an unrelated process, so the owner counts as
// running only while that PID still has the start time recorded with it.
function ownerRunning(owner: Owner): boolean {
  return processExists(owner.pid) && processStart(owner.pid) === owner.start
}

// A fixed locale and time zone keep the text comparable between runs. Windows has no `ps`;
// browser tests do not run there, so every owner keeps the same placeholder.
function processStart(pid: number): string {
  if (process.platform === 'win32') return 'unknown'
  const ps = spawnSync('ps', ['-ww', '-o', 'lstart=', '-p', String(pid)], {
    encoding: 'utf8',
    env: { ...process.env, LC_ALL: 'C', TZ: 'UTC' },
  })
  if (ps.error) throw new Error(`Read start time of process ${pid}: ${ps.error.message}`)
  return ps.stdout.trim()
}

function killRecordedServers(directory: string): void {
  let records: string[]
  try {
    records = readdirSync(join(directory, serversDirectory))
  } catch (error) {
    if (hasCode(error, 'ENOENT')) return
    throw error
  }
  for (const record of records) {
    const pid = parsePID(record)
    if (pid === undefined || !isServerFromRun(pid, directory)) continue
    killProcessGroup(pid)
  }
}

// The kernel may reuse a recorded PID after the server exits, so a process counts as this
// run's server only while its command still starts with an executable in the run directory.
// `-ww` keeps Linux `ps` from cutting a long command to $COLUMNS when writing to a pipe.
function isServerFromRun(pid: number, directory: string): boolean {
  if (process.platform === 'win32') return false
  const ps = spawnSync('ps', ['-ww', '-o', 'command=', '-p', String(pid)], {
    encoding: 'utf8',
  })
  if (ps.error) throw new Error(`Inspect recorded E2E server ${pid}: ${ps.error.message}`)
  return ps.stdout.trimStart().startsWith(join(directory, sep))
}

// Detached servers lead their own process group; the scenario server shares its worker's.
function killProcessGroup(pid: number): void {
  try {
    process.kill(-pid, 'SIGKILL')
    return
  } catch (error) {
    if (!hasCode(error, 'ESRCH')) throw error
  }
  try {
    process.kill(pid, 'SIGKILL')
  } catch (error) {
    if (!hasCode(error, 'ESRCH')) throw error
  }
}

function processExists(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    if (hasCode(error, 'ESRCH')) return false
    if (hasCode(error, 'EPERM')) return true
    throw error
  }
}

function parsePID(text: string): number | undefined {
  if (!/^[1-9][0-9]*$/.test(text.trim())) return undefined
  const pid = Number(text.trim())
  return Number.isSafeInteger(pid) ? pid : undefined
}

function hasCode(error: unknown, ...codes: string[]): boolean {
  return error instanceof Error && 'code' in error && codes.includes(String(error.code))
}
