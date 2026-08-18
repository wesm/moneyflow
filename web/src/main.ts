import { initTheme } from '@kenn-io/kit-ui'
import { mount } from 'svelte'

import App from './App.svelte'
import './app.css'
import { readBasePath } from './lib/controller/base-path'
import { parseApplicationRoute } from './lib/controller/routing'

const target = document.getElementById('app')
if (!target) throw new Error('Moneyflow application mount is missing.')

initTheme()
const basePath = readBasePath(document)
const route = parseApplicationRoute(basePath, location)
if (route.kind === 'profile') {
  mount(App, { target, props: { basePath, profileID: route.profileID } })
} else {
  target.innerHTML =
    '<main class="moneyflow-loading" aria-live="polite"><h1>Loading profiles…</h1></main>'
}
