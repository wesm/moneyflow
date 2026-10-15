import { getFlashes } from '@kenn-io/kit-ui'

import { status } from '../lib/shell/status.svelte'

/** What the user is shown: failure banners, and the notice in the status bar. */
export function notices(): {
  banners: { message: string; tone: string | undefined }[]
  status: string
} {
  return {
    banners: getFlashes().map(({ message, tone }) => ({ message, tone })),
    status: status.notice,
  }
}
