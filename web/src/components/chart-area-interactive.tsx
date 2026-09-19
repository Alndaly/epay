import * as React from "react"
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts"

import { useIsMobile } from "@/hooks/use-mobile"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { formatCents } from "@/lib/format"
import type { DailyStat } from "@/lib/types"

const chartConfig = {
  amount: {
    label: "成交额",
    color: "var(--primary)",
  },
} satisfies ChartConfig

const formatDate = (value: unknown) =>
  new Date(String(value)).toLocaleDateString("zh-CN", { month: "short", day: "numeric" })

/** 成交趋势图，结构来自 shadcn/ui 的 dashboard-01 区块。 */
export function ChartAreaInteractive({ daily }: { daily: DailyStat[] }) {
  const isMobile = useIsMobile()
  // 用户未手动选择时，移动端默认展示 7 天、桌面端 30 天（在渲染时推导，无需 effect）
  const [selected, setTimeRange] = React.useState<string | null>(null)
  const timeRange = selected ?? (isMobile ? "7d" : "30d")

  const days = timeRange === "7d" ? 7 : 30
  const data = daily.slice(-days)
  const total = data.reduce((sum, d) => sum + d.amount, 0)

  return (
    <Card className="@container/card">
      <CardHeader>
        <CardTitle>成交趋势</CardTitle>
        <CardDescription>
          最近 {days} 天成交 {formatCents(total)}
        </CardDescription>
        <CardAction>
          <ToggleGroup
            type="single"
            value={timeRange}
            onValueChange={(v) => v && setTimeRange(v)}
            variant="outline"
            className="hidden *:data-[slot=toggle-group-item]:px-4! @[767px]/card:flex"
          >
            <ToggleGroupItem value="30d">最近 30 天</ToggleGroupItem>
            <ToggleGroupItem value="7d">最近 7 天</ToggleGroupItem>
          </ToggleGroup>
          <Select value={timeRange} onValueChange={setTimeRange}>
            <SelectTrigger
              className="flex w-40 **:data-[slot=select-value]:block **:data-[slot=select-value]:truncate @[767px]/card:hidden"
              size="sm"
              aria-label="选择时间范围"
            >
              <SelectValue placeholder="最近 30 天" />
            </SelectTrigger>
            <SelectContent className="rounded-xl">
              <SelectItem value="30d" className="rounded-lg">
                最近 30 天
              </SelectItem>
              <SelectItem value="7d" className="rounded-lg">
                最近 7 天
              </SelectItem>
            </SelectContent>
          </Select>
        </CardAction>
      </CardHeader>
      <CardContent className="px-2 pt-4 sm:px-6 sm:pt-6">
        <ChartContainer config={chartConfig} className="aspect-auto h-[250px] w-full">
          <AreaChart data={data.map((d) => ({ ...d, amount: d.amount / 100 }))}>
            <defs>
              <linearGradient id="fillAmount" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="var(--color-amount)" stopOpacity={1.0} />
                <stop offset="95%" stopColor="var(--color-amount)" stopOpacity={0.1} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="date"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              minTickGap={32}
              tickFormatter={formatDate}
            />
            <ChartTooltip
              cursor={false}
              content={<ChartTooltipContent labelFormatter={formatDate} indicator="dot" />}
            />
            <Area dataKey="amount" type="monotone" fill="url(#fillAmount)" stroke="var(--color-amount)" />
          </AreaChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
