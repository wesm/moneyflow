import { cleanup, render, screen } from '@testing-library/svelte'
import { createRawSnippet } from 'svelte'
import { afterEach, describe, expect, it } from 'vitest'

import CommandBar from './CommandBar.svelte'

function snippet(text: string) {
  return createRawSnippet(() => ({ render: () => `<span>${text}</span>` }))
}

describe('CommandBar', () => {
  afterEach(cleanup)

  it('renders the leading, center, and trailing content in order', () => {
    const { container } = render(CommandBar, {
      leading: snippet('Trail'),
      center: snippet('Period'),
      trailing: snippet('Charts'),
    })
    expect(container.textContent?.replace(/\s+/g, '')).toBe('TrailPeriodCharts')
    expect(screen.getByText('Period')).not.toBeNull()
  })

  it('renders the row below the bar last', () => {
    const { container } = render(CommandBar, {
      leading: snippet('Trail'),
      center: snippet('Period'),
      below: snippet('Search'),
    })
    expect(container.textContent?.replace(/\s+/g, '')).toBe('TrailPeriodSearch')
  })

  it('renders without optional content', () => {
    const { container } = render(CommandBar, { leading: snippet('Trail') })
    expect(container.textContent?.trim()).toBe('Trail')
  })

  it('is the page banner by default', () => {
    render(CommandBar, { leading: snippet('Trail') })
    expect(screen.getByRole('banner').textContent).toContain('Trail')
  })

  it('is the View controls region where the sidebar bar is the banner', () => {
    render(CommandBar, { leading: snippet('Trail'), banner: false })
    expect(screen.queryByRole('banner')).toBeNull()
    expect(screen.getByRole('region', { name: 'View controls' }).textContent).toContain('Trail')
  })
})
