import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { CircleAlertIcon, InfoIcon, SaveIcon } from "lucide-react"
import { toast } from "sonner"

import { CopyInput } from "@/components/copy-input"
import { DriverField } from "@/components/driver-field"
import { QueryState } from "@/components/query-state"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { useChannels, useDrivers, useOverview, useSaveChannel } from "@/hooks/queries"
import { initialValue } from "@/lib/channel-options"
import type { Channel, ChannelOptions, Driver } from "@/lib/types"

/** 新建 / 编辑支付渠道。/channels/new 为新建，/channels/:id 为编辑。 */
export function ChannelEditPage() {
  const { id } = useParams()
  const drivers = useDrivers()
  const channels = useChannels()
  const overview = useOverview()

  if (!drivers.data || (id && !channels.data)) {
    return (
      <QueryState error={drivers.error ?? channels.error} onRetry={() => (drivers.refetch(), channels.refetch())} />
    )
  }
  const existing = id ? channels.data?.find((c) => c.id === Number(id)) : undefined
  if (id && !existing) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>渠道不存在</EmptyTitle>
          <EmptyDescription>
            该渠道可能已被删除，<Link to="/channels">返回渠道列表</Link>。
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  // key 保证切换编辑对象时表单状态完全重置
  return (
    <ChannelForm key={id ?? "new"} drivers={drivers.data} existing={existing} baseUrl={overview.data?.system.baseUrl} />
  )
}

function defaultOptions(driver: Driver | undefined, saved: ChannelOptions = {}): ChannelOptions {
  const options: ChannelOptions = {}
  for (const f of driver?.fields ?? []) options[f.key] = initialValue(f, saved[f.key])
  return options
}

function ChannelForm({ drivers, existing, baseUrl }: { drivers: Driver[]; existing?: Channel; baseUrl?: string }) {
  const navigate = useNavigate()
  const save = useSaveChannel(existing?.id)

  const [driverName, setDriverName] = useState(existing?.driver ?? "")
  const driver = drivers.find((d) => d.name === driverName)
  const [type, setType] = useState(existing?.type ?? "")
  const [name, setName] = useState(existing?.name ?? "")
  const [enabled, setEnabled] = useState(existing?.enabled ?? true)
  const [options, setOptions] = useState<ChannelOptions>(() => defaultOptions(driver, existing?.options))

  // 新建时切换驱动：重置参数，并用驱动推荐值填充尚未手动修改的标识与名称
  const selectDriver = (next: string) => {
    const d = drivers.find((x) => x.name === next)
    const prev = drivers.find((x) => x.name === driverName)
    setDriverName(next)
    setOptions(defaultOptions(d))
    if (!type || type === prev?.defaultType) setType(d?.defaultType ?? "")
    if (!name || name === prev?.title) setName(d?.title ?? "")
  }

  const submit = () =>
    save.mutate(
      { type, driver: driverName, name, enabled, options },
      {
        onSuccess: (c) => {
          toast.success(`${c.name}已保存并生效`)
          navigate("/channels")
        },
      },
    )

  const notifyUrl = existing?.notifyUrl ?? (baseUrl && type ? `${baseUrl}/notify/${type}` : "")

  return (
    <>
      {existing?.error && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>渠道初始化失败</AlertTitle>
          <AlertDescription>{existing.error}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>基本信息</CardTitle>
          <CardDescription>{driver?.description ?? "选择要接入的上游支付渠道。"}</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="driver">渠道驱动 *</FieldLabel>
              <Select value={driverName} onValueChange={selectDriver} disabled={!!existing}>
                <SelectTrigger id="driver">
                  <SelectValue placeholder="请选择渠道驱动" />
                </SelectTrigger>
                <SelectContent>
                  {drivers.map((d) => (
                    <SelectItem key={d.name} value={d.name}>
                      {d.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {existing && <FieldDescription>驱动创建后不可修改，如需更换请新建渠道。</FieldDescription>}
            </Field>
            <Field>
              <FieldLabel htmlFor="type">支付方式标识 *</FieldLabel>
              <Input
                id="type"
                value={type}
                disabled={!!existing}
                placeholder="alipay"
                onChange={(e) => setType(e.target.value.toLowerCase())}
              />
              <FieldDescription>
                商户下单时传入的 type，需与 new-api「充值方式」中的 type
                一致；只能包含小写字母、数字和下划线，创建后不可修改。
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="name">显示名称</FieldLabel>
              <Input id="name" value={name} placeholder={driver?.title} onChange={(e) => setName(e.target.value)} />
              <FieldDescription>展示在收银台页面上。</FieldDescription>
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="enabled">启用</FieldLabel>
                <FieldDescription>停用后不再接受新订单，但仍会处理已有订单的支付回调。</FieldDescription>
              </FieldContent>
              <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      {driver && (
        <Card>
          <CardHeader>
            <CardTitle>{driver.title}参数</CardTitle>
            <CardDescription>
              密钥类字段既可以粘贴内容，也可以填写服务器上的文件路径。保存前会实际校验配置。
            </CardDescription>
          </CardHeader>
          <CardContent>
            {driver.fields?.length ? (
              <FieldGroup>
                {driver.fields.map((f) => (
                  <DriverField
                    key={f.key}
                    field={f}
                    value={options[f.key]}
                    onChange={(v) => setOptions((o) => ({ ...o, [f.key]: v }))}
                  />
                ))}
              </FieldGroup>
            ) : (
              <FieldDescription>该渠道无需配置参数。</FieldDescription>
            )}
          </CardContent>
        </Card>
      )}

      {driver && (
        <Card>
          <CardHeader>
            <CardTitle>回调地址</CardTitle>
            <CardDescription>
              {driver.webhook
                ? "该渠道需要在其后台手动配置回调（Webhook）地址。"
                : "下单时会自动把回调地址传给上游，无需手动配置。"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              {driver.webhook && (
                <Alert>
                  <InfoIcon />
                  <AlertTitle>配置说明</AlertTitle>
                  <AlertDescription>{driver.webhook}</AlertDescription>
                </Alert>
              )}
              <Field>
                <FieldLabel htmlFor="notify-url">上游回调地址</FieldLabel>
                {notifyUrl ? (
                  <CopyInput id="notify-url" value={notifyUrl} />
                ) : (
                  <FieldDescription>填写支付方式标识后生成。</FieldDescription>
                )}
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter>
            <FieldGroup>
              {save.error && (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>保存失败</AlertTitle>
                  <AlertDescription>{save.error.message}</AlertDescription>
                </Alert>
              )}
              <ButtonGroup>
                <Button onClick={submit} disabled={save.isPending || !type}>
                  {save.isPending ? <Spinner /> : <SaveIcon />}
                  保存并生效
                </Button>
                <Button variant="outline" asChild>
                  <Link to="/channels">取消</Link>
                </Button>
              </ButtonGroup>
            </FieldGroup>
          </CardFooter>
        </Card>
      )}
    </>
  )
}
