import { expect, test } from '@playwright/test'

import { startE2EServer } from '../scripts/e2e-server'

test('direct listener reads but canonical-origin policy rejects its mutations', async ({
  page,
}) => {
  const server = await startE2EServer({
    basePath: '/moneyflow/',
    externalURL: 'https://moneyflow.example/moneyflow/',
  })
  try {
    await page.goto(server.url)
    await expect(page.getByRole('alert')).toContainText('This listener is read-only')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    const token = await page.locator('meta[name=moneyflow-mutation-token]').getAttribute('content')
    expect(token).toBeTruthy()
    expect(page.url()).not.toContain(token!)

    await page.keyboard.press('h')
    await expect(page.getByText('Open the canonical Moneyflow URL to make changes.')).toBeVisible()
    await expect(page.getByText(/0 pending/)).toBeVisible()

    const rejected = await page.request.post(`${server.url}api/v1/mutations`, {
      data: {},
      headers: {
        Origin: 'https://untrusted.example',
        'Sec-Fetch-Site': 'cross-site',
        'X-Moneyflow-Mutation-Token': token!,
      },
    })
    expect(rejected.status()).toBe(403)
    expect(await rejected.text()).not.toContain(token!)
  } finally {
    await server.stop()
  }
})
