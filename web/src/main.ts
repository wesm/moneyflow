import { initTheme } from '@kenn-io/kit-ui'
import { mount } from 'svelte'

import App from './App.svelte'
import './app.css'

export function readBasePath(documentValue: Document): string {
  const value = documentValue
    .querySelector<HTMLMetaElement>('meta[name="moneyflow-base-path"]')
    ?.getAttribute('content')
  if (
    value === null ||
    value === undefined ||
    !value.startsWith('/') ||
    !value.endsWith('/') ||
    value.includes('\\') ||
    value.includes('?') ||
    value.includes('#') ||
    value.includes('__') ||
    value.split('/').some((segment) => segment === '.' || segment === '..')
  ) {
    throw new Error('Moneyflow base path is invalid.')
  }
  return value
}

const target = document.getElementById('app')
if (!target) throw new Error('Moneyflow application mount is missing.')

initTheme()
mount(App, { target, props: { basePath: readBasePath(document) } })
