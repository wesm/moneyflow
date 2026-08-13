import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import App from './App.svelte'

describe('Moneyflow application scaffold', () => {
  afterEach(cleanup)

  it('mounts the shared chrome around a fixture loading state', () => {
    render(App, { basePath: '/moneyflow/' })

    expect(screen.getByRole('main', { name: 'Moneyflow' })).not.toBeNull()
    expect(screen.getByRole('heading', { name: 'Loading financial view…' })).not.toBeNull()
    expect(screen.getByRole('status').textContent).toBe('Loading fixture data…')
    expect(screen.getByRole('button', { name: /change theme/i })).not.toBeNull()
  })
})
