import type { ReactNode } from "react"
import { Link, useParams } from "react-router"
import { ArrowLeftIcon, CircleAlertIcon, RefreshCwIcon, SendIcon } from "lucide-react"

import { CopyInput } from "@/components/copy-input"
import { QueryState } from "@/components/query-state"
import { NotifyStatusBadge, OrderStatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableRow } from "@/components/ui/table"
import { useOrder, useOrderAction } from "@/hooks/queries"
import { formatTime } from "@/lib/format"

/** 键值列表：每行「表头单元格 + 数据单元格」，值为空时显示占位符。 */
function Rows({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <Table>
      <TableBody>
        {rows.map(([label, value]) => (
          <TableRow key={label}>
            <TableHead>{label}</TableHead>
            <TableCell>{value || "—"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function OrderDetailPage() {
  const { tradeNo = "" } = useParams()
  const { data: o, error, refetch } = useOrder(tradeNo)
  const renotify = useOrderAction("renotify")
  const sync = useOrderAction("sync")

  if (!o) return <QueryState error={error} onRetry={refetch} />

  const foreign = o.payCurrency && o.payCurrency !== "CNY"
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{o.name}</CardTitle>
          <CardDescription>平台订单号 {o.tradeNo}</CardDescription>
          <CardAction>
            <ButtonGroup>
              <Button variant="outline" asChild>
                <Link to="/orders">
                  <ArrowLeftIcon />
                  返回列表
                </Link>
              </Button>
              {o.status !== "paid" && (
                <Button variant="outline" disabled={sync.isPending} onClick={() => sync.mutate(o.tradeNo)}>
                  {sync.isPending ? <Spinner /> : <RefreshCwIcon />}
                  向上游查询
                </Button>
              )}
              {o.status === "paid" && (
                <Button disabled={renotify.isPending} onClick={() => renotify.mutate(o.tradeNo)}>
                  {renotify.isPending ? <Spinner /> : <SendIcon />}
                  补发通知
                </Button>
              )}
            </ButtonGroup>
          </CardAction>
        </CardHeader>
        <CardContent>
          <Rows
            rows={[
              ["支付状态", <OrderStatusBadge key="status" status={o.status} />],
              ["订单金额", `¥${o.money}`],
              ["实付金额", foreign ? `${o.payCurrency} ${o.payAmount}` : o.payCurrency && `¥${o.payAmount}`],
              ["已退款", o.refundMoney !== "0.00" && `¥${o.refundMoney}`],
              ["商户", o.pid],
              ["商户订单号", o.outTradeNo],
              ["支付方式", o.type],
              ["上游交易号", o.apiTradeNo],
              ["付款人", o.buyer],
              ["自定义参数", o.param],
              ["买家 IP", o.clientIp],
              ["创建时间", formatTime(o.createdAt)],
              ["支付时间", o.paidAt && formatTime(o.paidAt)],
              ["过期时间", o.status !== "paid" && formatTime(o.expireAt)],
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>商户通知</CardTitle>
          <CardDescription>支付成功后以 GET 请求商户 notify_url，商户返回 success 视为成功。</CardDescription>
          <CardAction>
            <NotifyStatusBadge status={o.notifyStatus} />
          </CardAction>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            {o.notifyError && (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>最近一次通知失败</AlertTitle>
                <AlertDescription>{o.notifyError}</AlertDescription>
              </Alert>
            )}
            <Rows
              rows={[
                ["已投递次数", String(o.notifyCount)],
                ["下次重试", o.nextNotifyAt && formatTime(o.nextNotifyAt)],
              ]}
            />
            <Field>
              <FieldLabel htmlFor="notify-url">异步通知地址</FieldLabel>
              <CopyInput id="notify-url" value={o.notifyUrl} />
            </Field>
            {o.returnUrl && (
              <Field>
                <FieldLabel htmlFor="return-url">同步跳转地址</FieldLabel>
                <CopyInput id="return-url" value={o.returnUrl} />
              </Field>
            )}
          </FieldGroup>
        </CardContent>
      </Card>
    </>
  )
}
