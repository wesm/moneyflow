import { expect, test, type Page } from '@playwright/test'

import { withScenarioServer, type ScenarioServer } from '../scripts/scenario-server'

test.describe.configure({ retries: 0 })
test.use({ timezoneId: 'UTC', locale: 'en-US' })

/**
 * The scenario's last refresh. Seven hours later a recent refresh is due, which reads the six
 * fixture rows in the last 90 days.
 */
const REFRESHED = String.raw`Oct 15, 2026(?:,| at)\s12:00\sPM`
const DUE_AT = new Date('2026-10-15T19:00:00Z')

/** Text naming the last refresh time, which WebKit writes as `Oct 15, 2026 at 12:00 PM`. */
function aged(lead: string, rest = '', whole = true): RegExp {
  const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const pattern = `${escape(lead)} \\(${REFRESHED}\\)${escape(rest)}`
  return new RegExp(whole ? `^${pattern}$` : pattern)
}

async function openDue(page: Page, server: ScenarioServer): Promise<void> {
  await server.advance(7 * 3600)
  await page.clock.setFixedTime(DUE_AT)
  await page.goto(server.url)
}

function refreshing(page: Page) {
  return page.getByRole('dialog', { name: 'Refreshing from Monarch' })
}

function banner(page: Page) {
  return page.locator('.mf-stale-banner')
}

async function expectBlockedProgress(page: Page): Promise<void> {
  const dialog = refreshing(page)
  await expect(dialog).toContainText('Recent data (last 90 days)')
  await expect(dialog.locator('.mf-refresh__count')).toHaveText('Fetched 3 of 6')
  await expect(dialog.getByRole('progressbar', { name: 'Refresh progress' })).toHaveAttribute(
    'max',
    '6',
  )
  await expect(dialog).toContainText(aged('Last updated 7 hours ago', '', false))
  await expect(page.locator('.app-shell')).toHaveAttribute('inert', '')
}

async function failRefresh(page: Page, server: ScenarioServer) {
  await server.refreshFault('fail')
  await openDue(page, server)
  const failed = page.getByRole('dialog', { name: 'Refresh from Monarch failed' })
  await expect(failed.getByRole('alert')).not.toBeEmpty()
  await expect(failed.getByRole('button', { name: 'Retry' })).toBeFocused()
  return failed
}

test('@smoke a due refresh opens the dialog with progress and closes when it succeeds', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await server.refreshFault('block')
    await openDue(page, server)
    await expectBlockedProgress(page)

    await server.release()

    await expect(refreshing(page)).toBeHidden()
    await expect(
      page
        .locator('.kit-flash-banner')
        .filter({ hasText: /Refresh complete: 6 imported, 0 removed\./ }),
    ).toBeVisible()
    await expect(banner(page)).toHaveCount(0)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    expect((await server.observe()).snapshot_calls).toBe(2)
  })
})

test('a failed refresh keeps the dialog open until Retry succeeds', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    const failed = await failRefresh(page, server)

    await failed.getByRole('button', { name: 'Retry' }).click()

    await expect(failed).toBeHidden()
    await expect(refreshing(page)).toBeHidden()
    await expect(
      page
        .locator('.kit-flash-banner')
        .filter({ hasText: /Refresh complete: 7 imported, 0 removed\./ }),
    ).toBeVisible()
    await expect(banner(page)).toHaveCount(0)
    expect((await server.observe()).snapshot_calls).toBe(3)
  })
})

test('Continue after a failure shows the stale banner until refresh is allowed', async ({
  page,
}, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    const failed = await failRefresh(page, server)
    const reason = await failed.getByRole('alert').textContent()

    await failed.getByRole('button', { name: 'Continue with data from Oct 15, 2026' }).click()

    await expect(failed).toBeHidden()
    const notice = banner(page)
    await expect(notice.getByRole('status')).toHaveText(
      aged('Showing data from 7 hours ago', `. ${reason}`),
    )
    await expect(notice.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    await expect(notice).toContainText('Automatic retry at 7:05 PM')
  })
})

test('Cancel stops the server refresh and shows the stale banner', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    const before = await server.observe()
    await server.refreshFault('block')
    await openDue(page, server)
    await expectBlockedProgress(page)

    await page.keyboard.press('Escape')

    await expect(refreshing(page)).toBeHidden()
    await expect(banner(page).getByRole('status')).toHaveText(
      aged('Showing data from 7 hours ago', '. Refresh was canceled; no provider data changed.'),
    )
    await expect(page.locator('.app-shell')).not.toHaveAttribute('inert')
    expect((await server.observe()).rows).toEqual(before.rows)
  })
})

test('removals are confirmed inside the refresh dialog', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    const before = await server.observe()
    await server.refreshFault('remove')
    await openDue(page, server)
    const confirm = page.getByRole('dialog', { name: 'Confirm provider refresh' })
    await expect(confirm).toContainText('This refresh would remove 5 posted transactions.')
    await expect(page.locator('.app-shell')).toHaveAttribute('inert', '')

    await page.keyboard.press('Escape')

    await expect(confirm).toBeHidden()
    await expect(banner(page).getByRole('status')).toContainText(
      'Refresh was not confirmed; no provider data changed.',
    )
    expect((await server.observe()).rows).toEqual(before.rows)
  })
})

test('another interface refresh is followed without a Cancel button', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    await server.otherRefresh('tui', true)
    await page.clock.setFixedTime(new Date('2026-10-15T12:00:00Z'))
    await page.goto(server.url)
    const dialog = refreshing(page)
    await expect(dialog).toContainText(
      'The terminal app (moneyflow tui) is refreshing this profile. Cancel it there.',
    )
    await expect(dialog.getByRole('button', { name: 'Cancel refresh' })).toHaveCount(0)

    await server.otherRefresh('tui', false)

    await expect(dialog).toBeHidden()
    await expect(banner(page)).toHaveCount(0)
  })
})

test('reloading during a refresh stops it and shows the stale banner', async ({ page }, info) => {
  await withScenarioServer(info.outputPath('scenario'), async (server) => {
    const before = await server.observe()
    await server.refreshFault('block')
    await openDue(page, server)
    await expectBlockedProgress(page)

    await page.reload()

    await expect(banner(page).getByRole('status')).toHaveText(
      aged('Showing data from 7 hours ago', '. Last refresh failed.', false),
    )
    await expect(refreshing(page)).toBeHidden()
    expect((await server.observe()).rows).toEqual(before.rows)
  })
})
