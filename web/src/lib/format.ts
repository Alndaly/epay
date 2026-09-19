import type { NotifyStatus, OrderStatus } from "@/lib/types"

/** 把以「分」为单位的金额格式化为 ¥1,234.56 */
export function formatCents(cents: number): string {
  return "¥" + (cents / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** 格式化为 2026-09-20 14:03:05（本地时区） */
export function formatTime(value?: string): string {
  if (!value) return "—"
  const d = new Date(value)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

type BadgeVariant = "default" | "secondary" | "destructive" | "outline"

export const orderStatusMeta: Record<OrderStatus, { label: string; variant: BadgeVariant }> = {
  paid: { label: "已支付", variant: "default" },
  pending: { label: "待支付", variant: "secondary" },
  expired: { label: "已过期", variant: "outline" },
}

export const notifyStatusMeta: Record<NotifyStatus, { label: string; variant: BadgeVariant }> = {
  none: { label: "无需通知", variant: "outline" },
  pending: { label: "通知中", variant: "secondary" },
  success: { label: "已通知", variant: "default" },
  failed: { label: "通知失败", variant: "destructive" },
}
