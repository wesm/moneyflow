import { existsSync } from 'node:fs'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'

const workingDirectory = resolve(process.cwd())
const webRoot = existsSync(join(workingDirectory, 'web', 'package.json'))
  ? join(workingDirectory, 'web')
  : workingDirectory
const repositoryRoot = dirname(webRoot)
const committedOpenAPI = join(repositoryRoot, 'api', 'openapi.yaml')
const committedSchema = join(webRoot, 'src', 'lib', 'api', 'schema.d.ts')
const prettierConfig = join(webRoot, '.prettierrc.json')

export function canonicalNewline(value: Uint8Array | string): Uint8Array {
  const text = typeof value === 'string' ? value : new TextDecoder().decode(value)
  return new TextEncoder().encode(`${text.replace(/\r\n?/g, '\n').replace(/\n*$/, '')}\n`)
}

export async function assertArtifactCurrent(
  path: string,
  generated: Uint8Array,
  label: string,
): Promise<void> {
  let current: Uint8Array
  try {
    current = await readFile(path)
  } catch {
    throw new Error(`${label} is missing; run \`bun run generate\``)
  }
  if (!Buffer.from(canonicalNewline(current)).equals(Buffer.from(canonicalNewline(generated)))) {
    throw new Error(`${label} is stale; run \`bun run generate\``)
  }
}

async function run(command: string[], cwd: string): Promise<Uint8Array> {
  const executable = command[0]
  if (executable === undefined) throw new Error('empty generator command')
  const result = spawnSync(executable, command.slice(1), { cwd, encoding: null })
  if (result.status !== 0) {
    if (result.stderr) process.stderr.write(result.stderr)
    throw new Error(`${executable} exited with status ${result.status ?? 'unknown'}`)
  }
  return result.stdout ?? new Uint8Array()
}

export async function generateAPI(check: boolean): Promise<void> {
  const temporaryDirectory = await mkdtemp(join(tmpdir(), 'moneyflow-web-api-'))
  try {
    const openAPI = canonicalNewline(
      await run(['go', 'run', './cmd/moneyflow', 'openapi', '--format', 'yaml'], repositoryRoot),
    )
    const temporaryOpenAPI = join(temporaryDirectory, 'openapi.yaml')
    const temporarySchema = join(temporaryDirectory, 'schema.d.ts')
    await writeFile(temporaryOpenAPI, openAPI)
    await run(
      [process.execPath, 'x', 'openapi-typescript', temporaryOpenAPI, '--output', temporarySchema],
      webRoot,
    )
    await run(
      [process.execPath, 'x', 'prettier', '--config', prettierConfig, '--write', temporarySchema],
      webRoot,
    )
    const schema = canonicalNewline(await readFile(temporarySchema))

    if (check) {
      await assertArtifactCurrent(committedOpenAPI, openAPI, 'canonical OpenAPI document')
      await assertArtifactCurrent(committedSchema, schema, 'generated browser API schema')
      return
    }
    await Promise.all([
      mkdir(dirname(committedOpenAPI), { recursive: true }),
      mkdir(dirname(committedSchema), { recursive: true }),
    ])
    await Promise.all([writeFile(committedOpenAPI, openAPI), writeFile(committedSchema, schema)])
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true })
  }
}

if (import.meta.main) {
  generateAPI(process.argv.includes('--check')).catch((error: unknown) => {
    const message = error instanceof Error ? error.message : 'API generation failed'
    console.error(message)
    process.exit(1)
  })
}
