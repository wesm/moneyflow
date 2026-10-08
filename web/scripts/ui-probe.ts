import { chromium, expect, type Page } from '@playwright/test'
import { mkdir, mkdtemp, readFile, writeFile, appendFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { parseArgs } from 'node:util'

import { startScenarioServer, type ScenarioServer, type Observation } from './scenario-server'

type Role = Parameters<Page['getByRole']>[0]
export interface BrowserAction {
  kind:
    | 'key'
    | 'click'
    | 'fill'
    | 'wait'
    | 'focus'
    | 'close-date'
    | 'checkpoint'
    | 'resize'
    | 'theme'
  value?: string
  role?: Role
  name?: string
  width?: number
  height?: number
  pending?: number
  targets?: string[]
  changed?: Partial<Observation['rows'][string]>
}

export function browserCampaign(seed: number): BrowserAction[] {
  const actions: BrowserAction[] = [
    { kind: 'key', value: 'f' },
    { kind: 'click', role: 'button', name: 'All time' },
    { kind: 'click', role: 'radio', name: 'Custom' },
    { kind: 'click', role: 'button', name: 'Previous month' },
    { kind: 'click', role: 'button', name: 'Sep 1, 2026' },
    { kind: 'click', role: 'button', name: 'Sep 30, 2026' },
    { kind: 'close-date' },
    { kind: 'click', role: 'checkbox', name: 'Show hidden transactions' },
    { kind: 'click', role: 'button', name: 'Apply filters' },
    { kind: 'focus', role: 'grid', name: 'Financial results' },
  ]
  let changed: BrowserAction['changed']
  switch (seed % 3) {
    case 0:
      actions.push(
        { kind: 'key', value: 'm' },
        { kind: 'fill', role: 'combobox', name: 'Merchant name', value: 'Dest' },
        { kind: 'wait', role: 'option', name: 'Destination Shop' },
        { kind: 'key', value: 'Enter' },
      )
      changed = { merchant: 'Destination Shop' }
      break
    case 1:
      actions.push(
        { kind: 'key', value: 'c' },
        { kind: 'fill', role: 'searchbox', name: 'Filter categories', value: 'Heal' },
        { kind: 'wait', role: 'combobox', name: 'Category: Health' },
        { kind: 'key', value: 'Enter' },
      )
      changed = { category: 'Health' }
      break
    default:
      actions.push({ kind: 'key', value: 'h' })
      changed = { hidden: true }
  }
  return [
    ...actions,
    { kind: 'checkpoint', pending: 1, targets: ['current-a', 'current-b'] },
    { kind: 'key', value: 'w' },
    { kind: 'click', role: 'button', name: 'Commit reviewed changes' },
    { kind: 'checkpoint', pending: 0, targets: [], changed },
  ]
}

// A replay always opens a fresh synthetic account. Locator actions go through
// the rendered app; the observation endpoint cannot perform an application edit.
export async function driveBrowser(
  page: Page,
  server: ScenarioServer,
  actions: BrowserAction[],
  root: string,
): Promise<void> {
  if (actions.length > 256) throw new Error('Browser sequence exceeds 256 steps')
  await writeFile(
    join(root, 'actions.jsonl'),
    actions.map((action) => JSON.stringify(action)).join('\n') + '\n',
    { mode: 0o600 },
  )
  await page.clock.setFixedTime(new Date('2026-10-15T12:00:00Z'))
  await page.goto(server.url)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  const before = await server.observe()
  for (const [index, action] of actions.entries()) {
    try {
      switch (action.kind) {
        case 'key':
          await page.keyboard.press(action.value ?? '')
          break
        case 'click':
          await page.getByRole(action.role!, { name: action.name!, exact: true }).click()
          break
        case 'fill':
          await page
            .getByRole(action.role!, { name: action.name!, exact: true })
            .fill(action.value ?? '')
          break
        case 'wait':
          await expect(
            page.getByRole(action.role!, { name: action.name!, exact: true }),
          ).toBeVisible()
          break
        case 'focus':
          await expect(
            page.getByRole(action.role!, { name: action.name!, exact: true }),
          ).toBeFocused()
          break
        case 'close-date':
          await page
            .getByRole('dialog', { name: 'Filter transactions', exact: true })
            .locator('button[aria-haspopup="dialog"]')
            .click()
          break
        case 'resize':
          await page.setViewportSize({ width: action.width!, height: action.height! })
          break
        case 'theme':
          await page.emulateMedia({ colorScheme: action.value === 'dark' ? 'dark' : 'light' })
          break
        case 'checkpoint': {
          await expect.poll(async () => (await server.observe()).pending).toBe(action.pending)
          const observed = await server.observe()
          expect(observed.targets).toEqual(action.targets)
          expect(observed.snapshot_calls).toBe(1)
          for (const id of ['older', 'hidden', 'other-month', 'other-account', 'destination'])
            expect(observed.rows[id]).toEqual(before.rows[id])
          for (const id of ['current-a', 'current-b'])
            expect(observed.rows[id]).toEqual({ ...before.rows[id], ...action.changed })
          if (action.pending === 0 && action.changed)
            await expect(
              page.getByRole('dialog', { name: 'Monarch write status' }).getByRole('status'),
            ).toHaveText('Provider write complete.')
          await writeFile(
            join(root, `observation-${index}.json`),
            JSON.stringify(observed, null, 2),
            { mode: 0o600 },
          )
          await page.screenshot({ path: join(root, `screen-${index}.png`), fullPage: true })
          break
        }
        default:
          throw new Error('Unknown browser action')
      }
      await appendFile(
        join(root, 'steps.jsonl'),
        JSON.stringify({ index, action, result: 'ok' }) + '\n',
        { mode: 0o600 },
      )
    } catch (error) {
      await appendFile(
        join(root, 'steps.jsonl'),
        JSON.stringify({ index, action, error: String(error) }) + '\n',
        { mode: 0o600 },
      )
      await page.screenshot({ path: join(root, 'failure.png'), fullPage: true })
      throw new Error(`Browser action ${index} (${action.kind}): ${String(error)}`)
    }
  }
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      seed: { type: 'string', default: '1' },
      replay: { type: 'string' },
      artifacts: { type: 'string', default: '../.cache/ui-harness' },
      headed: { type: 'boolean', default: false },
    },
  })
  const seed = Number(values.seed)
  if (!Number.isSafeInteger(seed) || seed < 0) throw new Error('Seed must be a nonnegative integer')
  const parent = resolve(values.artifacts)
  await mkdir(parent, { recursive: true, mode: 0o700 })
  const root = await mkdtemp(join(parent, 'web-'))
  const actions: BrowserAction[] = values.replay
    ? (await readFile(values.replay, 'utf8'))
        .trim()
        .split('\n')
        .map((line) => JSON.parse(line) as BrowserAction)
    : browserCampaign(seed)
  console.log(
    `Artifacts: ${root}\nReplay: bun scripts/ui-probe.ts --replay ${JSON.stringify(join(root, 'actions.jsonl'))}`,
  )
  await writeFile(
    join(root, 'run.json'),
    JSON.stringify({
      layer: 'web',
      fixture: 'editing-v1',
      seed,
      timezone: 'UTC',
      logical_time: '2026-10-15T12:00:00Z',
      wall_time: new Date().toISOString(),
      actions: actions.length,
    }),
    { mode: 0o600 },
  )
  const server = await startScenarioServer(join(root, 'server'))
  let failed = true
  try {
    const browser = await chromium.launch({ headless: !values.headed })
    try {
      const context = await browser.newContext({
        viewport: { width: 1440, height: 900 },
        timezoneId: 'UTC',
      })
      const page = await context.newPage()
      page.setDefaultTimeout(5_000)
      await context.tracing.start({ screenshots: true, snapshots: true, sources: true })
      try {
        await driveBrowser(page, server, actions, root)
        failed = false
      } finally {
        await context.tracing.stop({ path: join(root, 'trace.zip') })
      }
    } finally {
      await browser.close()
    }
  } finally {
    await server.finish(failed)
  }
}

if (import.meta.main) await main()
