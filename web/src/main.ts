import { initTheme } from '@kenn-io/kit-ui'
import { mount } from 'svelte'

import App from './App.svelte'
import './app.css'
import { readBasePath } from './lib/controller/base-path'

const target = document.getElementById('app')
if (!target) throw new Error('Moneyflow application mount is missing.')

initTheme()
mount(App, { target, props: { basePath: readBasePath(document) } })
