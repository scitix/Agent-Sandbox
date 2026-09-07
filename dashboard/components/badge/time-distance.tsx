import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { ClockIcon } from 'lucide-react'
import { useEffect, useState } from 'react'

// How precise the hover tooltip's absolute timestamp is. The visible relative
// label (e.g. "5 minutes ago") is unaffected; only the tooltip changes.
// Defaults to minute — trace/reservation views pass 'millisecond' so sub-second
// events keep their ordering on hover.
type TooltipPrecision = 'minute' | 'second' | 'millisecond'

// date-fns format strings keep the locale-aware full date ('PPP') and vary only
// the time token: 'p' (minute), 'pp' (seconds), 'pp.SSS' (milliseconds).
const TOOLTIP_FORMAT: Record<TooltipPrecision, string> = {
  minute: 'PPPp',
  second: 'PPPpp',
  millisecond: 'PPPpp.SSS',
}

export const TimeDistance = ({
  date,
  withIcon = true,
  className,
  tooltipPrecision = 'minute',
}: {
  date?: string | Date | null
  withIcon?: boolean
  className?: string
  tooltipPrecision?: TooltipPrecision
}) => {
  const [timeDiff, setTimeDiff] = useState('')
  const [startTime, setStartTime] = useState<Date | null>(null)

  useEffect(() => {
    if (!date) {
      // A ticking clock and an element observer are external systems; the
      // state they push is not derivable at render time.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setStartTime(null)
      setTimeDiff('')
      return
    }

    const time = new Date(date)
    setStartTime(time)

    const updateTimeDiff = () => {
      const timeDifference = formatDistanceToNow(time, {
        locale: zhCN,
        addSuffix: true,
      })
      setTimeDiff(timeDifference.replace(/^[^\d]*\s*/, ''))
    }

    updateTimeDiff()
    const timer = setInterval(updateTimeDiff, 10000)
    return () => clearInterval(timer)
  }, [date])

  if (!startTime) {
    return null
  }

  return (
    <TooltipProvider delay={10}>
      <Tooltip>
        <TooltipTrigger
          className={cn(
            'flex cursor-help items-center gap-1 text-xs',
            className
          )}
        >
          {withIcon && <ClockIcon className="text-muted-foreground size-4" />}
          {timeDiff}
        </TooltipTrigger>
        <TooltipContent>
          {format(startTime, TOOLTIP_FORMAT[tooltipPrecision], {
            locale: zhCN,
          })}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
