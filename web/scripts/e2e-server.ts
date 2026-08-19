import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { chmod, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

export interface E2EServer {
  basePath: string
  origin: string
  profileAPI: string
  profileID: string
  url: string
  stop(): Promise<void>
}

export interface E2EServerOptions {
  basePath?: string
  externalURL?: string
  fixturePath?: string
  profileHome?: string
  seedProfile?: boolean
}

export interface OnboardingE2EServer {
  basePath: string
  origin: string
  url: string
  expire(profileID: string): Promise<void>
  logs(): Promise<string>
  stop(): Promise<void>
}

export interface OnboardingE2EServerOptions {
  recoveryProfile?: boolean
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

async function waitForApplication(
  child: ChildProcess,
  applicationURL: string,
  stderr: () => string,
): Promise<string> {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`Moneyflow E2E server exited before health check: ${stderr()}`)
    }
    try {
      const response = await fetch(applicationURL, { headers: { Accept: 'text/html' } })
      if (response.ok && /\/p\/profile_[a-z2-7]{26}\/$/.test(new URL(response.url).pathname)) {
        return response.url
      }
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

export async function startE2EServer(
  requested: string | E2EServerOptions = '/',
): Promise<E2EServer> {
  const options = typeof requested === 'string' ? { basePath: requested } : requested
  const normalized = normalizedBasePath(options.basePath ?? '/')
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
  if (options.seedProfile) {
    if (!options.profileHome) {
      await rm(binaryDirectory, { recursive: true, force: true })
      throw new Error('A persistent E2E profile home is required before seeding.')
    }
    const seedBinary = join(
      binaryDirectory,
      process.platform === 'win32' ? 'seedprofile.exe' : 'seedprofile',
    )
    const seedBuild = spawnSync('go', ['build', '-o', seedBinary, './internal/tools/seedprofile'], {
      cwd: repository,
      encoding: 'utf8',
    })
    if (seedBuild.status !== 0) {
      await rm(binaryDirectory, { recursive: true, force: true })
      throw new Error(`Could not build the E2E profile seeder: ${seedBuild.stderr}`)
    }
    const seeded = spawnSync(seedBinary, [options.profileHome], {
      cwd: repository,
      encoding: 'utf8',
    })
    if (seeded.status !== 0) {
      await rm(binaryDirectory, { recursive: true, force: true })
      throw new Error(`Could not seed the E2E profile: ${seeded.stderr}`)
    }
  }
  let lastError: unknown
  try {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const port = await availablePort()
      const origin = `http://127.0.0.1:${port}`
      let stderr = ''
      const args = [
        'web',
        ...(options.fixturePath
          ? ['--fixture', resolve(repository, options.fixturePath)]
          : options.profileHome
            ? []
            : ['--demo']),
        ...(options.profileHome ? ['--profile', 'Moneyflow'] : []),
        '--open=false',
        '--listen',
        `127.0.0.1:${port}`,
        '--base-path',
        normalized,
        ...(options.externalURL ? ['--external-url', options.externalURL] : []),
      ]
      const child = spawn(binary, args, {
        cwd: repository,
        detached: true,
        env: options.profileHome
          ? { ...process.env, MONEYFLOW_HOME: options.profileHome }
          : process.env,
        stdio: ['ignore', 'ignore', 'pipe'],
      })
      child.stderr?.setEncoding('utf8')
      child.stderr?.on('data', (chunk: string) => (stderr += chunk))
      try {
        const url = await waitForApplication(child, `${origin}${normalized}`, () => stderr)
        const profileID = /\/p\/(profile_[a-z2-7]{26})\/$/.exec(new URL(url).pathname)?.[1]
        if (!profileID) throw new Error('Moneyflow E2E server returned an invalid profile route.')
        return {
          basePath: normalized,
          origin,
          profileAPI: `${origin}${normalized}api/v1/profiles/${profileID}/`,
          profileID,
          url,
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

export async function startOnboardingE2EServer(
  requestedBasePath = '/',
  options: OnboardingE2EServerOptions = {},
): Promise<OnboardingE2EServer> {
  const normalized = normalizedBasePath(requestedBasePath)
  const repository = resolve(process.cwd(), '..')
  const isolatedBase = join(repository, '.cache', 'web-e2e')
  await mkdir(isolatedBase, { recursive: true, mode: 0o700 })
  await chmod(isolatedBase, 0o700)
  const binaryDirectory = await mkdtemp(join(isolatedBase, 'moneyflow-onboarding-e2e-'))
  const binary = join(
    binaryDirectory,
    process.platform === 'win32' ? 'webtestserver.exe' : 'webtestserver',
  )
  const build = spawnSync('go', ['build', '-o', binary, './internal/tools/webtestserver'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) {
    await rm(binaryDirectory, { recursive: true, force: true })
    throw new Error(`Could not build Moneyflow onboarding E2E server: ${build.stderr}`)
  }
  let lastError: unknown
  try {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const port = await availablePort()
      const origin = `http://127.0.0.1:${port}`
      const profileHome = join(binaryDirectory, `profiles-${attempt}`)
      const rootToken = randomUUID()
      await mkdir(profileHome, { mode: 0o700 })
      await writeFile(join(profileHome, '.moneyflow-webtest-root'), rootToken, { mode: 0o600 })
      let stderr = ''
      const child = spawn(
        binary,
        [
          '--home',
          profileHome,
          '--root-token',
          rootToken,
          '--listen',
          `127.0.0.1:${port}`,
          '--base-path',
          normalized,
          ...(options.recoveryProfile ? ['--recovery-profile'] : []),
        ],
        { cwd: repository, detached: true, stdio: ['ignore', 'ignore', 'pipe'] },
      )
      child.stderr?.setEncoding('utf8')
      child.stderr?.on('data', (chunk: string) => (stderr += chunk))
      try {
        const url = `${origin}${normalized}`
        await waitForCatalog(child, url, () => stderr)
        return {
          basePath: normalized,
          origin,
          url,
          async expire(profileID) {
            const response = await fetch(
              `${origin}/__moneyflow_test/profiles/${encodeURIComponent(profileID)}/expire`,
              { method: 'POST' },
            )
            if (!response.ok) throw new Error('Could not expire the synthetic provider session.')
          },
          async logs() {
            return stderr
          },
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

async function waitForCatalog(
  child: ChildProcess,
  url: string,
  stderr: () => string,
): Promise<void> {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`Moneyflow onboarding E2E server exited before health check: ${stderr()}`)
    }
    try {
      const response = await fetch(url, { headers: { Accept: 'text/html' } })
      if (response.ok && new URL(response.url).pathname === new URL(url).pathname) return
    } catch {
      // The concrete loopback listener is still starting.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 50))
  }
  throw new Error(`Moneyflow onboarding E2E server did not become healthy: ${stderr()}`)
}
