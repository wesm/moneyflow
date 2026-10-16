/** Resolves with the element once the DOM holds it. It waits on DOM changes, with no deadline. */
export function appears(find: () => HTMLElement | null): Promise<HTMLElement> {
  return new Promise((resolve) => {
    const check = () => {
      const element = find()
      if (!element) return
      observer.disconnect()
      resolve(element)
    }
    const observer = new MutationObserver(check)
    observer.observe(document.body, { childList: true, subtree: true, attributes: true })
    check()
  })
}
