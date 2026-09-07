// i18n-processed-v1.1.0 (no translatable strings)
import {
  Button,
  buttonVariants,
} from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { VariantProps } from 'class-variance-authority'
import React, { ReactNode } from 'react'

type TooltipButtonProps = React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    tooltip: ReactNode
    side?: React.ComponentProps<typeof TooltipContent>['side']
  }

const TooltipButton = function LoadableButton({
  ref,
  className,
  variant,
  size,
  tooltip,
  side,
  children,
  ...props
}: TooltipButtonProps) {
  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant={variant}
              size={size}
              className={cn(buttonVariants({ variant, size, className }))}
              ref={ref}
              {...props}
            />
          }
        >
          {children}
        </TooltipTrigger>
        <TooltipContent side={side}>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

TooltipButton.displayName = 'TooltipButton'
export default TooltipButton
