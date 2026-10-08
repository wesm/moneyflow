import { expect, test } from '@playwright/test'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { startE2EServer } from '../scripts/e2e-server'

test('@smoke Demo opens editable synthetic transactions from the profile selector', async ({
  page,
}) => {
  const profileHome = await mkdtemp(join(tmpdir(), 'moneyflow-demo-selector-'))
  const server = await startE2EServer({ profileHome, selector: true, basePath: '/moneyflow/' })
  try {
    await page.goto(server.url)
    await page.getByRole('button', { name: /Demo/ }).click()

    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toBeFocused()
    await expect(page).toHaveURL(/\/moneyflow\/p\/profile_[a-z2-7]{26}\//)
    expect(Number(await grid.getAttribute('aria-rowcount'))).toBeGreaterThan(1)
    const demoURL = page.url()
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.reload()
    await expect(page.getByText(/1 pending/)).toBeVisible()

    await page.goto(server.url)
    await expect(page.getByRole('heading', { name: 'Choose a Moneyflow profile' })).toBeVisible()
    await page.keyboard.press('d')
    await expect(grid).toBeFocused()
    expect(page.url()).toBe(demoURL)
    await expect(page.getByText(/1 pending/)).toBeVisible()
    const catalog = await page.request.get(`${server.url}api/v1/profiles`)
    expect(await catalog.json()).toEqual({ version: '1', profiles: [] })
  } finally {
    await server.stop()
    await rm(profileHome, { recursive: true, force: true })
  }
})
