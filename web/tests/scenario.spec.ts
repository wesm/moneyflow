import { expect, test, type Page } from '@playwright/test'
import { readFile, readdir, rm, stat } from 'node:fs/promises'
import { join } from 'node:path'

import { cell } from './fixtures'
import { withScenarioServer, type ScenarioServer } from '../scripts/scenario-server'

test.describe.configure({ retries: 0 })
test.use({ timezoneId: 'UTC' })

test('@smoke scenario failures retain evidence and successful runs remove it', async ({
  browserName,
}, info) => {
  const failedRoot = info.outputPath(`${browserName}-failed-scenario`)
  const successfulRoot = info.outputPath('successful-scenario')
  await expect(
    withScenarioServer(failedRoot, async (server) => {
      expect((await server.observe()).snapshot_calls).toBe(1)
      throw new Error('intentional checkpoint failure')
    }),
  ).rejects.toThrow('intentional checkpoint failure')
  expect(
    JSON.parse(await readFile(join(failedRoot, 'home/provider/provider-state.json'), 'utf8'))
      .transactions['current-a'],
  ).toBeDefined()
  expect(await readdir(join(failedRoot, 'home/profiles'))).toHaveLength(1)
  expect((await stat(join(failedRoot, 'server.log'))).isFile()).toBe(true)
  await withScenarioServer(successfulRoot, async (server) => {
    expect((await server.observe()).pending).toBe(0)
  })
  await expect(stat(successfulRoot)).rejects.toMatchObject({ code: 'ENOENT' })
  // Remove the intentional failure only after all evidence assertions pass.
  await rm(failedRoot, { recursive: true })
})

async function september(page: Page, server: ScenarioServer, showHidden = false): Promise<void> {
  await page.clock.setFixedTime(new Date('2026-10-15T12:00:00Z'))
  await page.goto(server.url)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  // Latest is in September, so the Month segment selects September.
  await page
    .getByRole('radiogroup', { name: 'Period', exact: true })
    .getByRole('radio', { name: 'Month' })
    .click()
  await expect(page.getByRole('button', { name: 'Sep 2026', exact: true })).toBeVisible()
  if (!showHidden) {
    await page.keyboard.press('f')
    await page.getByRole('button', { name: 'Show hidden transactions', exact: true }).click()
    await expect(page).toHaveURL(/[?&]hidden=0/)
    await page.keyboard.press('Escape')
  }
  const shop = page
    .getByRole('row')
    .filter({ has: page.getByRole('gridcell', { name: 'Example Shop', exact: true }) })
  await cell(shop, 'select').click()
}

test('@smoke a narrow empty search view keeps Clear search reachable', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server)
    await page.setViewportSize({ width: 390, height: 844 })
    // WebKit reports the phone media query after the resize resolves. Typing before it would
    // go to the desktop field, which the phone layout then replaces mid-search.
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
    await page.keyboard.press('/')
    await page
      .getByRole('searchbox', { name: 'Search transactions' })
      .fill('NoSuchSyntheticMerchant')
    await page.keyboard.press('Enter')
    await expect(page.getByText('No transactions', { exact: true })).toBeVisible()
    const clear = page.getByRole('button', { name: 'Clear search' })
    await expect(clear).toBeInViewport()
    expect(
      await page.locator('body').evaluate((body) => body.scrollWidth <= body.clientWidth),
    ).toBe(true)
    const length = await page.evaluate(() => history.length)
    await clear.click()
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toBeVisible()
    await expect(page.getByText('No transactions', { exact: true })).not.toBeVisible()
    // Leaving the search field commits the cleared search. A tap on a phone row would drill.
    await grid.focus()
    await expect(page).not.toHaveURL(/[?&]q=/)
    await expect.poll(() => page.evaluate(() => history.length)).toBe(length + 1)
    await page.goBack()
    await expect(page).toHaveURL(/[?&]q=NoSuchSyntheticMerchant(?:&|$)/)
  })
})

test('@smoke mixed hiding, undo, rejection and cancellation remain usable at narrow width', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server, true)
    const before = await server.observe()
    await page.setViewportSize({ width: 390, height: 844 })
    await page.emulateMedia({ colorScheme: 'dark' })
    await page.keyboard.press('m')
    const edit = page.getByRole('dialog', { name: 'Edit merchant', exact: true })
    await expect(edit).toBeVisible()
    await edit.getByRole('combobox', { name: 'Merchant' }).fill('Cancelled Name')
    await edit.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    expect((await server.observe()).pending).toBe(0)
    // Each key waits for the browser to show the edit before it, not only for the server to
    // record it, so the scenario does not depend on how keys pressed meanwhile are handled.
    const toast = (start: RegExp) => page.getByRole('status').filter({ hasText: start })
    await page.keyboard.press('h')
    await expect(toast(/^Hidden /)).toBeVisible()
    expect((await server.observe()).pending).toBe(1)
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    expect((await server.observe()).rows).toEqual(before.rows)
    await page.keyboard.press('u')
    await expect(toast(/^Undid /)).toBeVisible()
    expect((await server.observe()).pending).toBe(0)
    await page.keyboard.press('Shift+U')
    await expect(toast(/^Redid /)).toBeVisible()
    expect((await server.observe()).pending).toBe(1)
    await server.fault('reject')
    await page.keyboard.press('w')
    await page.getByRole('button', { name: /^Commit reviewed changes(?: Enter)?$/ }).click()
    // The rejected write opens its status in place of review; nothing is stacked beneath it.
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write.getByRole('button', { name: 'Stop and reconcile' })).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(1)
    expect(await write.evaluate((node) => node.contains(document.activeElement))).toBe(true)
    expect((await server.observe()).snapshot_calls).toBe(1)
    await page.screenshot({ path: info.outputPath('narrow-write.png'), fullPage: true })
    await write.getByRole('button', { name: 'Stop and reconcile' }).click()
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
    await expect(write.getByRole('status')).toHaveText(
      'Provider write stopped. Remaining edits discarded.',
    )
    await expect(write.getByText('Progress', { exact: true })).toHaveCount(0)
    expect((await server.observe()).rows.older).toEqual(before.rows.older)
    // Explicit reconciliation reloads provider truth; the rejected write itself
    // must not trigger that download before the user chooses this escape hatch.
    expect((await server.observe()).snapshot_calls).toBe(2)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    expect(
      await page.locator('body').evaluate((body) => body.scrollWidth <= body.clientWidth),
    ).toBe(true)
  })
})

test('@smoke filtered merchant editing preserves committed data until the provider finishes', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server)
    const before = await server.observe()
    await page.keyboard.press('m')
    const edit = page.getByRole('dialog', { name: 'Edit merchant', exact: true })
    await expect(edit.getByRole('button', { name: 'Stage change to 2 transactions' })).toBeVisible()
    await edit.getByRole('combobox', { name: 'Merchant' }).fill('Destination Shop')
    await page.keyboard.press('Enter')
    await page.keyboard.press('Enter')
    await expect(edit).not.toBeVisible()
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    expect((await server.observe()).rows).toEqual(before.rows)
    await server.fault('block')
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    await review.getByRole('button', { name: /^Commit reviewed changes(?: Enter)?$/ }).click()
    await expect
      .poll(
        async () =>
          (await server.observe()).calls.filter(
            (call) => call.method === 'update' && call.outcome === 'started',
          ).length,
      )
      .toBe(1)
    expect((await server.observe()).rows).toEqual(before.rows)
    // The running write shows its progress in the status bar, not in a drawer.
    await expect(page.locator('.mf-status-line')).toContainText(/Writing to Monarch: \d of 2…/)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await server.release()
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
    const after = await server.observe()
    expect(after.rows['current-a']?.merchant).toBe('Destination Shop')
    expect(after.rows.older).toEqual(before.rows.older)
    expect(after.snapshot_calls).toBe(1)
    expect(after.audit).toContain('current-a')
    await expect(page.getByRole('status', { name: 'Notice' })).toHaveText(
      'Saved 2 changes to Monarch.',
    )
    await expect(page.getByRole('dialog')).toHaveCount(0)

    // With nothing left to review, w and the sidebar reopen the finished write's status.
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await page.keyboard.press('w')
    await expect(write).toBeVisible()
    await expect(write.getByRole('status')).toHaveText('Provider write complete.')
    await expect(write.getByText('2 of 2 complete')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(write).toBeHidden()
    // At this width the sidebar is a rail; its pending icon opens the last write.
    await page.getByRole('button', { name: 'Review pending changes' }).click()
    await expect(write).toBeVisible()
  })
})

test('@smoke c waits for a delayed row selection, then edits it in a focused editor', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    let releaseSelection!: () => void
    const selectionReleased = new Promise<void>((resolve) => (releaseSelection = resolve))
    await page.route('**/view/transition', async (route) => {
      if (route.request().postDataJSON().action !== 'selection.toggle') {
        await route.continue()
        return
      }
      const response = await route.fetch()
      await selectionReleased
      await route.fulfill({ response })
    })
    try {
      await september(page, server)
      await page.keyboard.press('c')
      const edit = page.getByRole('dialog', { name: 'Change category' })
      // The editor waits for the selection, so it cannot edit the view before the selection.
      await expect(edit).toHaveCount(0)
      releaseSelection()
      await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
      const field = edit.getByRole('combobox', { name: 'Category' })
      await expect(field).toBeFocused()
      await field.fill('Heal')
      await expect(edit.getByRole('option').first()).toHaveAccessibleName(/^Health, /)
      await page.keyboard.press('Enter')
      await expect(edit).not.toBeVisible()
      expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    } finally {
      releaseSelection()
      await page.unrouteAll({ behavior: 'wait' })
    }
  })
})

test('@smoke applied but unanswered category edit survives a process restart', async ({
  page,
}, info) => {
  // The full sidebar's pending card holds the Write button.
  await page.setViewportSize({ width: 1440, height: 900 })
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server)
    await page.keyboard.press('c')
    const edit = page.getByRole('dialog', { name: 'Change category' })
    const field = edit.getByRole('combobox', { name: 'Category' })
    await field.fill('Heal')
    await expect(edit.getByRole('option').first()).toHaveAccessibleName(/^Health, /)
    await page.keyboard.press('Enter')
    await expect(edit).not.toBeVisible()
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    await server.fault('block-after')
    await page.keyboard.press('w')
    await page.getByRole('button', { name: /^Commit reviewed changes(?: Enter)?$/ }).click()
    await expect
      .poll(
        async () =>
          (await server.observe()).calls.filter(
            (call) => call.outcome === 'applied' && call.method === 'update',
          ).length,
      )
      .toBeGreaterThanOrEqual(1)
    await server.restart(true)
    // Only the next page load should recover the write after its lease expires.
    await page.goto('about:blank')
    await server.advance(180)
    await page.goto(server.url)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Write / })).toBeVisible()
    // A write that needs the user opens its status on load.
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write).toBeVisible()
    await write.getByRole('button', { name: 'Check and resume', exact: true }).click()
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
    const after = await server.observe()
    expect(after.rows['current-a']?.category).toBe('Health')
    expect(after.rows['current-b']?.category).toBe('Health')
    expect(after.rows.older?.category).toBe('Home')
    expect(after.snapshot_calls).toBe(1)
    expect(after.calls.filter((call) => call.method === 'update')).toHaveLength(2)
  })
})
