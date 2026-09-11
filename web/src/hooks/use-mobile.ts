import * as React from 'react'

const MOBILE_BREAKPOINT = 768

export function useIsMobile() {
  const [isMobile, setIsMobile] = React.useState<boolean | undefined>(undefined)

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`)
    const onChange = () => {
      setIsMobile(window.innerWidth < MOBILE_BREAKPOINT)
    }
    mql.addEventListener('change', onChange)
    // Vendored shadcn hook (npx shadcn add, see docs/contributing.md) —
    // this is the upstream implementation subscribing to matchMedia, a
    // genuine external system; the initial synchronous read just seeds
    // state with the current value before the first 'change' fires.
    // oxlint-disable-next-line react/set-state-in-effect
    setIsMobile(window.innerWidth < MOBILE_BREAKPOINT)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  return !!isMobile
}
