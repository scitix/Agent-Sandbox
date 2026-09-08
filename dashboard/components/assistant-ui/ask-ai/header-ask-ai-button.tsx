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

"use client"

/**
 * The header's assistant button.
 *
 * Opens the side panel rather than navigating: the reason to ask from a page is
 * that you want to keep looking at it. Navigating to the dedicated page is what
 * the sidebar entry is for.
 */

import type { FC } from "react"
import { usePathname } from "next/navigation"
import { useAtom } from "jotai"
import { Sparkles } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"
import { atomAssistantOpen } from "@/lib/assistant/store"

export const HeaderAskAIButton: FC = () => {
  const { t } = useTranslation()
  const [open, setOpen] = useAtom(atomAssistantOpen)
  const pathname = usePathname()
  // The page IS the assistant. The panel is suppressed there, so leaving the
  // button would offer an action with no effect — worse than not offering it.
  if (/(^|\/)assistant\/?$/.test(pathname)) return null
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant={open ? "secondary" : "ghost"}
            size="icon"
            className={cn("size-8", open && "text-primary")}
            onClick={() => setOpen(!open)}
            aria-label={t("assistant.title")}
            aria-pressed={open}
          >
            <Sparkles className="size-4" />
          </Button>
        }
      />
      <TooltipContent>{t("assistant.askHere")}</TooltipContent>
    </Tooltip>
  )
}
