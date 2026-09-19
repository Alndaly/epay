import { useState } from "react"
import { EyeIcon, KeyRoundIcon, PlusIcon, StoreIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { CopyInput } from "@/components/copy-input"
import { QueryState } from "@/components/query-state"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useCreateMerchant, useDeleteMerchant, useMerchants, useOverview, useUpdateMerchant } from "@/hooks/queries"
import { api } from "@/lib/api"
import { formatTime } from "@/lib/format"
import type { Merchant } from "@/lib/types"

/** 正在展示密钥的商户（新建、查看或重置密钥后）。 */
interface Credentials {
  pid: string
  key: string
  title: string
}

export function MerchantsPage() {
  const { data, error, refetch } = useMerchants()
  const update = useUpdateMerchant()
  const [credentials, setCredentials] = useState<Credentials | null>(null)

  const showKey = async (m: Merchant) => {
    try {
      const { key } = await api.merchantKey(m.pid)
      setCredentials({ pid: m.pid, key, title: "商户密钥" })
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>商户</CardTitle>
        <CardDescription>
          每个接入方（如 new-api）对应一个商户，使用商户 ID 与密钥按易支付协议签名下单。
        </CardDescription>
        <CardAction>
          <CreateMerchantDialog onCreated={(m) => setCredentials({ pid: m.pid, key: m.key, title: "商户已创建" })} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {!data ? (
          <QueryState error={error} onRetry={refetch} />
        ) : data.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <StoreIcon />
              </EmptyMedia>
              <EmptyTitle>还没有商户</EmptyTitle>
              <EmptyDescription>新建商户后，把商户 ID 和密钥填入 new-api 即可完成对接。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>商户 ID</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>密钥</TableHead>
                <TableHead>启用</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((m) => (
                <TableRow key={m.pid}>
                  <TableCell>{m.pid}</TableCell>
                  <TableCell>{m.name || "—"}</TableCell>
                  <TableCell>{m.keyMasked}</TableCell>
                  <TableCell>
                    <Switch
                      checked={m.enabled}
                      disabled={update.isPending}
                      onCheckedChange={(enabled) => update.mutate({ pid: m.pid, name: m.name, enabled })}
                      aria-label={`${m.enabled ? "停用" : "启用"}商户 ${m.pid}`}
                    />
                  </TableCell>
                  <TableCell>{formatTime(m.createdAt)}</TableCell>
                  <TableCell>
                    <ButtonGroup>
                      <Button variant="outline" size="sm" onClick={() => showKey(m)}>
                        <EyeIcon />
                        查看密钥
                      </Button>
                      <ResetKeyButton
                        merchant={m}
                        onReset={(key) => (setCredentials({ pid: m.pid, key, title: "密钥已重置" }), refetch())}
                      />
                      <DeleteMerchantButton merchant={m} />
                    </ButtonGroup>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      <CredentialsDialog credentials={credentials} onClose={() => setCredentials(null)} />
    </Card>
  )
}

function CreateMerchantDialog({ onCreated }: { onCreated: (m: Merchant) => void }) {
  const [open, setOpen] = useState(false)
  const create = useCreateMerchant()

  const onSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    create.mutate(
      { pid: String(form.get("pid")), name: String(form.get("name")) },
      {
        onSuccess: (m) => {
          setOpen(false)
          onCreated(m)
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={(o) => (setOpen(o), create.reset())}>
      <DialogTrigger asChild>
        <Button>
          <PlusIcon />
          新建商户
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={onSubmit}>
          <FieldGroup>
            <DialogHeader>
              <DialogTitle>新建商户</DialogTitle>
              <DialogDescription>密钥将自动生成，创建后可随时查看或重置。</DialogDescription>
            </DialogHeader>
            {create.error && (
              <Alert variant="destructive">
                <AlertDescription>{create.error.message}</AlertDescription>
              </Alert>
            )}
            <Field>
              <FieldLabel htmlFor="merchant-name">名称</FieldLabel>
              <Input id="merchant-name" name="name" placeholder="new-api" autoFocus />
            </Field>
            <Field>
              <FieldLabel htmlFor="merchant-pid">商户 ID</FieldLabel>
              <Input id="merchant-pid" name="pid" placeholder="留空自动分配，如 1001" />
              <FieldDescription>字母、数字、下划线或短横线，创建后不可修改。</FieldDescription>
            </Field>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline" type="button">
                  取消
                </Button>
              </DialogClose>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending && <Spinner />}
                创建
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** 展示商户对接信息：支付地址、商户 ID、密钥，均可一键复制。 */
function CredentialsDialog({ credentials, onClose }: { credentials: Credentials | null; onClose: () => void }) {
  const overview = useOverview()
  return (
    <Dialog open={!!credentials} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{credentials?.title}</DialogTitle>
          <DialogDescription>在 new-api「系统设置 → 支付设置」中填写以下信息。</DialogDescription>
        </DialogHeader>
        {credentials && (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="cred-url">支付地址</FieldLabel>
              <CopyInput id="cred-url" value={overview.data?.system.baseUrl ?? ""} />
            </Field>
            <Field>
              <FieldLabel htmlFor="cred-pid">易支付商户 ID</FieldLabel>
              <CopyInput id="cred-pid" value={credentials.pid} />
            </Field>
            <Field>
              <FieldLabel htmlFor="cred-key">易支付商户密钥</FieldLabel>
              <CopyInput id="cred-key" value={credentials.key} />
              <FieldDescription>请妥善保管，泄露后可在商户列表中重置。</FieldDescription>
            </Field>
          </FieldGroup>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button>完成</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ResetKeyButton({ merchant, onReset }: { merchant: Merchant; onReset: (key: string) => void }) {
  const reset = async () => {
    try {
      const { key } = await api.resetMerchantKey(merchant.pid)
      onReset(key)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm">
          <KeyRoundIcon />
          重置密钥
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>重置商户 {merchant.pid} 的密钥？</AlertDialogTitle>
          <AlertDialogDescription>
            旧密钥立即失效，商户需要同步更新为新密钥，否则下单与回调验签都会失败。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction onClick={reset}>重置</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function DeleteMerchantButton({ merchant }: { merchant: Merchant }) {
  const remove = useDeleteMerchant()
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Trash2Icon />
          删除
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除商户 {merchant.pid}？</AlertDialogTitle>
          <AlertDialogDescription>
            删除后该商户无法再下单，已支付订单也将无法再通知该商户。如只是暂停接入，建议关闭「启用」开关。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => remove.mutate(merchant.pid)}>
            删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
