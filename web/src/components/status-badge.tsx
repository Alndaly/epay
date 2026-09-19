import { Badge } from "@/components/ui/badge"
import { notifyStatusMeta, orderStatusMeta } from "@/lib/format"
import type { NotifyStatus, OrderStatus } from "@/lib/types"

export function OrderStatusBadge({ status }: { status: OrderStatus }) {
  const meta = orderStatusMeta[status]
  return <Badge variant={meta.variant}>{meta.label}</Badge>
}

export function NotifyStatusBadge({ status }: { status: NotifyStatus }) {
  const meta = notifyStatusMeta[status]
  return <Badge variant={meta.variant}>{meta.label}</Badge>
}
