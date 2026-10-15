import './browser'

import { dismissFlash } from '@kenn-io/kit-ui'
import { afterEach, beforeEach } from 'vitest'

import { status } from '../lib/shell/status.svelte'
import { memoryStorage } from './storage'

// Each test starts with empty storage. Defining the globals without reading them first avoids
// Node's built-in Web Storage, whose getter warns when no storage file is configured.
beforeEach(() => {
  for (const name of ['localStorage', 'sessionStorage']) {
    Object.defineProperty(globalThis, name, { configurable: true, value: memoryStorage() })
  }
})

// Banners and the status bar notice are module state; each test starts without them.
afterEach(() => {
  dismissFlash()
  status.clear()
})
