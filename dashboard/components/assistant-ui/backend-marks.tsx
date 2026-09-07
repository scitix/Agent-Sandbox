// The harness logos, in one place.
//
// Two components need them and they must agree: the toolbar picker (which
// harness a new conversation starts under) and the conversation list (which
// harness an existing one runs on, now that the list merges both).
import { ClaudeCode, OpenCode } from '@lobehub/icons'
import { cn } from '@/lib/utils'
import type { FC } from 'react'

interface Mark {
  label: string
  Icon: FC<{ size?: number }>
  /** Extra classes for the wrapper, for a logo that needs per-theme handling. */
  className?: string
}

/**
 * The harnesses we ship a logo for.
 *
 * OpenCode's monochrome mark paints itself with `currentColor`, so it already
 * follows the text colour of whatever it sits in and needs no help in either
 * theme. It used to carry `dark:invert` on the assumption that it was solid
 * black — which inverted the light-on-dark glyph back to black and made it
 * invisible in dark mode, exactly the failure the class was added to prevent.
 * Claude Code's is a fixed orange that reads on both, and inverting it would
 * turn it blue.
 *
 * A backend absent from this map is one the UI cannot draw, and both call sites
 * treat that as "do not offer it" rather than rendering a nameless entry.
 */
export const BACKEND_MARKS: Record<string, Mark> = {
  'claude-code': { label: 'Claude Code', Icon: ClaudeCode.Color },
  opencode: { label: 'OpenCode', Icon: OpenCode },
}

export function BackendMark({
  id,
  size = 16,
  className,
  labelled = false,
}: {
  id?: string
  size?: number
  className?: string
  /** Name the harness in a native tooltip. Off by default: where the mark sits
   *  next to its own label, or inside a control that already has a `title`, a
   *  second one just shadows the more useful message (a disabled entry's reason,
   *  say) whenever the pointer happens to be over the icon. */
  labelled?: boolean
}) {
  const mark = id ? BACKEND_MARKS[id] : undefined
  if (!mark) return null
  const { Icon, label } = mark
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center',
        mark.className,
        className
      )}
      {...(labelled ? { title: label } : {})}
    >
      <Icon size={size} />
    </span>
  )
}
