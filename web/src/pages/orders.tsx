import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router"
import { ReceiptTextIcon, SearchIcon } from "lucide-react"

import { QueryState } from "@/components/query-state"
import { NotifyStatusBadge, OrderStatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup } from "@/components/ui/field"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useChannels, useOrders } from "@/hooks/queries"
import { formatTime } from "@/lib/format"

const PAGE_SIZE = 20

export function OrdersPage() {
  // 筛选条件保存在 URL 中，刷新页面或从其他页面跳转（如 ?notify=failed）都能保留
  const [params, setParams] = useSearchParams()
  const filters = {
    keyword: params.get("keyword") ?? "",
    status: params.get("status") ?? "all",
    notify: params.get("notify") ?? "all",
    type: params.get("type") ?? "all",
    page: Number(params.get("page") ?? 1),
  }
  const { data, error, refetch, isFetching } = useOrders({ ...filters, pageSize: PAGE_SIZE })
  const channels = useChannels()

  const update = (patch: Partial<Record<keyof typeof filters, string | number>>) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries({ ...patch, page: patch.page ?? 1 })) {
      if (value === "" || value === "all" || value === 1) next.delete(key)
      else next.set(key, String(value))
    }
    setParams(next, { replace: true })
  }

  // 关键字输入防抖 300ms 后再更新 URL 触发查询
  const [keyword, setKeyword] = useState(filters.keyword)
  useEffect(() => {
    if (keyword === filters.keyword) return
    const timer = setTimeout(() => update({ keyword }), 300)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword])

  const totalPages = data ? Math.max(1, Math.ceil(data.total / PAGE_SIZE)) : 1

  return (
    <Card>
      <CardHeader>
        <CardTitle>订单</CardTitle>
        <CardDescription>{data ? `共 ${data.total} 笔` : "加载中…"}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup>
          {/* responsive：窄屏纵向堆叠，宽屏横向排列（shadcn Field 内置） */}
          <Field orientation="responsive">
            <InputGroup>
              <InputGroupAddon>
                <SearchIcon />
              </InputGroupAddon>
              <InputGroupInput
                placeholder="订单号 / 商品名"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
              />
            </InputGroup>
            <Select value={filters.type} onValueChange={(type) => update({ type })}>
              <SelectTrigger aria-label="支付方式">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部方式</SelectItem>
                {channels.data?.map((c) => (
                  <SelectItem key={c.id} value={c.type}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={filters.status} onValueChange={(status) => update({ status })}>
              <SelectTrigger aria-label="支付状态">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="paid">已支付</SelectItem>
                <SelectItem value="pending">未支付</SelectItem>
              </SelectContent>
            </Select>
            <Select value={filters.notify} onValueChange={(notify) => update({ notify })}>
              <SelectTrigger aria-label="通知状态">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部通知</SelectItem>
                <SelectItem value="pending">通知中</SelectItem>
                <SelectItem value="success">已通知</SelectItem>
                <SelectItem value="failed">通知失败</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {!data ? (
            <QueryState error={error} onRetry={refetch} />
          ) : data.items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ReceiptTextIcon />
                </EmptyMedia>
                <EmptyTitle>暂无订单</EmptyTitle>
                <EmptyDescription>商户下单后会显示在这里。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table aria-busy={isFetching}>
              <TableHeader>
                <TableRow>
                  <TableHead>平台订单号</TableHead>
                  <TableHead>商户订单号</TableHead>
                  <TableHead>商品</TableHead>
                  <TableHead>金额</TableHead>
                  <TableHead>支付方式</TableHead>
                  <TableHead>支付状态</TableHead>
                  <TableHead>商户通知</TableHead>
                  <TableHead>创建时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.map((o) => (
                  <TableRow key={o.tradeNo}>
                    <TableCell>
                      <Button variant="link" size="sm" asChild>
                        <Link to={`/orders/${o.tradeNo}`}>{o.tradeNo}</Link>
                      </Button>
                    </TableCell>
                    <TableCell>{o.outTradeNo}</TableCell>
                    <TableCell>{o.name}</TableCell>
                    <TableCell>¥{o.money}</TableCell>
                    <TableCell>{channels.data?.find((c) => c.type === o.type)?.name ?? o.type}</TableCell>
                    <TableCell>
                      <OrderStatusBadge status={o.status} />
                    </TableCell>
                    <TableCell>
                      <NotifyStatusBadge status={o.notifyStatus} />
                    </TableCell>
                    <TableCell>{formatTime(o.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </FieldGroup>
      </CardContent>
      {data && totalPages > 1 && (
        <CardFooter>
          <PageNav page={filters.page} totalPages={totalPages} onChange={(page) => update({ ...filters, page })} />
        </CardFooter>
      )}
    </Card>
  )
}

/** 分页：上一页 / 当前页附近的页码 / 下一页。 */
function PageNav({ page, totalPages, onChange }: { page: number; totalPages: number; onChange: (p: number) => void }) {
  const start = Math.max(1, Math.min(page - 2, totalPages - 4))
  const pages = Array.from({ length: Math.min(5, totalPages) }, (_, i) => start + i)
  const go = (p: number) => (e: React.MouseEvent) => {
    e.preventDefault()
    if (p >= 1 && p <= totalPages && p !== page) onChange(p)
  }

  return (
    <Pagination>
      <PaginationContent>
        <PaginationItem>
          <PaginationPrevious href="#" text="上一页" onClick={go(page - 1)} aria-disabled={page <= 1} />
        </PaginationItem>
        {pages.map((p) => (
          <PaginationItem key={p}>
            <PaginationLink href="#" isActive={p === page} onClick={go(p)}>
              {p}
            </PaginationLink>
          </PaginationItem>
        ))}
        <PaginationItem>
          <PaginationNext href="#" text="下一页" onClick={go(page + 1)} aria-disabled={page >= totalPages} />
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}
