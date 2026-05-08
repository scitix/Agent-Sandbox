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

import { VariantProps } from "class-variance-authority"
import React, { ReactNode } from "react"

import { Button, buttonVariants } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

import { cn } from "@/lib/utils"

type TooltipButtonProps = React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
    tooltip: ReactNode
    side?: React.ComponentProps<typeof TooltipContent>["side"]
  }

const TooltipButton = function TooltipButton({
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
              ref={ref as React.Ref<HTMLButtonElement>}
              {...(props as React.ComponentProps<typeof Button>)}
            >
              {children}
            </Button>
          }
        />
        <TooltipContent side={side}>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

TooltipButton.displayName = "TooltipButton"
export default TooltipButton
