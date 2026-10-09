import { afterEach, describe, expect, it } from 'vitest'

import { containMenuKeys } from './menu-keys'

function keysReachingWindow(press: () => void): string[] {
  const seen: string[] = []
  const listener = (event: KeyboardEvent) => seen.push(event.key)
  window.addEventListener('keydown', listener)
  try {
    press()
  } finally {
    window.removeEventListener('keydown', listener)
  }
  return seen
}

function press(target: Element, key: string): void {
  target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
}

describe('containMenuKeys', () => {
  afterEach(() => document.body.replaceChildren())

  it('passes every key while the menu is closed', () => {
    const wrapper = document.createElement('div')
    wrapper.innerHTML = '<button aria-expanded="false">Menu</button>'
    document.body.append(wrapper)
    containMenuKeys(wrapper)
    const trigger = wrapper.querySelector('button')!
    expect(
      keysReachingWindow(() => ['d', 'ArrowDown'].forEach((key) => press(trigger, key))),
    ).toEqual(['d', 'ArrowDown'])
  })

  it('keeps keys in an open panel except Escape and Tab', () => {
    const wrapper = document.createElement('div')
    wrapper.innerHTML =
      '<button>Menu</button><div class="kit-popover-card"><button>Item</button></div>'
    document.body.append(wrapper)
    containMenuKeys(wrapper)
    const item = wrapper.querySelector('.kit-popover-card button')!
    const keys = ['ArrowDown', 'd', 'Enter', 'Escape', 'Tab']
    expect(keysReachingWindow(() => keys.forEach((key) => press(item, key)))).toEqual([
      'Escape',
      'Tab',
    ])
  })

  it('keeps keys on an expanded trigger and stops after cleanup', () => {
    const wrapper = document.createElement('div')
    wrapper.innerHTML = '<button aria-expanded="true">Menu</button>'
    document.body.append(wrapper)
    const remove = containMenuKeys(wrapper)
    const trigger = wrapper.querySelector('button')!
    expect(keysReachingWindow(() => press(trigger, 'g'))).toEqual([])
    remove()
    expect(keysReachingWindow(() => press(trigger, 'g'))).toEqual(['g'])
  })
})
