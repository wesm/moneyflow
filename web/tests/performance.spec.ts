import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

import type { Locator } from '@playwright/test'

import type { BudgetConfig, InteractionMeasurements } from '../scripts/check-budgets'
import { checkBudgets } from '../scripts/check-budgets'

import { expect, openMoneyflow, test } from './fixtures'

const sampleCount = 100

interface PerformanceWindow extends Window {
  moneyflowCursorSamples: number[]
  moneyflowDrillSamples: number[]
}

function p95(samples: number[]): number {
  const ordered = [...samples].sort((left, right) => left - right)
  return ordered[Math.ceil(ordered.length * 0.95) - 1]!
}

/** The identity of the grid's cursor row, from its row element's id. */
async function cursorIdentity(grid: Locator): Promise<string> {
  const id = await grid.getAttribute('aria-activedescendant')
  expect(id).toMatch(/^moneyflow-row-/)
  return id!.slice('moneyflow-row-'.length)
}

// chart_cursor times a cursor key to the cash-flow chart drawing the new row's columns solid.
// Until the composition chart takes input, chart_drill times a row double-click to the drilled URL.
test('chart cursor and drill p95 stay within measured budgets', async ({ page, server }) => {
  test.setTimeout(120_000)
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const chart = page.locator('.mf-cashflow')
  await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
  await expect(chart).toHaveAttribute('data-cursor', await cursorIdentity(grid))
  await page.evaluate(() => {
    const performanceWindow = window as unknown as PerformanceWindow
    performanceWindow.moneyflowCursorSamples = []
    performanceWindow.moneyflowDrillSamples = []
  })

  for (let index = 0; index < sampleCount; index += 1) {
    await chart.evaluate((element) => {
      const before = element.getAttribute('data-cursor')
      window.addEventListener(
        'keydown',
        () => {
          const started = performance.now()
          const observer = new MutationObserver(() => {
            const drawn = element.getAttribute('data-cursor')
            if (drawn === null || drawn === before) return
            observer.disconnect()
            const performanceWindow = window as unknown as PerformanceWindow
            performanceWindow.moneyflowCursorSamples.push(performance.now() - started)
          })
          observer.observe(element, { attributes: true, attributeFilter: ['data-cursor'] })
        },
        { capture: true, once: true },
      )
    })
    await page.keyboard.press('j')
    await page.waitForFunction(
      (count) => (window as unknown as PerformanceWindow).moneyflowCursorSamples.length === count,
      index + 1,
    )
    // The solid columns belong to the row the cursor moved to.
    const moved = await cursorIdentity(grid)
    await expect(chart).toHaveAttribute('data-cursor', moved)
    // Back up untimed, so the cursor never runs out of rows.
    await page.keyboard.press('k')
    await expect(chart).not.toHaveAttribute('data-cursor', moved)
  }

  const initialURL = page.url()
  const drillRow = grid.locator('[role="row"][aria-rowindex="3"]')
  const drillTarget = drillRow.locator('[role="gridcell"][data-column="count"]')
  const drillLabel = await drillRow.locator('[role="gridcell"][data-column="name"]').innerText()
  for (let index = 0; index < sampleCount; index += 1) {
    await drillTarget.evaluate((element) => {
      element.addEventListener(
        'dblclick',
        () => {
          const started = performance.now()
          const initialLocation = location.href
          const poll = (): void => {
            if (location.href !== initialLocation) {
              const performanceWindow = window as unknown as PerformanceWindow
              performanceWindow.moneyflowDrillSamples.push(performance.now() - started)
              return
            }
            requestAnimationFrame(poll)
          }
          requestAnimationFrame(poll)
        },
        { capture: true, once: true },
      )
    })
    await drillTarget.dblclick()
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(drillLabel)
    await page.waitForFunction(
      (count) => (window as unknown as PerformanceWindow).moneyflowDrillSamples.length === count,
      index + 1,
    )
    await page.goBack()
    await expect(page).toHaveURL(initialURL)
    await expect(drillTarget).toBeVisible()
  }

  const { cursorSamples, drillSamples } = await page.evaluate(() => {
    const performanceWindow = window as unknown as PerformanceWindow
    return {
      cursorSamples: performanceWindow.moneyflowCursorSamples,
      drillSamples: performanceWindow.moneyflowDrillSamples,
    }
  })
  const measurements: InteractionMeasurements = {
    chart_cursor: { percentile: 'p95', samples: sampleCount, value_ms: p95(cursorSamples) },
    chart_drill: { percentile: 'p95', samples: sampleCount, value_ms: p95(drillSamples) },
  }
  const budgets = JSON.parse(
    await readFile(resolve(process.cwd(), 'budgets.json'), 'utf8'),
  ) as BudgetConfig
  const results = await checkBudgets(resolve(process.cwd(), 'dist'), budgets, measurements)
  const interactions = results.filter((result) => result.unit === 'ms')
  console.log(`web interaction measurements: ${JSON.stringify(interactions)}`)
})
