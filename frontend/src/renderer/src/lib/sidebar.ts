import { createContext } from 'react'

// A phone's sidebar is a full-screen drawer, so opening a thread's side panel
// dismisses it rather than stacking two overlays. Desktop has no provider: the
// thread list and the side panel share the window side by side.
export const DismissSidebarDrawer = createContext<(() => void) | null>(null)
