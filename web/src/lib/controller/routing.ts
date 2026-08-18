import { normalizeBrowserBasePath } from './base-path'

const profileIDPattern = /^profile_[a-z2-7]{26}$/

export type ApplicationRoute = { kind: 'catalog' } | { kind: 'profile'; profileID: string }

export function parseApplicationRoute(
  basePath: string,
  location: Pick<URL, 'pathname'>,
): ApplicationRoute {
  const base = normalizeBrowserBasePath(basePath)
  if (location.pathname === base) return { kind: 'catalog' }
  if (!location.pathname.startsWith(`${base}p/`)) invalidRoute()
  const relative = location.pathname.slice(`${base}p/`.length)
  if (!relative.endsWith('/') || relative.slice(0, -1).includes('/')) invalidRoute()
  const profileID = relative.slice(0, -1)
  if (!profileIDPattern.test(profileID)) invalidRoute()
  return { kind: 'profile', profileID }
}

export function profileApplicationPath(basePath: string, profileID: string): string {
  assertProfileID(profileID)
  return `${normalizeBrowserBasePath(basePath)}p/${profileID}/`
}

export function profileAPIBase(basePath: string, profileID: string): string {
  assertProfileID(profileID)
  return `${normalizeBrowserBasePath(basePath)}api/v1/profiles/${profileID}/`
}

export function assetBase(basePath: string): string {
  return normalizeBrowserBasePath(basePath)
}

function assertProfileID(profileID: string): void {
  if (!profileIDPattern.test(profileID)) invalidRoute()
}

function invalidRoute(): never {
  throw new Error('Moneyflow application route is invalid.')
}
