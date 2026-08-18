import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

const secrets = [
  'private@example.test',
  'synthetic-provider-password',
  'JBSWY3DPEHPK3PXP',
  'synthetic-account-password',
]

test('onboarding responses, problems, DOM, console, and logs remain credential blind', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer()
  const responses: string[] = []
  const responseTasks: Promise<void>[] = []
  const consoleMessages: string[] = []
  page.on('console', (message) => consoleMessages.push(message.text()))
  page.on('response', (response) => {
    if (!response.url().includes('/onboarding/')) return
    responseTasks.push(
      response
        .text()
        .then((body) => responses.push(body))
        .then(() => undefined)
        .catch(() => undefined),
    )
  })
  try {
    await page.goto(server.url)
    await page.keyboard.press('a')
    await page.keyboard.press('m')
    await page.getByLabel('Profile name').fill('Private Profile')
    await page.getByLabel('Profile name').press('Enter')
    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
    await page.getByRole('button', { name: 'Continue with USD / 2' }).click()
    await expect(page.getByRole('heading', { name: 'Connect Monarch Money' })).toBeVisible()
    await page.getByLabel('Monarch email').fill(secrets[0]!)
    await page.getByLabel('Monarch password').fill(secrets[1]!)
    await page.getByLabel('TOTP secret').fill(secrets[2]!)
    await page.getByLabel('Moneyflow account password', { exact: true }).fill(secrets[3]!)
    await page.getByLabel('Confirm Moneyflow account password').fill(secrets[3]!)
    await page.getByRole('button', { name: 'Connect' }).click()

    await expect(page.locator('input[type="password"]')).toHaveCount(0)
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
    await Promise.all(responseTasks)
    const observed = [await page.locator('body').innerHTML(), ...responses, ...consoleMessages]
    observed.push(await server.logs())
    for (const secret of secrets) expect(observed.join('\n')).not.toContain(secret)
  } finally {
    await server.stop()
  }
})

test('profile tokens and onboarding attempts cannot be reused across profiles', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer()
  try {
    await page.goto(server.url)
    const result = await page.evaluate(async (basePath) => {
      const read = async (response: Response) => ({
        status: response.status,
        body: await response.json(),
      })
      const catalogBootstrap = await fetch(`${basePath}api/v1/bootstrap`).then((response) =>
        response.json(),
      )
      const create = async (display_name: string) => {
        const response = await fetch(`${basePath}api/v1/profiles`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Moneyflow-Mutation-Token': catalogBootstrap.mutation_token,
          },
          body: JSON.stringify({ version: '1', display_name, provider_kind: 'monarch' }),
        })
        return (await response.json()).profile.id as string
      }
      const first = await create('Token Alpha')
      const second = await create('Token Bravo')
      const bootstrap = async (profileID: string) =>
        await fetch(`${basePath}api/v1/profiles/${profileID}/bootstrap`).then((response) =>
          response.json(),
        )
      const firstToken = await bootstrap(first)
      const secondToken = await bootstrap(second)
      const start = await fetch(`${basePath}api/v1/profiles/${first}/onboarding/start`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Moneyflow-Mutation-Token': firstToken.mutation_token,
        },
        body: JSON.stringify({
          protocol_version: 1,
          month_to_date: false,
          settings: { currency: 'USD', scale: 2 },
        }),
      }).then(read)
      const wrongToken = await fetch(`${basePath}api/v1/profiles/${second}/onboarding/start`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Moneyflow-Mutation-Token': firstToken.mutation_token,
        },
        body: JSON.stringify({ protocol_version: 1, month_to_date: false }),
      }).then(read)
      const wrongAttempt = await fetch(
        `${basePath}api/v1/profiles/${second}/onboarding/${start.body.attempt_id}/submit`,
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Moneyflow-Mutation-Token': secondToken.mutation_token,
          },
          body: JSON.stringify({
            protocol_version: 1,
            expected_state_version: start.body.state_version,
            action: 'retry',
          }),
        },
      ).then(read)
      return { wrongToken, wrongAttempt }
    }, server.basePath)

    expect(result.wrongToken.status).toBe(403)
    expect(result.wrongToken.body.code).toBe('invalid_token')
    expect(result.wrongAttempt.status).toBeGreaterThanOrEqual(400)
    expect(result.wrongAttempt.body.code).toBe('onboarding_expired')
  } finally {
    await server.stop()
  }
})
