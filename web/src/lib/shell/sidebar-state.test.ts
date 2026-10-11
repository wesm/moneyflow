import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { SidebarState } from './sidebar-state.svelte'
import { memoryStorage } from '../../test/storage'

const wide = { current: true }
const narrow = { current: false }

describe('SidebarState', () => {
  beforeEach(() => vi.stubGlobal('localStorage', memoryStorage()))
  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.replaceChildren()
  })

  it('shows the full sidebar on a wide window until the viewer collapses it', async () => {
    const sidebar = new SidebarState(wide)
    expect(sidebar.wide).toBe(true)
    expect(sidebar.full).toBe(true)

    await sidebar.setCollapsed(true)
    expect(sidebar.full).toBe(false)
    expect(sidebar.wide).toBe(true)
    await sidebar.setCollapsed(false)
    expect(sidebar.full).toBe(true)
  })

  it('shows no full sidebar on a narrower window, whatever the choice', async () => {
    const sidebar = new SidebarState(narrow)
    expect(sidebar.wide).toBe(false)
    expect(sidebar.full).toBe(false)
    await sidebar.setCollapsed(false)
    expect(sidebar.full).toBe(false)
  })

  it('remembers the choice in this browser', async () => {
    await new SidebarState(wide).setCollapsed(true)
    expect(new SidebarState(wide).full).toBe(false)

    await new SidebarState(wide).setCollapsed(false)
    expect(new SidebarState(wide).full).toBe(true)
  })

  it('works without browser storage', async () => {
    const refuse = () => {
      throw new Error('storage is disabled')
    }
    vi.stubGlobal('localStorage', { getItem: refuse, setItem: refuse })
    const sidebar = new SidebarState(wide)
    expect(sidebar.full).toBe(true)
    await sidebar.setCollapsed(true)
    expect(sidebar.full).toBe(false)
    expect(new SidebarState(wide).full).toBe(true)
  })

  it('focuses the control that undoes the change', async () => {
    const control = document.createElement('span')
    control.className = 'mf-collapse-control'
    const toggle = control.appendChild(document.createElement('button'))
    document.body.append(control)
    await new SidebarState(wide).setCollapsed(true)
    expect(document.activeElement).toBe(toggle)
  })
})
