// Placeholder screen used by every route in routes.tsx until the issue
// that owns it (see the comment on each route) fills it in. Deliberately
// dumb: it renders static copy only, and must never grow logic that
// belongs in a later issue's real screen — see the routing skeleton's own
// doc comment in routes.tsx.
type PlaceholderProps = {
  title: string
  description: string
}

export function Placeholder({ title, description }: PlaceholderProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-12 text-center">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="text-muted-foreground max-w-md text-sm text-balance">
        {description}
      </p>
    </div>
  )
}
