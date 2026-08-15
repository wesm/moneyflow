import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

export interface E2EServer {
  basePath: string
  origin: string
  url: string
  stop(): Promise<void>
}

async function availablePort(): Promise<number> {
  return await new Promise((resolvePort, reject) => {
    const server = createServer()
    server.unref()
    server.on('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      if (address === null || typeof address === 'string') {
        server.close(() => reject(new Error('Could not reserve a loopback port.')))
        return
      }
      server.close((error) => (error ? reject(error) : resolvePort(address.port)))
    })
  })
}

function normalizedBasePath(basePath: string): string {
  if (basePath === '' || basePath === '/') return '/'
  return `/${basePath.replace(/^\/+|\/+$/g, '')}/`
}

async function waitForHealth(
  child: ChildProcess,
  healthURL: string,
  stderr: () => string,
): Promise<void> {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`Moneyflow E2E server exited before health check: ${stderr()}`)
    }
    try {
      const response = await fetch(healthURL)
      if (response.ok) return
    } catch {
      // The concrete loopback listener is still starting.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 50))
  }
  throw new Error(`Moneyflow E2E server did not become healthy: ${stderr()}`)
}

async function stopProcessGroup(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.pid === undefined) return
  try {
    process.kill(-child.pid, 'SIGTERM')
  } catch {
    child.kill('SIGTERM')
  }
  const exited = new Promise<void>((resolveExit) => child.once('exit', () => resolveExit()))
  const forced = new Promise<void>((resolveForce) =>
    setTimeout(() => {
      if (child.exitCode === null && child.pid !== undefined) {
        try {
          process.kill(-child.pid, 'SIGKILL')
        } catch {
          child.kill('SIGKILL')
        }
      }
      resolveForce()
    }, 5_000),
  )
  await Promise.race([exited, forced])
}

export async function startE2EServer(basePath = '/'): Promise<E2EServer> {
  const normalized = normalizedBasePath(basePath)
  const repository = resolve(process.cwd(), '..')
  const binaryDirectory = await mkdtemp(join(tmpdir(), 'moneyflow-e2e-'))
  const binary = join(binaryDirectory, process.platform === 'win32' ? 'moneyflow.exe' : 'moneyflow')
  const build = spawnSync('go', ['build', '-o', binary, './cmd/moneyflow'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) {
    await rm(binaryDirectory, { recursive: true, force: true })
    throw new Error(`Could not build Moneyflow E2E server: ${build.stderr}`)
  }
  let lastError: unknown
  try {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const port = await availablePort()
      const origin = `http://127.0.0.1:${port}`
      let stderr = ''
      const child = spawn(
        binary,
        [
          'web',
          '--demo',
          '--open=false',
          '--listen',
          `127.0.0.1:${port}`,
          '--base-path',
          normalized,
        ],
        { cwd: repository, detached: true, stdio: ['ignore', 'ignore', 'pipe'] },
      )
      child.stderr?.setEncoding('utf8')
      child.stderr?.on('data', (chunk: string) => (stderr += chunk))
      try {
        await waitForHealth(child, `${origin}${normalized}api/v1/health`, () => stderr)
        return {
          basePath: normalized,
          origin,
          url: `${origin}${normalized}`,
          stop: async () => {
            await stopProcessGroup(child)
            await rm(binaryDirectory, { recursive: true, force: true })
          },
        }
      } catch (error) {
        lastError = error
        await stopProcessGroup(child)
        if (!stderr.toLowerCase().includes('address already in use')) break
      }
    }
    throw lastError
  } catch (error) {
    await rm(binaryDirectory, { recursive: true, force: true })
    throw error
  }
}
