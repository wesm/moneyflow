import { expect, test, type Locator, type Page } from '@playwright/test'

import { openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

/** Types `text` into a step's picker, takes the first suggestion, and confirms the step. */
async function pickAndConfirm(page: Page, picker: Locator, text: string): Promise<void> {
  await expect(picker).toBeFocused()
  await page.keyboard.type(text)
  await page.keyboard.press('Enter')
  await expect(picker).toHaveValue(text)
  await page.keyboard.press('Enter')
  await expect(picker).toBeHidden()
}

test('the category manager stages every action from its own keys', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const view = page.url()
    await page.keyboard.press('Shift+C')
    const manager = page.getByRole('dialog', { name: 'Manage categories' })
    const filter = manager.getByLabel('Filter categories')
    const list = manager.getByRole('listbox', { name: 'Categories' })
    const name = manager.getByRole('textbox', { name: 'Name' })
    await expect(filter).toBeFocused()

    // Letters typed in the filter filter the list; ArrowDown leaves it for the list's keys.
    await page.keyboard.type('din')
    await expect(filter).toHaveValue('din')
    await expect(name).toBeHidden()
    await expect(list.getByRole('option')).toHaveCount(1)
    await expect(list.getByRole('option').first()).toHaveAccessibleName(/^Dining, Food, \d+ /)
    await page.keyboard.press('ArrowDown')
    await expect(list).toBeFocused()

    await page.keyboard.press('r')
    await expect(name).toBeFocused()
    await expect(name).toHaveValue('Dining')
    await page.keyboard.type('Dining Out')
    await page.keyboard.press('Enter')
    await expect(name).toBeHidden()
    await expect(list).toBeFocused()
    await expect(list.getByRole('option', { name: /^Dining Out, Food,/ })).toBeVisible()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()

    // A name another category has needs a merge, as in the TUI; Esc then steps out of the step.
    await page.keyboard.press('r')
    await page.keyboard.type('coffee')
    await expect(manager.getByRole('alert')).toHaveText(
      'That label already exists; use merge instead.',
    )
    await page.keyboard.press('Enter')
    await expect(manager.getByRole('button', { name: 'Rename', exact: true })).toBeDisabled()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(name).toBeHidden()
    await expect(list).toBeFocused()

    await page.keyboard.press('g')
    const group = manager.getByRole('combobox', { name: 'Group' })
    await expect(group).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(manager.getByRole('button', { name: 'Move', exact: true })).toBeDisabled()
    await pickAndConfirm(page, group, 'Entertainment')
    await expect(list.getByRole('option', { name: /^Dining Out, Entertainment,/ })).toBeVisible()
    await expect(page.getByText(/2 pending edit/)).toBeVisible()

    await page.keyboard.press('m')
    const destination = manager.getByRole('combobox', { name: 'Destination' })
    await expect(manager.getByRole('heading', { name: 'Merge Dining Out into' })).toBeVisible()
    await expect(manager.getByRole('button', { name: 'Merge', exact: true })).toBeDisabled()
    await pickAndConfirm(page, destination, 'Coffee')
    await expect(page.getByText(/3 pending edit/)).toBeVisible()
    await expect(list.getByRole('option')).toHaveCount(0)

    // Up from the empty list returns to the filter, where Esc clears it.
    await page.keyboard.press('ArrowUp')
    await expect(filter).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(filter).toHaveValue('')
    await page.keyboard.press('ArrowDown')

    await page.keyboard.press('n')
    await expect(name).toBeFocused()
    await expect(manager.getByRole('combobox', { name: /^Category group: / })).toBeVisible()
    await page.keyboard.type('Pet Care')
    await page.keyboard.press('Enter')
    await expect(page.getByText(/4 pending edit/)).toBeVisible()
    await expect(list.getByRole('option', { name: /^Pet Care,/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )

    // A category without transactions deletes from its step with Enter, with no replacement.
    const replacement = manager.getByRole('combobox', { name: 'Replacement' })
    await page.keyboard.press('d')
    await expect(manager.getByRole('heading', { name: 'Delete Pet Care' })).toBeVisible()
    await expect(manager.getByText('0 transactions', { exact: true })).toBeVisible()
    await expect(replacement).toHaveCount(0)
    await page.keyboard.press('Enter')
    await expect(page.getByText(/5 pending edit/)).toBeVisible()
    await expect(list.getByRole('option', { name: /^Pet Care,/ })).toHaveCount(0)

    // A category with transactions shows their count and needs a replacement.
    await page.keyboard.press('ArrowUp')
    await expect(filter).toBeFocused()
    await page.keyboard.type('coffee')
    await page.keyboard.press('ArrowDown')
    const coffee = list.getByRole('option', { name: /^Coffee, Food, \d+ transactions$/ })
    const count = (await coffee.getAttribute('aria-label'))?.match(/(\d+) transactions$/)?.[1]
    expect(Number(count)).toBeGreaterThan(0)
    await page.keyboard.press('d')
    await expect(manager.getByText(`${count} transactions`, { exact: true })).toBeVisible()
    await expect(replacement).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(manager.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
    await pickAndConfirm(page, replacement, 'Groceries')
    await expect(page.getByText(/6 pending edit/)).toBeVisible()
    await expect(list.getByRole('option')).toHaveCount(0)
    await page.keyboard.press('ArrowUp')
    await page.keyboard.press('Escape')
    await expect(filter).toHaveValue('')
    await page.keyboard.press('ArrowDown')

    // A protected category starts no step, and the keys never reach the table.
    await page.keyboard.press('Home')
    const first = list.getByRole('option').first()
    await expect(first).toHaveAttribute('aria-selected', 'true')
    await expect(first).toHaveAccessibleName(/, protected, /)
    for (const key of ['r', 'g', 'm', 'd']) await page.keyboard.press(key)
    await expect(manager.getByRole('heading', { level: 2 })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(manager).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page).toHaveURL(view)
  } finally {
    await server.stop()
  }
})

test('a category that only pending edits use still needs a replacement', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await page.keyboard.press('Shift+C')
    const manager = page.getByRole('dialog', { name: 'Manage categories' })
    const list = manager.getByRole('listbox', { name: 'Categories' })
    await expect(manager.getByLabel('Filter categories')).toBeFocused()
    await expect(list.getByRole('option').first()).toBeVisible()
    await page.keyboard.press('ArrowDown')
    await expect(list).toBeFocused()
    await page.keyboard.press('n')
    await expect(manager.getByRole('textbox', { name: 'Name' })).toBeFocused()
    await page.keyboard.type('Pet Care')
    await page.keyboard.press('Enter')
    await expect(list.getByRole('option', { name: /^Pet Care, .*, 0 transactions$/ })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(grid).toBeFocused()

    await page.keyboard.press('c')
    const editor = page.getByRole('dialog', { name: 'Change category' })
    const field = editor.getByRole('combobox', { name: 'Category' })
    await field.fill('Pet Care')
    await page.keyboard.press('Enter')
    await expect(field).toHaveValue('Pet Care')
    await expect(editor.getByRole('button', { name: /^Stage change to / })).toBeEnabled()
    await page.keyboard.press('Enter')
    await expect(editor).toBeHidden()
    await expect(page.getByText(/2 pending edit/)).toBeVisible()

    // The catalog still counts 0 committed transactions, but the pending edit uses Pet Care.
    await page.keyboard.press('Shift+C')
    await expect(manager.getByLabel('Filter categories')).toBeFocused()
    await page.keyboard.type('pet care')
    await expect(list.getByRole('option')).toHaveCount(1)
    await page.keyboard.press('ArrowDown')
    await expect(list).toBeFocused()
    await page.keyboard.press('d')
    await expect(manager.getByRole('heading', { name: 'Delete Pet Care' })).toBeVisible()
    await expect(
      manager.getByText('Pending edits use this category', { exact: true }),
    ).toBeVisible()
    const replacement = manager.getByRole('combobox', { name: 'Replacement' })
    await expect(replacement).toBeFocused()
    await pickAndConfirm(page, replacement, 'Groceries')
    await expect(page.getByText(/3 pending edit/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('the group manager stages every action and has no move', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const view = page.url()
    await page.keyboard.press('Shift+G')
    const manager = page.getByRole('dialog', { name: 'Manage category groups' })
    const filter = manager.getByLabel('Filter groups')
    const list = manager.getByRole('listbox', { name: 'Category groups' })
    const name = manager.getByRole('textbox', { name: 'Name' })
    await expect(filter).toBeFocused()
    await expect(manager.getByRole('button', { name: 'Move to group' })).toHaveCount(0)

    await page.keyboard.type('trav')
    await expect(list.getByRole('option')).toHaveCount(1)
    await page.keyboard.press('ArrowDown')
    await expect(list).toBeFocused()
    await page.keyboard.press('r')
    await expect(name).toHaveValue('Travel')
    await page.keyboard.type('Travel Plans')
    await page.keyboard.press('Enter')
    await expect(list.getByRole('option', { name: /^Travel Plans,/ })).toBeVisible()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()

    // g has no group action here, and the drawer keeps it from the table's grouping key.
    await page.keyboard.press('g')
    await expect(manager.getByRole('heading', { level: 2 })).toHaveCount(0)

    await page.keyboard.press('m')
    const destination = manager.getByRole('combobox', { name: 'Destination' })
    await expect(destination).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(manager.getByRole('button', { name: 'Merge', exact: true })).toBeDisabled()
    await pickAndConfirm(page, destination, 'Entertainment')
    await expect(page.getByText(/2 pending edit/)).toBeVisible()
    await expect(list.getByRole('option')).toHaveCount(0)
    await page.keyboard.press('ArrowUp')
    await page.keyboard.press('Escape')
    await expect(filter).toHaveValue('')
    await page.keyboard.press('ArrowDown')

    await page.keyboard.press('n')
    await expect(name).toBeFocused()
    await expect(manager.getByRole('combobox', { name: /^Category group/ })).toHaveCount(0)
    await page.keyboard.type('Pets')
    await page.keyboard.press('Enter')
    await expect(page.getByText(/3 pending edit/)).toBeVisible()
    await expect(list.getByRole('option', { name: /^Pets,/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )

    // A group without categories deletes with Enter; one with categories needs a replacement.
    const replacement = manager.getByRole('combobox', { name: 'Replacement' })
    await page.keyboard.press('d')
    await expect(manager.getByRole('heading', { name: 'Delete Pets' })).toBeVisible()
    await expect(replacement).toHaveCount(0)
    await page.keyboard.press('Enter')
    await expect(page.getByText(/4 pending edit/)).toBeVisible()
    await expect(list.getByRole('option', { name: /^Pets,/ })).toHaveCount(0)

    await page.keyboard.press('ArrowUp')
    await page.keyboard.type('entertainment')
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('d')
    await expect(replacement).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(manager.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
    await pickAndConfirm(page, replacement, 'Shopping')
    await expect(page.getByText(/5 pending edit/)).toBeVisible()
    await expect(list.getByRole('option')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(filter).toHaveValue('')

    await page.keyboard.press('Escape')
    await expect(manager).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page).toHaveURL(view)
  } finally {
    await server.stop()
  }
})

test('a pointer alone can start, leave, and confirm a manager step', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.getByRole('button', { name: 'Edit categories' }).click()
    const manager = page.getByRole('dialog', { name: 'Manage categories' })
    const list = manager.getByRole('listbox', { name: 'Categories' })
    await list.getByRole('option', { name: /^Books,/ }).click()
    await expect(list.getByRole('option', { name: /^Books,/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await manager.getByRole('button', { name: 'Rename' }).click()
    const name = manager.getByRole('textbox', { name: 'Name' })
    await expect(name).toHaveValue('Books')
    await manager.getByRole('button', { name: 'Back' }).click()
    await expect(name).toBeHidden()

    await manager.getByRole('button', { name: 'Rename' }).click()
    await name.fill('Reading')
    await manager.getByRole('button', { name: 'Rename', exact: true }).click()
    await expect(name).toBeHidden()
    await expect(list.getByRole('option', { name: /^Reading, Shopping,/ })).toBeVisible()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await manager.getByRole('button', { name: 'Close' }).click()
    await expect(manager).toBeHidden()
  } finally {
    await server.stop()
  }
})
