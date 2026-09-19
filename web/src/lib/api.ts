import type { Channel, ChannelInput, Driver, Merchant, Order, OrderQuery, Overview, Page } from "@/lib/types"

/** 接口错误，status 为 HTTP 状态码，message 为后端返回的可读原因。 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

const BASE = "/admin/api"

/**
 * 调用管理后台接口。后端统一返回 {"data": ...} 或 {"error": "..."}。
 * 写请求必须使用 JSON（后端据此防御 CSRF），因此总是带上 Content-Type。
 */
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  })
  const payload = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new ApiError(res.status, payload.error ?? `请求失败（HTTP ${res.status}）`)
  }
  return payload.data as T
}

function query(params: object): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "" && value !== "all") search.set(key, String(value))
  }
  const s = search.toString()
  return s ? `?${s}` : ""
}

export const api = {
  session: () => request<{ username: string }>("GET", "/session"),
  login: (username: string, password: string) =>
    request<{ username: string }>("POST", "/login", { username, password }),
  logout: () => request<null>("POST", "/logout"),

  overview: () => request<Overview>("GET", "/overview"),
  drivers: () => request<Driver[]>("GET", "/drivers"),

  channels: () => request<Channel[]>("GET", "/channels"),
  createChannel: (input: ChannelInput) => request<Channel>("POST", "/channels", input),
  updateChannel: (id: number, input: ChannelInput) => request<Channel>("PUT", `/channels/${id}`, input),
  toggleChannel: (id: number, enabled: boolean) => request<Channel>("PUT", `/channels/${id}/enabled`, { enabled }),
  deleteChannel: (id: number) => request<null>("DELETE", `/channels/${id}`),

  merchants: () => request<Merchant[]>("GET", "/merchants"),
  createMerchant: (input: { pid: string; name: string }) => request<Merchant>("POST", "/merchants", input),
  updateMerchant: (pid: string, input: { name: string; enabled: boolean }) =>
    request<Merchant>("PUT", `/merchants/${encodeURIComponent(pid)}`, input),
  merchantKey: (pid: string) => request<{ key: string }>("GET", `/merchants/${encodeURIComponent(pid)}/key`),
  resetMerchantKey: (pid: string) =>
    request<{ key: string }>("POST", `/merchants/${encodeURIComponent(pid)}/reset-key`, {}),
  deleteMerchant: (pid: string) => request<null>("DELETE", `/merchants/${encodeURIComponent(pid)}`),

  orders: (q: OrderQuery) => request<Page<Order>>("GET", `/orders${query(q)}`),
  order: (tradeNo: string) => request<Order>("GET", `/orders/${encodeURIComponent(tradeNo)}`),
  renotify: (tradeNo: string) => request<Order>("POST", `/orders/${encodeURIComponent(tradeNo)}/renotify`, {}),
  syncOrder: (tradeNo: string) => request<Order>("POST", `/orders/${encodeURIComponent(tradeNo)}/sync`, {}),
}
