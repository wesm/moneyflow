import { expect, test } from '@playwright/test'

import { startE2EServer } from '../scripts/e2e-server'

test('@smoke root and nested deployments keep every request in their configured base path', async ({
  page,
}) => {
  const server = await startE2EServer('/moneyflow/')
  try {
    const requests: string[] = []
    page.on('request', (request) => requests.push(request.url()))
    await page.goto(server.url)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await page.keyboard.press('g')
    await expect(page).toHaveURL(/\/moneyflow\/\?group=category&v=1$/)

    const health = await page.request.get(`${server.origin}/moneyflow/api/v1/health`)
    expect(health.ok()).toBe(true)
    const openAPI = await page.request.get(`${server.origin}/moneyflow/openapi.json`)
    expect(openAPI.ok()).toBe(true)
    const unrelatedRoot = await page.request.get(`${server.origin}/`)
    expect(unrelatedRoot.status()).toBe(404)

    const sameOriginRequests = requests.filter((url) => url.startsWith(server.origin))
    expect(sameOriginRequests.length).toBeGreaterThan(1)
    expect(sameOriginRequests.every((url) => new URL(url).pathname.startsWith('/moneyflow/'))).toBe(
      true,
    )
  } finally {
    await server.stop()
  }
})
