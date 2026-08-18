export function normalizeBrowserBasePath(input: string): string {
  const candidate = input === '' ? '/' : input
  if (
    candidate.includes('\\') ||
    candidate.includes('?') ||
    candidate.includes('#') ||
    candidate.includes('\0') ||
    candidate.includes('\r') ||
    candidate.includes('\n') ||
    (candidate !== '/' && candidate.startsWith('//')) ||
    /%2f|%5c/i.test(candidate)
  ) {
    throw new Error('Moneyflow base path is invalid.')
  }
  let decoded: string
  try {
    decoded = decodeURIComponent(candidate)
  } catch {
    throw new Error('Moneyflow base path is invalid.')
  }
  if (
    decoded.includes('%') ||
    decoded.includes('\\') ||
    decoded.includes('?') ||
    decoded.includes('#') ||
    decoded.includes('\0') ||
    decoded.includes('\r') ||
    decoded.includes('\n') ||
    decoded.includes('://')
  )
    throw new Error('Moneyflow base path is invalid.')
  const segments = decoded.split('/').filter((segment, index, values) => {
    if (segment !== '') return true
    return index === 0 || index === values.length - 1
  })
  if (segments.some((segment) => segment === '.' || segment === '..')) {
    throw new Error('Moneyflow base path is invalid.')
  }
  if (decoded === '/') return '/'
  const normalized = `/${decoded.replace(/^\/+|\/+$/g, '')}/`
  if (normalized.includes('//')) throw new Error('Moneyflow base path is invalid.')
  return normalized
}

export function readBasePath(documentValue: Document): string {
  const value = documentValue
    .querySelector<HTMLMetaElement>('meta[name="moneyflow-base-path"]')
    ?.getAttribute('content')
  if (value === null || value === undefined || value === '__MONEYFLOW_BASE_PATH__') {
    throw new Error('Moneyflow base path is invalid.')
  }
  return normalizeBrowserBasePath(value)
}

export function apiURL(basePath: string, endpoint: string): string {
  const base = normalizeBrowserBasePath(basePath)
  const relative = endpoint.replace(/^\/+/, '')
  if (relative === '' || relative.includes('..') || relative.includes('\\')) {
    throw new Error('Moneyflow API path is invalid.')
  }
  return `${base}${relative}`
}

export function applicationURL(applicationPath: string, canonicalQuery: string): string {
  const base = normalizeBrowserBasePath(applicationPath)
  return canonicalQuery === '' ? base : `${base}?${canonicalQuery}`
}
