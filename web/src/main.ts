import { initTheme } from '@kenn-io/kit-ui'
import { mount } from 'svelte'

import App from './App.svelte'
import './app.css'
import { normalizeBrowserBasePath } from './lib/controller/base-path'

export function readBasePath(documentValue: Document): string {
  const value = documentValue
    .querySelector<HTMLMetaElement>('meta[name="moneyflow-base-path"]')
    ?.getAttribute('content')
  if (value === null || value === undefined || value.includes('__')) {
    throw new Error('Moneyflow base path is invalid.')
  }
  return normalizeBrowserBasePath(value)
}

const target = document.getElementById('app')
if (!target) throw new Error('Moneyflow application mount is missing.')

initTheme()
mount(App, { target, props: { basePath: readBasePath(document) } })
