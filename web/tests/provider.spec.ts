import { expect, test, type Page, type Route } from '@playwright/test'

import { openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('an unbound provider profile explains why refresh is unavailable', async ({ page }) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    const asMonarch = async (route: Route) => {
      const response = await route.fetch()
      const body = (await response.json()) as Record<string, unknown>
      await route.fulfill({ response, json: { ...body, profile_kind: 'monarch' } })
    }
    await page.route('**/api/v1/profiles/*/view', asMonarch)
    await page.route('**/api/v1/profiles/*/view/transition', asMonarch)
    await openMoneyflow(page, server)

    const refresh = page.getByRole('button', { name: 'Refresh provider data' })
    await expect(refresh).toBeDisabled()
    await expect(refresh).toHaveAttribute('title', 'Connect a provider before refreshing.')
    await expect(page.getByText('Connect a provider before refreshing.')).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('r refreshes through the protected base-path API without leaking provider state to history', async ({
  page,
}) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    const fixture = await installProviderRoutes(page, false)
    await openMoneyflow(page, server)
    // Idle status keeps the last refresh's progress counts; the sidebar shows the update time.
    await expect(page.getByText('Updated just now')).toBeVisible()
    await expect(page.getByText('Refreshing provider data: 20 of 40')).toHaveCount(0)

    await page.keyboard.press('r')

    await expect(
      page
        .locator('.kit-flash-banner')
        .filter({ hasText: 'Refresh complete: 40 imported, 0 removed.' }),
    ).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Refreshing from Monarch' })).toHaveCount(0)
    expect(fixture.refreshBodies).toHaveLength(1)
    expect(fixture.refreshBodies[0]).toMatchObject({ manual: true, query: 'cur=USD%3A2&v=1' })
    const historyText = await page.evaluate(() => JSON.stringify(history.state))
    expect(historyText).not.toContain('opaque-confirmation')
    expect(historyText).not.toContain('imported_transactions')
    expect(page.url()).toBe(`${server.url}?cur=USD%3A2&v=1`)
  } finally {
    await server.stop()
  }
})

test('a refresh the server cannot complete shows a visible error', async ({ page }) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    await installProviderRoutes(page, false)
    await page.route('**/api/v1/profiles/*/provider/refresh', async (route) => {
      await route.fulfill({
        status: 500,
        contentType: 'application/problem+json',
        json: {
          type: 'about:blank',
          title: 'Internal Server Error',
          status: 500,
          detail: 'The request could not be completed.',
          code: 'internal_error',
        },
      })
    })
    await openMoneyflow(page, server)

    await page.keyboard.press('r')

    const dialog = page.getByRole('dialog', { name: 'Refresh from Monarch failed' })
    await expect(dialog.getByRole('alert')).toHaveText(
      'The request could not be completed. The terminal running moneyflow web shows the cause.',
    )
    await expect(dialog.getByRole('button', { name: 'Retry' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

test('keyboard-only deletion confirmation applies only after explicit Enter', async ({ page }) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    const fixture = await installProviderRoutes(page, true)
    await openMoneyflow(page, server)

    await page.keyboard.press('r')
    const dialog = page.getByRole('dialog', { name: 'Confirm provider refresh' })
    await expect(dialog).toContainText('5 posted transactions')
    expect(fixture.confirmBodies).toHaveLength(0)

    await page.keyboard.press('Enter')

    await expect(dialog).toBeHidden()
    // The confirmed refresh reads the status before it sends the confirmation.
    await expect.poll(() => fixture.confirmBodies.length).toBe(1)
    expect(fixture.confirmBodies[0]).toMatchObject({
      manual: true,
      confirmation_token: 'opaque-confirmation',
    })
  } finally {
    await server.stop()
  }
})

test('Escape cancels deletion confirmation and a later refresh fetches a new candidate', async ({
  page,
}) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    const fixture = await installProviderRoutes(page, true)
    await openMoneyflow(page, server)

    await page.keyboard.press('r')
    const dialog = page.getByRole('dialog', { name: 'Confirm provider refresh' })
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    expect(fixture.confirmBodies).toHaveLength(0)
    await expect(page.locator('.mf-stale-banner')).toContainText(
      'Refresh was not confirmed; no provider data changed.',
    )

    await page.keyboard.press('r')
    await expect(dialog).toBeVisible()
    expect(fixture.refreshBodies).toHaveLength(2)
    expect(fixture.confirmBodies).toHaveLength(0)
    expect(page.url()).toBe(`${server.url}?cur=USD%3A2&v=1`)
  } finally {
    await server.stop()
  }
})

test('a refresh confirmation started from the phone drawer closes the drawer', async ({ page }) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    await installProviderRoutes(page, true)
    await page.setViewportSize({ width: 390, height: 844 })
    await openMoneyflow(page, server)
    await page.getByRole('button', { name: 'Open navigation' }).click()
    const drawer = page.getByRole('dialog', { name: 'Navigation' })
    await drawer.getByRole('button', { name: 'Refresh provider data' }).click()

    const dialog = page.getByRole('dialog', { name: 'Confirm provider refresh' })
    await expect(dialog).toBeVisible()
    await expect(drawer).toBeHidden()
    expect(await dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
  } finally {
    await server.stop()
  }
})

async function installProviderRoutes(page: Page, requireConfirmation: boolean) {
  let projection: Record<string, unknown> | undefined
  const refreshBodies: Record<string, unknown>[] = []
  const confirmBodies: Record<string, unknown>[] = []
  const status = () => ({
    version: '1',
    revision: String(projection?.revision ?? '0'),
    generation: '1',
    provider_kind: 'monarch',
    last_success: new Date().toISOString(),
    refresh_due: 'none',
    refreshing: false,
    progress: { fetched: 20, total: 40 },
    summary: summary(0),
    capability: refreshCapability(),
  })

  const patchProjection = async (route: Route) => {
    const response = await route.fetch()
    if (!response.ok()) {
      await route.fulfill({ response })
      return
    }
    const body = (await response.json()) as Record<string, unknown>
    const capabilities = body.capabilities as Array<Record<string, unknown>>
    body.capabilities = capabilities.map((capability) =>
      capability.id === 'provider.refresh' ? refreshCapability() : capability,
    )
    // The demo profile is local, which has no provider controls; present it as a provider profile.
    body.profile_kind = 'monarch'
    projection = body
    await route.fulfill({ response, json: body })
  }
  await page.route('**/api/v1/profiles/*/view', patchProjection)
  await page.route('**/api/v1/profiles/*/view/transition', patchProjection)
  await page.route('**/api/v1/profiles/*/provider/status', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', json: status() })
  })
  await page.route('**/api/v1/profiles/*/provider/refresh', async (route) => {
    refreshBodies.push((await route.request().postDataJSON()) as Record<string, unknown>)
    if (requireConfirmation) {
      const provider = {
        ...status(),
        code: 'provider_deletion_confirmation_required',
        confirmation_token: 'opaque-confirmation',
        summary: summary(5),
      }
      await route.fulfill({
        status: 409,
        contentType: 'application/problem+json',
        json: {
          type: 'about:blank',
          title: 'Conflict',
          status: 409,
          detail: 'Confirm the proposed provider removals.',
          code: 'provider_deletion_confirmation_required',
          provider,
        },
      })
      return
    }
    await fulfillRefresh(route, projection, status())
  })
  await page.route('**/api/v1/profiles/*/provider/refresh/confirm', async (route) => {
    confirmBodies.push((await route.request().postDataJSON()) as Record<string, unknown>)
    await fulfillRefresh(route, projection, status())
  })
  return { refreshBodies, confirmBodies }
}

async function fulfillRefresh(
  route: Route,
  projection: Record<string, unknown> | undefined,
  status: Record<string, unknown>,
): Promise<void> {
  if (!projection) throw new Error('The provider fixture has no current view projection.')
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    json: {
      version: '1',
      revision: projection.revision,
      generation: '2',
      status,
      projection,
      selection: { kind: 'preserved', value: projection.selection },
    },
  })
}

function refreshCapability() {
  return {
    id: 'provider.refresh',
    key_display: 'r',
    description: 'Refresh provider data',
    category: 'System',
    available: true,
  }
}

function summary(removedTransactions: number) {
  return {
    imported_accounts: 0,
    imported_merchants: 0,
    imported_groups: 0,
    imported_categories: 0,
    imported_transactions: 40,
    removed_transactions: removedTransactions,
    removed_operations: 0,
    removed_targets: 0,
    retained_operations: 0,
    rebased_hide_targets: 0,
    discarded_redo_operations: 0,
  }
}
