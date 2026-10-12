/**
 * The full sidebar shows at 1280 px and wider. kit-ui-check allows only the shared kit
 * breakpoints in CSS, so the shell reads this width through `MediaQuery` instead.
 *
 * The shell has three frames: the phone frame at 640 px and narrower (kit `MEDIA.compact` is
 * `max-width: 640px`, so 640 px itself is a phone), the icon rail from 641 to 1279 px, and the
 * full sidebar from 1280 px.
 */
export const SIDEBAR_FULL_QUERY = '(min-width: 1280px)'

/** Whether focus is in the command bar's search field, where the user may be typing. */
export function inSearch(): boolean {
  return document.activeElement?.closest('.mf-command-search') != null
}
