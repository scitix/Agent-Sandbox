// The app's search box: a leading magnifier, the field, and a clear button that
// only exists while there is something to clear.
//
// It lives here rather than being re-assembled per call site because the three
// pieces have to agree on geometry — the icon is absolutely positioned over the
// input, so its inset and the input's padding are one measurement, and every
// hand-rolled copy has got that slightly wrong in a different direction.
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { SearchIcon, XIcon } from 'lucide-react'

export function SearchInput({
  value,
  onChange,
  onClear,
  placeholder,
  className,
  /** The field's own fill. A toolbar sits on the page canvas and wants the panel
   *  colour; a panel-coloured surface wants the canvas colour back. */
  fill = 'card',
}: {
  value: string
  onChange: (value: string) => void
  onClear?: () => void
  placeholder?: string
  className?: string
  fill?: 'card' | 'background'
}) {
  const { t } = useTranslation()
  const clear = () => {
    onChange('')
    onClear?.()
  }
  return (
    <div className={cn('relative h-9', className)}>
      <SearchIcon className="text-muted-foreground pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2" />
      <Input
        placeholder={placeholder}
        value={value}
        onChange={e => onChange(e.target.value)}
        className={cn(
          'h-9 w-full pl-9 pr-8 text-xs md:text-xs',
          fill === 'card' ? 'bg-card' : 'bg-background'
        )}
      />
      <Button
        variant="ghost"
        size="icon"
        className="absolute right-2 top-1/2 size-5 -translate-y-1/2 rounded-full"
        onClick={clear}
        hidden={!value}
        aria-label={t('table.clear')}
      >
        <XIcon className="size-3" />
      </Button>
    </div>
  )
}
