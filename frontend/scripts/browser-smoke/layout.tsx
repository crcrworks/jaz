import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRoute, createRouter, RouterProvider } from '@tanstack/react-router'
import { useContext, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { createRoot } from 'react-dom/client'
import { BrowserPanelSlot, BrowserWorkspace } from '@/components/browser/BrowserWorkspace'
import { SidePanelControl } from '@/components/session/SidePanelControl'
import { SidePanelDrawer } from '@/components/session/SidePanelDrawer'
import { SidePanelStateProvider, useSidePanelState } from '@/components/session/SidePanelState'
import { useBrowserSessions } from '@/lib/browserSessions'
import { useIsMobile } from '@/lib/hooks/useIsMobile'
import { keys } from '@/lib/query/keys'
import { Route as rootRoute } from '@/routes/__root'
import { SidePanelSlot } from '@/lib/sidePanelSlot'
import { setThemePref } from '@/lib/theme'

const PANEL_MIN_THREAD_WIDTH = 360
const OVERVIEW_PANEL_WIDTH = 300

// The panel's width, from the row the panel and conversation share. Mirrors the
// clamp in useSidePanelState: the preferred width yields to the conversation's
// 360 pixels before the panel does.
function panelWidthFor(row: number): number {
  const available = row - PANEL_MIN_THREAD_WIDTH
  const preferred = Math.max(640, row * 0.6)
  return Math.min(Math.max(preferred, Math.min(400, Math.max(240, available))), Math.max(240, Math.min(1180, available)))
}

// A thread's side panel mounts in the root layout beside the conversation, so
// this runs the production root route: the thread list, the panel slot, and the
// panel's own drawer. It checks that opening, closing, switching and resizing
// the panel leaves the thread list alone, that the panel gives way to the
// conversation when the window narrows, and that a phone's full-screen drawer
// and panel never stack.
export async function exerciseBrowserLayout(): Promise<void> {
  const element = document.createElement('div')
  element.style.cssText = 'position:fixed;inset:0;background:var(--color-bg)'
  document.body.append(element)
  const root = createRoot(element)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(keys.browserSettings, { enabled: false, mode: 'desktop' })
  let row: HTMLDivElement | null = null
  function Chat() {
    const sessions = useBrowserSessions()
    const panel = useSidePanelState('layout')
    const isMobile = useIsMobile()
    const slot = useContext(SidePanelSlot)
    useLayoutEffect(() => {
      sessions.update('layout', { target: { sourceUrl: location.origin + '/target', displayUrl: location.origin + '/target' } })
    }, [sessions])
    useLayoutEffect(() => {
      row = slot.row
      return slot.row ? panel.containerRef(slot.row) : undefined
    }, [slot.row, panel.containerRef])
    return <>
      <div data-layout-chat className="flex h-full min-w-0 flex-col">
        <header className="flex h-[52px] shrink-0 items-center justify-end px-4">
          <SidePanelControl open={panel.open} mode={panel.mode} onToggle={panel.toggleMode} />
        </header>
        <div className="min-w-0 flex-1 p-6 text-ink">
          <p className="text-[15px]">Open the browser to review the page alongside this conversation.</p>
        </div>
      </div>
      {slot.slot && createPortal(<SidePanelDrawer panel={panel} isMobile={isMobile}>
        {panel.mode === 'tabs'
          ? <BrowserPanelSlot sessionId="layout" visible={panel.open} onClose={panel.close} />
          : <div className="h-full bg-bg p-6" style={{ width: panel.width }}>Overview</div>}
      </SidePanelDrawer>, slot.slot)}
    </>
  }
  const chatRoute = createRoute({ getParentRoute: () => rootRoute, path: '/sessions/$sessionId', component: Chat })
  const router = createRouter({
    routeTree: rootRoute.addChildren([chatRoute]),
    history: createMemoryHistory({ initialEntries: ['/sessions/layout'] }),
  })
  const until = async (check: () => boolean | Promise<boolean>) => {
    const end = Date.now() + 5000
    while (!await check()) {
      if (Date.now() > end) {
        throw new Error('Browser layout check timed out: ' + check.toString())
      }
      await new Promise((resolve) => setTimeout(resolve, 20))
    }
  }
  const click = async (selector: string) => {
    const target = element.querySelector<HTMLElement>(selector)!
    if (!target) {
      throw new Error('Missing browser layout control: ' + selector)
    }
    const initial = target.getBoundingClientRect()
    await window.smoke.pointer('mouseMove', Math.round(initial.x + initial.width / 2), Math.round(initial.y + initial.height / 2))
    await new Promise((resolve) => setTimeout(resolve, 250))
    const bounds = target.getBoundingClientRect()
    const x = Math.round(bounds.x + bounds.width / 2)
    const y = Math.round(bounds.y + bounds.height / 2)
    await window.smoke.pointer('mouseMove', x, y)
    await window.smoke.pointer('mouseDown', x, y)
    await window.smoke.pointer('mouseUp', x, y)
  }
  const panelRow = () => {
    if (!row) {
      throw new Error('The root layout did not provide a panel row')
    }
    return row
  }
  const drawerElement = () => panelRow().lastElementChild!.firstElementChild as HTMLElement
  const panelWidth = () => drawerElement().getBoundingClientRect().width
  const panelShown = () => !drawerElement().inert
  const rowWidth = () => panelRow().getBoundingClientRect().width
  const conversationWidth = () => rowWidth() - panelWidth()
  const sidebarColumn = () => element.querySelector('aside')!.parentElement!.parentElement as HTMLElement
  const sidebarWidth = () => sidebarColumn().getBoundingClientRect().width
  const sidebarX = () => Math.round(sidebarColumn().getBoundingClientRect().left)
  const sidebarPreference = () => localStorage.getItem('jaz.sidebar')
  const nextFrames = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  const keptThreadList = async (action: string) => {
    await nextFrames()
    if (sidebarWidth() !== 264 || sidebarPreference() !== 'open') {
      throw new Error(`${action} changed the thread list: ${sidebarWidth()}px wide, preference ${sidebarPreference()}`)
    }
    if (conversationWidth() < PANEL_MIN_THREAD_WIDTH) {
      throw new Error(`${action} squeezed the conversation to ${conversationWidth()}px`)
    }
  }
  const animatePanel = async (selector: string, destination: number) => {
    const samples: { width: number; left: number; time: number }[] = []
    let frame = 0
    const record = () => {
      const bounds = drawerElement().getBoundingClientRect()
      samples.push({ width: bounds.width, left: bounds.left, time: performance.now() })
      frame = requestAnimationFrame(record)
    }
    record()
    try {
      await click(selector)
      await until(() => Math.abs(panelWidth() - destination) < 0.1)
      await new Promise(requestAnimationFrame)
    } finally {
      cancelAnimationFrame(frame)
    }
    const start = samples[0].width
    const distance = Math.abs(destination - start)
    const intermediate = samples.filter(({ width }) => Math.abs(width - start) > 2 && Math.abs(width - destination) > 2)
    // A busy main thread can leave a long gap between samples, which cannot show
    // whether the panel jumped inside it. Only adjacent frames prove a jump.
    const jumps = samples.slice(1).flatMap((sample, index) => sample.time - samples[index].time < 50 ? [Math.abs(sample.left - samples[index].left)] : [])
    if (intermediate.length < 3 || !jumps.length || Math.max(...jumps) > distance * 0.65) {
      console.warn(JSON.stringify({ panelFrames: samples }))
      throw new Error(`Panel snapped: ${start}px to ${destination}px, ${intermediate.length} intermediate frames, ${jumps.length ? Math.max(...jumps) : 'no'} observed edge jump`)
    }
    console.warn(JSON.stringify({ panelAnimation: { start, destination, frames: samples.length, intermediate: intermediate.length, maxEdgeJump: Math.max(...jumps) } }))
  }
  const previous = { sidebar: localStorage.getItem('jaz.sidebar'), width: localStorage.getItem('jaz.sidebarWidth') }
  try {
    localStorage.setItem('jaz.sidebar', 'open')
    localStorage.removeItem('jaz.sidebarWidth')
    await window.smoke.resize(1440, 900)
    await until(() => window.innerWidth === 1440)
    root.render(<QueryClientProvider client={queryClient}><BrowserWorkspace><SidePanelStateProvider><RouterProvider router={router} /></SidePanelStateProvider></BrowserWorkspace></QueryClientProvider>)
    await until(() => Boolean(element.querySelector('[title="Open Side Panel"]')) && sidebarWidth() === 264)
    const openWidth = panelWidthFor(rowWidth())
    await animatePanel('[title="Open Side Panel"]', openWidth)
    await window.smoke.capture('browser-preview-opening')
    await keptThreadList('Opening the side panel')
    await animatePanel('[title="Hide Side Panel"]', 0)
    await keptThreadList('Closing the side panel')
    await animatePanel('[title="Open Side Panel"]', openWidth)
    await window.smoke.capture('browser-preview-reopening')
    await animatePanel('[title="Open Overview"]', OVERVIEW_PANEL_WIDTH)
    await keptThreadList('Opening Overview')
    await animatePanel('[title="Open Side Panel"]', openWidth)
    // The shortcut opens and closes the panel without touching the list.
    await window.smoke.key('S', ['meta', 'shift'])
    await until(() => !panelShown())
    await keptThreadList('Command-Shift-S closing the panel')
    await window.smoke.key('S', ['meta', 'shift'])
    await until(() => panelShown() && Math.abs(panelWidth() - openWidth) < 1)
    await keptThreadList('Command-Shift-S opening the panel')
    // The panel and the list share the row: the panel gives way as the list
    // opens and takes its preferred width again as it closes.
    const drawer = element.querySelector<HTMLElement>('[role="separator"][aria-label="Resize side panel"]')!.parentElement!
    const panelCard = drawer.lastElementChild!.firstElementChild!
    const browserHost = element.querySelector<HTMLElement>('[data-browser-session="layout"]')!
    const samples: { sidebar: number; drawer: number; expected: number; browserRight: number; browserLeft: number; cardRight: number; cardLeft: number }[] = []
    const record = () => {
      const card = panelCard.getBoundingClientRect()
      const browser = browserHost.getBoundingClientRect()
      samples.push({
        sidebar: sidebarWidth(),
        drawer: drawer.getBoundingClientRect().width,
        expected: panelWidthFor(rowWidth()),
        browserRight: browser.right,
        browserLeft: browser.left,
        cardRight: card.right,
        cardLeft: card.left,
      })
    }
    const observer = new ResizeObserver(record)
    observer.observe(sidebarColumn())
    observer.observe(drawer)
    try {
      for (const expected of [0, 264, 0, 264]) {
        await click('[aria-label="Hide sidebar"], [aria-label="Show sidebar"]')
        await until(() => sidebarWidth() === expected && Math.abs(panelWidth() - panelWidthFor(rowWidth())) < 1)
      }
    } finally {
      observer.disconnect()
    }
    const movingFrames = samples.filter((sample) => sample.sidebar > 1 && sample.sidebar < 263)
    const lag = Math.max(...samples.map((sample) => Math.abs(sample.drawer - sample.expected)))
    const edgeGap = Math.max(...samples.flatMap((sample) => [Math.abs(sample.browserRight - sample.cardRight), Math.abs(sample.browserLeft - sample.cardLeft)]))
    if (movingFrames.length < 8 || lag > 1 || edgeGap > 1) {
      throw new Error(`Browser resize was not synchronized: ${movingFrames.length} moving frames, ${lag}px width lag, ${edgeGap}px edge gap`)
    }
    console.warn(JSON.stringify({ layoutSynchronization: { movingFrames: movingFrames.length, maxWidthLag: lag, maxEdgeGap: edgeGap } }))
    await window.smoke.capture('browser-navigation-open')
    await keptThreadList('Showing and hiding the thread list')
    // Resizing the panel by drag and by keyboard stays inside the bounds the
    // handle publishes to assistive technology.
    const divider = element.querySelector<HTMLElement>('[role="separator"][aria-label="Resize side panel"]')!
    const bounds = () => ({
      min: Number(divider.getAttribute('aria-valuemin')),
      max: Number(divider.getAttribute('aria-valuemax')),
      width: Number(divider.getAttribute('aria-valuenow')),
    })
    const clamped = (width: number) => {
      const { min, max } = bounds()
      return Math.round(Math.min(Math.max(width, min), max))
    }
    const grip = divider.getBoundingClientRect()
    if (getComputedStyle(divider.lastElementChild!).backgroundColor !== 'rgba(0, 0, 0, 0)') {
      throw new Error('The resize border should be hidden at rest')
    }
    const x = Math.round(grip.x + grip.width / 2)
    const y = Math.round(grip.y + grip.height / 2)
    if (!divider.contains(document.elementFromPoint(x, y))) {
      throw new Error('The browser covers the resize handle')
    }
    await window.smoke.pointer('mouseMove', x, y)
    await until(() => getComputedStyle(divider.lastElementChild!).backgroundColor !== 'rgba(0, 0, 0, 0)')
    // A synthetic move can be dropped between frames; re-issuing it is the same
    // input the user's pointer produces, and the width still has to arrive.
    const dragTo = async (toX: number, expected: number) => {
      for (let attempt = 0; attempt < 10; attempt += 1) {
        await window.smoke.pointer('mouseMove', toX, y)
        const end = Date.now() + 200
        while (Date.now() < end) {
          if (Math.abs(panelWidth() - expected) < 1) return
          await new Promise((resolve) => setTimeout(resolve, 20))
        }
      }
      throw new Error(`Dragging to ${toX}px left the panel at ${panelWidth()}px instead of ${expected}px`)
    }
    const start = bounds().width
    await window.smoke.pointer('mouseDown', x, y)
    await dragTo(x + 64, clamped(start - 64))
    await dragTo(x - 100, clamped(start + 100))
    await window.smoke.pointer('mouseUp', x - 100, y)
    await until(() => Math.abs(panelWidth() - clamped(start + 100)) < 1)
    if (document.body.style.cursor || document.body.style.userSelect) {
      throw new Error('Resize left the page in dragging state')
    }
    const dragged = clamped(start + 100)
    await click('[role="separator"][aria-label="Resize side panel"]')
    if (document.activeElement !== divider) {
      throw new Error('Clicking the divider did not focus it for keyboard resizing')
    }
    await window.smoke.key('Right')
    await until(() => Math.abs(panelWidth() - clamped(dragged - 24)) < 1)
    await window.smoke.key('Tab')
    await until(() => document.activeElement !== divider)
    await window.smoke.key('Tab', ['shift'])
    await window.smoke.capture('browser-divider-keyboard')
    await until(() => document.activeElement === divider && divider.matches(':focus-visible'))
    const focusedGrip = divider.getBoundingClientRect()
    const focusedX = Math.round(focusedGrip.x + focusedGrip.width / 2)
    await window.smoke.pointer('mouseMove', focusedX, y)
    await window.smoke.pointer('mouseDown', focusedX, y)
    await dragTo(focusedX - 24, dragged)
    await window.smoke.capture('browser-divider-drag')
    if (getComputedStyle(divider).outlineStyle !== 'none') {
      throw new Error(`The resize handle draws a second line: ${getComputedStyle(divider).outline}`)
    }
    await window.smoke.pointer('mouseUp', focusedX - 24, y)
    await window.smoke.key('Right')
    await until(() => Math.abs(panelWidth() - clamped(dragged - 24)) < 1)
    await keptThreadList('Resizing the side panel')
    setThemePref('dark')
    await new Promise((resolve) => setTimeout(resolve, 250))
    await window.smoke.capture('browser-layout-dark')
    setThemePref('light')
    await new Promise((resolve) => setTimeout(resolve, 250))
    await window.smoke.capture('browser-layout-light')
    // At the desktop window's minimum width the panel shrinks to keep the
    // conversation's 360 pixels; the thread list stays where the user put it.
    await window.smoke.resize(940, 850)
    await until(() => window.innerWidth === 940)
    await until(() => Math.abs(conversationWidth() - PANEL_MIN_THREAD_WIDTH) < 1)
    await keptThreadList('Narrowing the window to the desktop minimum')
    if (panelWidth() < 240) {
      throw new Error(`The narrow window left the panel ${panelWidth()}px wide`)
    }
    await window.smoke.capture('browser-layout-narrow')
    // A phone stacks both: opening the panel dismisses the full-screen drawer.
    await window.smoke.resize(390, 844)
    await until(() => window.innerWidth === 390)
    if (panelShown()) {
      await window.smoke.key('S', ['meta', 'shift'])
      await until(() => !panelShown())
    }
    await until(() => sidebarX() < 0)
    await click('[aria-label="Show sidebar"]')
    await until(() => sidebarX() === 0)
    await window.smoke.key('S', ['meta', 'shift'])
    await until(() => panelShown())
    await until(() => sidebarX() < 0)
    await window.smoke.capture('browser-layout-phone')
  } catch (error) {
    await window.smoke.capture('browser-layout-failure')
    throw new Error(`${(error as Error).message}; viewport=${window.innerWidth}, panel=${panelWidth()}, row=${rowWidth()}, sidebar=${sidebarWidth()}`, { cause: error })
  } finally {
    root.unmount()
    queryClient.clear()
    element.remove()
    for (const [key, value] of [['jaz.sidebar', previous.sidebar], ['jaz.sidebarWidth', previous.width]] as const) {
      if (value === null) {
        localStorage.removeItem(key)
      } else {
        localStorage.setItem(key, value)
      }
    }
    await window.smoke.resize(1050, 850)
  }
}
