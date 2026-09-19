import { BellRingIcon, CircleAlertIcon, CircleCheckIcon, TrendingUpIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardAction, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { formatCents } from "@/lib/format"
import type { Overview } from "@/lib/types"

/** 概览统计卡片，结构来自 shadcn/ui 的 dashboard-01 区块。 */
export function SectionCards({ data }: { data: Overview }) {
  const { summary, system } = data
  const conversion = summary.todayOrders ? Math.round((summary.todayPaid / summary.todayOrders) * 100) : 0

  return (
    <div className="grid grid-cols-1 gap-4 *:data-[slot=card]:bg-gradient-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>今日成交额</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {formatCents(summary.todayAmount)}
          </CardTitle>
          <CardAction>
            <Badge variant="outline">
              <TrendingUpIcon />
              {summary.todayPaid} 笔
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">今日下单 {summary.todayOrders} 笔</div>
          <div className="text-muted-foreground">支付转化率 {conversion}%</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>累计成交额</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {formatCents(summary.totalAmount)}
          </CardTitle>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">全部已支付订单</div>
          <div className="text-muted-foreground">按订单金额（人民币）统计</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>可用支付渠道</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {system.activeChannels} / {system.channels}
          </CardTitle>
          <CardAction>
            {system.failedChannels > 0 ? (
              <Badge variant="destructive">
                <CircleAlertIcon />
                {system.failedChannels} 个异常
              </Badge>
            ) : (
              <Badge variant="outline">
                <CircleCheckIcon />
                正常
              </Badge>
            )}
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">已接入 {system.merchants} 个商户</div>
          <div className="text-muted-foreground">支持 {system.drivers} 种渠道驱动</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>商户通知</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {summary.notifyPending} 待投递
          </CardTitle>
          <CardAction>
            <Badge variant={summary.notifyFailed > 0 ? "destructive" : "outline"}>
              <BellRingIcon />
              {summary.notifyFailed} 失败
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">失败的通知可在订单详情中补发</div>
          <div className="text-muted-foreground">自动重试 12 次，跨度约 25 小时</div>
        </CardFooter>
      </Card>
    </div>
  )
}
