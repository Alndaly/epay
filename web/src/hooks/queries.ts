import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { ChannelInput, OrderQuery } from "@/lib/types"

// 集中定义查询键，避免各页面手写字符串导致缓存失效遗漏。
export const keys = {
  session: ["session"] as const,
  overview: ["overview"] as const,
  drivers: ["drivers"] as const,
  channels: ["channels"] as const,
  merchants: ["merchants"] as const,
  orders: (q: OrderQuery) => ["orders", q] as const,
  order: (tradeNo: string) => ["order", tradeNo] as const,
}

export const useSession = () => useQuery({ queryKey: keys.session, queryFn: api.session, retry: false })
export const useOverview = () => useQuery({ queryKey: keys.overview, queryFn: api.overview, refetchInterval: 30_000 })
export const useDrivers = () => useQuery({ queryKey: keys.drivers, queryFn: api.drivers, staleTime: Infinity })
export const useChannels = () => useQuery({ queryKey: keys.channels, queryFn: api.channels })
export const useMerchants = () => useQuery({ queryKey: keys.merchants, queryFn: api.merchants })
export const useOrders = (q: OrderQuery) =>
  useQuery({ queryKey: keys.orders(q), queryFn: () => api.orders(q), placeholderData: (prev) => prev })
export const useOrder = (tradeNo: string) =>
  useQuery({ queryKey: keys.order(tradeNo), queryFn: () => api.order(tradeNo) })

/** 渠道 / 商户变更会影响概览统计，统一刷新相关缓存。 */
function useInvalidate() {
  const qc = useQueryClient()
  return (...queryKeys: readonly (readonly unknown[])[]) =>
    Promise.all(queryKeys.map((queryKey) => qc.invalidateQueries({ queryKey })))
}

export function useSaveChannel(id?: number) {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: ChannelInput) => (id ? api.updateChannel(id, input) : api.createChannel(input)),
    onSuccess: () => invalidate(keys.channels, keys.overview),
  })
}

export function useToggleChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.toggleChannel(id, enabled),
    onSuccess: (c) => {
      toast.success(`${c.name}已${c.enabled ? "启用" : "停用"}`)
      return invalidate(keys.channels, keys.overview)
    },
    onError: (e) => toast.error(e.message),
  })
}

export function useDeleteChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: api.deleteChannel,
    onSuccess: () => {
      toast.success("支付渠道已删除")
      return invalidate(keys.channels, keys.overview)
    },
    onError: (e) => toast.error(e.message),
  })
}

export function useCreateMerchant() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: api.createMerchant,
    onSuccess: () => invalidate(keys.merchants, keys.overview),
  })
}

export function useUpdateMerchant() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ pid, ...input }: { pid: string; name: string; enabled: boolean }) => api.updateMerchant(pid, input),
    onSuccess: () => invalidate(keys.merchants),
    onError: (e) => toast.error(e.message),
  })
}

export function useDeleteMerchant() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: api.deleteMerchant,
    onSuccess: () => {
      toast.success("商户已删除")
      return invalidate(keys.merchants, keys.overview)
    },
    onError: (e) => toast.error(e.message),
  })
}

/** 订单运维操作（补发通知 / 主动查单），成功后刷新订单详情与列表。 */
export function useOrderAction(action: "renotify" | "sync") {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: action === "renotify" ? api.renotify : api.syncOrder,
    onSuccess: (order) => {
      qc.setQueryData(keys.order(order.tradeNo), order)
      qc.invalidateQueries({ queryKey: ["orders"] })
      qc.invalidateQueries({ queryKey: keys.overview })
      if (action === "renotify") toast.success("已重新排队通知商户")
      else toast.success(order.status === "paid" ? "上游确认已支付，订单已入账" : "上游显示尚未支付")
    },
    onError: (e) => toast.error(e.message),
  })
}
