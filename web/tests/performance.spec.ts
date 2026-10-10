import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

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

// Until the charts draw, the samplers time the same paths through the table: a cursor key to
// the grid's new active row, and a row double-click to the drilled URL.
test('chart cursor and drill p95 stay within measured budgets', async ({ page, server }) => {
  test.setTimeout(120_000)
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
  await page.evaluate(() => {
    const performanceWindow = window as unknown as PerformanceWindow
    performanceWindow.moneyflowCursorSamples = []
    performanceWindow.moneyflowDrillSamples = []
  })

  for (let index = 0; index < sampleCount; index += 1) {
    await grid.evaluate((element) => {
      const before = element.getAttribute('aria-activedescendant')
      window.addEventListener(
        'keydown',
        () => {
          const started = performance.now()
          const observer = new MutationObserver(() => {
            if (element.getAttribute('aria-activedescendant') === before) return
            observer.disconnect()
            const performanceWindow = window as unknown as PerformanceWindow
            performanceWindow.moneyflowCursorSamples.push(performance.now() - started)
          })
          observer.observe(element, {
            attributes: true,
            attributeFilter: ['aria-activedescendant'],
          })
        },
        { capture: true, once: true },
      )
    })
    await page.keyboard.press('j')
    await page.waitForFunction(
      (count) => (window as unknown as PerformanceWindow).moneyflowCursorSamples.length === count,
      index + 1,
    )
    // Back up untimed, so the cursor never runs out of rows.
    const moved = await grid.getAttribute('aria-activedescendant')
    await page.keyboard.press('k')
    await expect(grid).not.toHaveAttribute('aria-activedescendant', moved!)
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
