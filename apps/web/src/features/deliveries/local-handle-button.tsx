/** 「在本地处理此阶段」按钮：直连本机 infera-link 守护进程拉起 CLI（R4） */
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Terminal } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { handleLocally } from '@/features/deliveries/local-link'

export function LocalHandleButton({ deliveryId }: { deliveryId: string }) {
  const [done, setDone] = useState(false)
  const handle = useMutation({
    mutationFn: () => handleLocally(deliveryId),
    onSuccess: (res) => {
      setDone(true)
      toast.success(
        `已在本机拉起 ${res.cli}（${res.node}）——完成后经 MCP 自动交回，流水线继续`
      )
    },
    onError: (e: Error) => toast.error(e.message),
  })
  return (
    <Button
      size='lg'
      variant='outline'
      disabled={handle.isPending}
      onClick={() => handle.mutate()}
    >
      <Terminal />
      {handle.isPending ? '拉起中…' : done ? '再次拉起' : '在本地处理此阶段'}
    </Button>
  )
}
