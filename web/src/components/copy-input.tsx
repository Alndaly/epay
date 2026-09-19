import { useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { toast } from "sonner"

import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group"

/** 只读输入框 + 复制按钮，用于展示回调地址、商户密钥等需要复制到其他系统的值。 */
export function CopyInput({
  value,
  id,
  ...props
}: { value: string; id?: string } & React.ComponentProps<typeof InputGroup>) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error("复制失败，请手动选择复制")
    }
  }

  return (
    <InputGroup {...props}>
      <InputGroupInput id={id} value={value} readOnly onFocus={(e) => e.currentTarget.select()} />
      <InputGroupAddon align="inline-end">
        <InputGroupButton size="icon-xs" onClick={copy} aria-label="复制">
          {copied ? <CheckIcon /> : <CopyIcon />}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  )
}
