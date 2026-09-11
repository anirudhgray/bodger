import { isRouteErrorResponse, useRouteError } from 'react-router-dom'

import { Placeholder } from '@/pages/Placeholder'

// RouteError is react-router-dom's errorElement for the whole tree (see
// routes.tsx): it replaces the library's own default crash screen (which
// cites its own internals — "provide your own ErrorBoundary or
// errorElement prop", not something a bodger user should ever see) with
// the same Placeholder styling every other screen uses.
// isRouteErrorResponse distinguishes a thrown Response (a route the
// router itself couldn't resolve — a raw fetch or navigation failure, not
// an actual path in routes.tsx) from a real thrown error in a component.
//
// Kept in its own file, not inlined in routes.tsx, so that file only
// exports the non-component route tree/router (routes.test.tsx imports
// this directly to mount it against a route that deliberately throws).
export function RouteError() {
  const error = useRouteError()
  const description = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : 'Reload the page, or try again in a moment.'
  return <Placeholder title="Something went wrong" description={description} />
}
