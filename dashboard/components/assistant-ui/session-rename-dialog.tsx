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

// Rename a conversation. One dialog, two callers — the header's title menu and
// the menu on a history row — because the two must not drift into different rules
// about what an acceptable title is.
//
// Renaming is a CAPABILITY: the port exposes `rename` only when the harness can do
// it (see backend-port), so a caller that has no rename function simply never
// offers the action. This component assumes the check already happened.
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useTranslation } from '@/lib/i18n'
import { useState } from 'react'

export function SessionRenameDialog({
  open,
  onOpenChange,
  currentTitle,
  onRename,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentTitle: string
  onRename: (title: string) => void | Promise<void>
}) {
  const { t } = useTranslation()
  const [value, setValue] = useState(currentTitle)

  // Re-seed each time it opens: the dialog outlives one use (it stays mounted
  // next to the trigger), and reopening it on a different conversation would
  // otherwise offer the previous one's name.
  //
  // Keyed on the open transition rather than written from an effect, so the
  // first render after opening already shows the right name instead of the
  // previous one for a frame.
  const [seeded, setSeeded] = useState<string | null>(null)
  if (open && seeded !== currentTitle) {
    setSeeded(currentTitle)
    setValue(currentTitle)
  }
  if (!open && seeded !== null) setSeeded(null)

  const submit = () => {
    const title = value.trim()
    // An empty title is not a rename, it is a way to lose the conversation in the
    // list. Keep the dialog open so the user can see why nothing happened.
    if (!title) return
    onOpenChange(false)
    void onRename(title)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t('assistant.renameSession')}</DialogTitle>
        </DialogHeader>
        <Input
          value={value}
          autoFocus
          onChange={e => setValue(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter') {
              e.preventDefault()
              submit()
            }
          }}
          placeholder={t('assistant.renamePlaceholder')}
        />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={submit} disabled={!value.trim()}>
            {t('common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
