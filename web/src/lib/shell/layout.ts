/** Whether focus is in the command bar's search field, where the user may be typing. */
export function inSearch(): boolean {
  return document.activeElement?.closest('.mf-command-search') != null
}
