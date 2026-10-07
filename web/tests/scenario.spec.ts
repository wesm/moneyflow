import { expect, test, type Page } from '@playwright/test'
import { readFile, readdir, rm, stat } from 'node:fs/promises'
import { join } from 'node:path'

import { withScenarioServer, type ScenarioServer } from '../scripts/scenario-server'

test.describe.configure({ retries: 0 })
test.use({ timezoneId: 'UTC' })

test('@smoke charts label monetary values in desktop and narrow layouts', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await page.goto(server.url)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    const chart = page.getByRole('complementary', { name: 'Visualizations' })
    await expect(chart.locator('svg text').filter({ hasText: '−82.00' })).toBeVisible()
    await expect(chart.locator('svg text').filter({ hasText: '−41.00' })).toBeVisible()
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(chart).not.toBeVisible()
    await expect(page.getByRole('switch', { name: 'Charts' })).not.toBeChecked()
    await page.getByRole('switch', { name: 'Charts' }).check()
    const drawer = page.getByRole('dialog', { name: 'Moneyflow visualizations' })
    await expect(drawer.locator('svg text').filter({ hasText: '−82.00' })).toBeVisible()
    expect(
      await page.locator('body').evaluate((body) => body.scrollWidth <= body.clientWidth),
    ).toBe(true)
    await page.keyboard.press('Escape')
    await page.setViewportSize({ width: 1440, height: 900 })
    await expect(chart).toBeVisible()
    await page.getByRole('grid', { name: 'Financial results' }).focus()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    for (const grouping of ['category', 'group', 'account', 'time']) {
      await page.keyboard.press('g')
      await expect(page.getByRole('navigation', { name: 'Active refinements' })).toContainText(
        `Group: ${grouping}`,
      )
    }
    await expect(chart.getByRole('region', { name: 'USD time chart', exact: true })).toBeVisible()
    await expect(chart.locator('svg text').filter({ hasText: /^0\.00$/ })).toBeVisible()
  })
})

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
  await page.keyboard.press('f')
  const filters = page.getByRole('dialog', { name: 'Filter transactions', exact: true })
  await filters.getByRole('button', { name: 'All time', exact: true }).click()
  const picker = page.getByRole('dialog', { name: 'Select date range' })
  await picker.getByRole('radio', { name: 'Custom', exact: true }).click()
  await picker.getByRole('button', { name: 'Previous month' }).click()
  await picker.getByRole('button', { name: 'Sep 1, 2026', exact: true }).click()
  await picker.getByRole('button', { name: 'Sep 30, 2026', exact: true }).click()
  await filters.locator('button[aria-haspopup="dialog"]').click()
  if (!showHidden)
    await filters.getByRole('checkbox', { name: 'Show hidden transactions' }).uncheck()
  await filters.getByRole('button', { name: 'Apply filters' }).click()
  await expect(filters).not.toBeVisible()
  await page
    .getByRole('row')
    .filter({ has: page.getByRole('gridcell', { name: 'Example Shop', exact: true }) })
    .click()
}

test('@smoke long active filters keep Clear reachable on a narrow empty view', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.keyboard.press('/')
    await page
      .getByRole('searchbox', { name: 'Search transactions' })
      .fill('NoSuchSyntheticMerchant')
    await page.keyboard.press('Enter')
    await expect(page.getByText('No transactions', { exact: true })).toBeVisible()
    const clear = page
      .getByRole('navigation', { name: 'Active refinements' })
      .getByRole('button', { name: /^Clear/ })
    await expect(clear).toBeInViewport()
    expect(
      await page.locator('body').evaluate((body) => body.scrollWidth <= body.clientWidth),
    ).toBe(true)
    await clear.click()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
    await expect(page.getByText('No transactions', { exact: true })).not.toBeVisible()
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
    await edit.getByLabel('Merchant name').fill('Cancelled Name')
    await edit.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    expect((await server.observe()).pending).toBe(0)
    await page.keyboard.press('h')
    await expect.poll(async () => (await server.observe()).pending).toBe(1)
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    expect((await server.observe()).rows).toEqual(before.rows)
    await page.keyboard.press('u')
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
    await page.keyboard.press('Shift+U')
    await expect.poll(async () => (await server.observe()).pending).toBe(1)
    await server.fault('reject')
    await page.keyboard.press('w')
    await page.getByRole('button', { name: 'Commit reviewed changes' }).click()
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write.getByRole('button', { name: 'Stop and reconcile' })).toBeVisible()
    expect((await server.observe()).snapshot_calls).toBe(1)
    await page.screenshot({ path: info.outputPath('narrow-write.png'), fullPage: true })
    await write.getByRole('button', { name: 'Stop and reconcile' }).click()
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
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
    await expect(edit.getByText('2 transactions affected', { exact: true })).toBeVisible()
    await edit.getByLabel('Merchant name').fill('Destination Shop')
    await page.keyboard.press('Enter')
    await expect(edit).not.toBeVisible()
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    expect((await server.observe()).rows).toEqual(before.rows)
    await server.fault('block')
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    await review.getByRole('button', { name: 'Commit reviewed changes' }).click()
    await expect
      .poll(
        async () =>
          (await server.observe()).calls.filter(
            (call) => call.method === 'update' && call.outcome === 'started',
          ).length,
      )
      .toBe(1)
    expect((await server.observe()).rows).toEqual(before.rows)
    await server.release()
    await expect.poll(async () => (await server.observe()).pending).toBe(0)
    const after = await server.observe()
    expect(after.rows['current-a']?.merchant).toBe('Destination Shop')
    expect(after.rows.older).toEqual(before.rows.older)
    expect(after.snapshot_calls).toBe(1)
    expect(after.audit).toContain('current-a')
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write.getByRole('status')).toHaveText('Provider write complete.')
    await expect(write.getByText('2 of 2 complete', { exact: true })).toBeVisible()
  })
})

test('@smoke a delayed row selection keeps focus in the category editor', async ({
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
      const filter = edit.getByRole('searchbox', { name: 'Filter categories' })
      await filter.fill('Heal')
      await expect(
        edit.getByRole('combobox', { name: 'Category: Health', exact: true }),
      ).toBeVisible()
      await expect(filter).toBeFocused()
      releaseSelection()
      await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
      await expect(filter).toBeFocused()
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
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await september(page, server)
    await page.keyboard.press('c')
    const edit = page.getByRole('dialog', { name: 'Change category' })
    await edit.getByRole('searchbox', { name: 'Filter categories' }).fill('Heal')
    await expect(
      edit.getByRole('combobox', { name: 'Category: Health', exact: true }),
    ).toBeVisible()
    await expect(edit.getByRole('searchbox', { name: 'Filter categories' })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(edit).not.toBeVisible()
    expect((await server.observe()).targets).toEqual(['current-a', 'current-b'])
    await server.fault('block-after')
    await page.keyboard.press('w')
    await page.getByRole('button', { name: 'Commit reviewed changes' }).click()
    await expect
      .poll(
        async () =>
          (await server.observe()).calls.filter(
            (call) => call.outcome === 'applied' && call.method === 'update',
          ).length,
      )
      .toBeGreaterThanOrEqual(1)
    await server.restart(true)
    await server.advance(180)
    await page.goto(server.url)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Write / })).toBeVisible()
    await page.keyboard.press('w')
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
