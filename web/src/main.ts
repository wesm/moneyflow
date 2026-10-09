import { mount } from 'svelte'

import App from './App.svelte'
import './app.css'
import { readBasePath } from './lib/controller/base-path'
import { parseApplicationRoute } from './lib/controller/routing'
import { initMoneyflowTheme } from './lib/shell/theme'

const target = document.getElementById('app')
if (!target) throw new Error('Moneyflow application mount is missing.')

initMoneyflowTheme()
const basePath = readBasePath(document)
const route = parseApplicationRoute(basePath, location)
if (route.kind === 'profile') {
  mount(App, { target, props: { basePath, profileID: route.profileID } })
} else {
  mount(App, { target, props: { basePath } })
}
