import { Link } from "react-router"
import { CircleAlertIcon } from "lucide-react"

import { ChartAreaInteractive } from "@/components/chart-area-interactive"
import { CopyInput } from "@/components/copy-input"
import { QueryState } from "@/components/query-state"
import { SectionCards } from "@/components/section-cards"
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { useOverview } from "@/hooks/queries"

export function OverviewPage() {
  const { data, error, refetch } = useOverview()
  if (!data) return <QueryState error={error} onRetry={refetch} />

  const { system, summary } = data
  return (
    <>
      {system.failedChannels > 0 && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>有 {system.failedChannels} 个支付渠道初始化失败</AlertTitle>
          <AlertDescription>这些渠道当前无法下单，请检查密钥等配置。</AlertDescription>
          <AlertAction>
            <Button size="sm" variant="outline" asChild>
              <Link to="/channels">查看渠道</Link>
            </Button>
          </AlertAction>
        </Alert>
      )}
      {summary.notifyFailed > 0 && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>有 {summary.notifyFailed} 笔订单通知商户失败</AlertTitle>
          <AlertDescription>商户可能没有正确处理回调，确认商户恢复后可手动补发通知。</AlertDescription>
          <AlertAction>
            <Button size="sm" variant="outline" asChild>
              <Link to="/orders?notify=failed">查看订单</Link>
            </Button>
          </AlertAction>
        </Alert>
      )}
      <SectionCards data={data} />
      <ChartAreaInteractive daily={summary.daily} />
      <Card>
        <CardHeader>
          <CardTitle>接入信息</CardTitle>
          <CardDescription>在 new-api「支付设置」中填写支付地址，并在「商户」页面获取商户 ID 与密钥。</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="base-url">支付地址（new-api 填这个）</FieldLabel>
              <CopyInput id="base-url" value={system.baseUrl} />
              <FieldDescription>new-api 会自动拼接 /submit.php，请勿重复填写。</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="mapi-url">API 下单地址</FieldLabel>
              <CopyInput id="mapi-url" value={system.mapiUrl} />
            </Field>
            <Field>
              <FieldLabel htmlFor="api-url">查询 / 退款地址</FieldLabel>
              <CopyInput id="api-url" value={system.apiUrl} />
              <FieldDescription>当前版本 {system.version}</FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>
    </>
  )
}
