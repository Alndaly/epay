import { ZapIcon } from "lucide-react"

import { LoginForm } from "@/components/login-form"

/** 登录页，结构来自 shadcn/ui 的 login-03 区块。 */
export function LoginPage() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center gap-2 self-center font-medium">
          <div className="flex size-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <ZapIcon className="size-4" />
          </div>
          统一支付网关
        </div>
        <LoginForm />
      </div>
    </div>
  )
}
