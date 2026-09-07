import { useTranslation } from '@/lib/i18n'
import { ReactNode, useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'

type UseCopyToClipboardProps = {
  text: string
  copyMessage?: string
}

export function useCopyToClipboard({
  text,
  copyMessage,
}: UseCopyToClipboardProps) {
  const { t } = useTranslation()
  const [isCopied, setIsCopied] = useState(false)
  const timeoutRef = useRef<NodeJS.Timeout | null>(null)

  const handleCopy = useCallback(() => {
    navigator.clipboard
      .writeText(text)
      .then(() => {
        toast.success(copyMessage ?? t('layout.copy.success'))
        setIsCopied(true)
        if (timeoutRef.current) {
          clearTimeout(timeoutRef.current)
          timeoutRef.current = null
        }
        timeoutRef.current = setTimeout(() => {
          setIsCopied(false)
        }, 2000)
      })
      .catch(() => {
        toast.error(t('layout.copy.failed'))
      })
  }, [text, copyMessage, t])

  return { isCopied, handleCopy }
}

export function useCopyToClipboardWithText() {
  const { t } = useTranslation()
  const [isCopied, setIsCopied] = useState(false)
  const timeoutRef = useRef<NodeJS.Timeout | null>(null)

  const handleCopyWithText = useCallback(
    (newText: string, copyMessage?: ReactNode) => {
      navigator.clipboard
        .writeText(newText)
        .then(() => {
          toast.success(t('layout.copy.success'), {
            description: copyMessage,
          })
          setIsCopied(true)
          if (timeoutRef.current) {
            clearTimeout(timeoutRef.current)
            timeoutRef.current = null
          }
          timeoutRef.current = setTimeout(() => {
            setIsCopied(false)
          }, 2000)
        })
        .catch(() => {
          toast.error(t('layout.copy.failed'))
        })
    },
    [t]
  )

  return { isCopied, handleCopyWithText }
}
