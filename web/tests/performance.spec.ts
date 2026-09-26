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

test('chart cursor and drill p95 stay within measured budgets', async ({ page, server }) => {
  test.setTimeout(120_000)
  await openMoneyflow(page, server)
  const visualizations = page.getByRole('complementary', { name: 'Visualizations' })
  const marks = visualizations.getByRole('button')
  expect(await marks.count()).toBeGreaterThanOrEqual(2)
  await page.evaluate(() => {
    const performanceWindow = window as unknown as PerformanceWindow
    performanceWindow.moneyflowCursorSamples = []
    performanceWindow.moneyflowDrillSamples = []
  })

  for (let index = 0; index < sampleCount; index += 1) {
    const mark = marks.nth((index + 1) % 2)
    await mark.evaluate((element) => {
      element.addEventListener(
        'click',
        () => {
          const started = performance.now()
          const observer = new MutationObserver(() => {
            if (element.getAttribute('aria-pressed') !== 'true') return
            observer.disconnect()
            const performanceWindow = window as unknown as PerformanceWindow
            performanceWindow.moneyflowCursorSamples.push(performance.now() - started)
          })
          observer.observe(element, { attributes: true, attributeFilter: ['aria-pressed'] })
        },
        { capture: true, once: true },
      )
    })
    await mark.dispatchEvent('click')
    await expect(mark).toHaveAttribute('aria-pressed', 'true')
    await page.waitForFunction(
      (count) => (window as unknown as PerformanceWindow).moneyflowCursorSamples.length === count,
      index + 1,
    )
  }

  const initialURL = page.url()
  const drillMark = marks.nth(1)
  const drillLabel = (await drillMark.getAttribute('aria-label'))!.split(',')[0]!
  for (let index = 0; index < sampleCount; index += 1) {
    await drillMark.evaluate((element) => {
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
    await drillMark.dispatchEvent('dblclick')
    await expect(page.getByRole('navigation', { name: 'Active refinements' })).toContainText(
      drillLabel,
    )
    await page.waitForFunction(
      (count) => (window as unknown as PerformanceWindow).moneyflowDrillSamples.length === count,
      index + 1,
    )
    await page.goBack()
    await expect(page).toHaveURL(initialURL)
    await expect(drillMark).toBeVisible()
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
