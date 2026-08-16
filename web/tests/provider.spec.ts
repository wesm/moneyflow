import { expect, test, type Page, type Route } from '@playwright/test'

import { openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('an unbound profile explains why provider refresh is unavailable', async ({ page }) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    await openMoneyflow(page, server)

    const refresh = page.getByRole('button', { name: 'Refresh provider data' })
    await expect(refresh).toBeDisabled()
    await expect(refresh).toHaveAttribute('title', 'Connect a provider before refreshing.')
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
    await expect(page.getByText('Refreshing provider data: 20 of 40')).toBeVisible()

    await page.keyboard.press('r')

    await expect(page.getByText('Provider refresh complete.')).toBeAttached()
    expect(fixture.refreshBodies).toHaveLength(1)
    expect(fixture.refreshBodies[0]).toMatchObject({ manual: true, query: 'v=1' })
    const historyText = await page.evaluate(() => JSON.stringify(history.state))
    expect(historyText).not.toContain('opaque-confirmation')
    expect(historyText).not.toContain('imported_transactions')
    expect(page.url()).toBe(`${server.url}?v=1`)
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
    expect(fixture.confirmBodies).toHaveLength(1)
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

    await page.keyboard.press('r')
    await expect(dialog).toBeVisible()
    expect(fixture.refreshBodies).toHaveLength(2)
    expect(fixture.confirmBodies).toHaveLength(0)
    expect(page.url()).toBe(`${server.url}?v=1`)
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
    last_success: new Date().toISOString(),
    progress: { fetched: 20, total: 40 },
    summary: summary(0),
    capability: refreshCapability(),
  })

  await page.route('**/api/v1/view', async (route) => {
    const response = await route.fetch()
    const body = (await response.json()) as Record<string, unknown>
    const capabilities = body.capabilities as Array<Record<string, unknown>>
    body.capabilities = capabilities.map((capability) =>
      capability.id === 'provider.refresh' ? refreshCapability() : capability,
    )
    projection = body
    await route.fulfill({ response, json: body })
  })
  await page.route('**/api/v1/provider/status', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', json: status() })
  })
  await page.route('**/api/v1/provider/refresh', async (route) => {
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
  await page.route('**/api/v1/provider/refresh/confirm', async (route) => {
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
