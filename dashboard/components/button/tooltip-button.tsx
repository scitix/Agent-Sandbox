/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

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
