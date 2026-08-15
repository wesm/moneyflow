import { expect, test } from '@playwright/test'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('pending edits and revision survive a real server restart on one explicit profile', async ({
  page,
}) => {
  const profileHome = await mkdtemp(join(tmpdir(), 'moneyflow-restart-profile-'))
  let first = await startE2EServer({ profileHome, seedProfile: true })
  try {
    await openMoneyflow(page, first)
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await first.stop()

    const second = await startE2EServer({ profileHome })
    first = second
    await openMoneyflow(page, second)
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(page.getByText('profile revision 2')).toBeVisible()
    await expect(page.locator('.finance-grid__row--pending')).not.toHaveCount(0)
  } finally {
    await first.stop()
    await rm(profileHome, { recursive: true, force: true })
  }
})
