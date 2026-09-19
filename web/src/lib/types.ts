// 与后端 /admin/api 接口对应的数据类型（字段含义见 internal/admin）。

export type FieldType = "text" | "textarea" | "select" | "switch" | "number" | "tags"

/** 渠道驱动的一个配置项描述，前端据此自动渲染表单。 */
export interface DriverField {
  key: string
  label: string
  type: FieldType
  required?: boolean
  secret?: boolean
  placeholder?: string
  help?: string
  options?: { value: string; label: string }[]
  default?: unknown
}

export interface Driver {
  name: string
  title: string
  description: string
  defaultType: string
  /** 需要在渠道后台手动配置回调地址时的说明 */
  webhook: string
  fields: DriverField[] | null
}

export type ChannelOptions = Record<string, unknown>

export interface Channel {
  id: number
  type: string
  driver: string
  driverTitle: string
  name: string
  enabled: boolean
  options: ChannelOptions
  notifyUrl: string
  /** 渠道初始化失败的原因 */
  error?: string
  createdAt: string
  updatedAt: string
}

export interface ChannelInput {
  type: string
  driver: string
  name: string
  enabled: boolean
  options: ChannelOptions
}

export interface Merchant {
  pid: string
  /** 仅在新建时返回明文 */
  key: string
  keyMasked: string
  name: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export type OrderStatus = "pending" | "paid" | "expired"
export type NotifyStatus = "none" | "pending" | "success" | "failed"

export interface Order {
  tradeNo: string
  outTradeNo: string
  pid: string
  type: string
  name: string
  money: string
  param: string
  notifyUrl: string
  returnUrl: string
  clientIp: string
  device: string
  status: OrderStatus
  payKind: string
  payCurrency: string
  payAmount: string
  apiTradeNo: string
  buyer: string
  refundMoney: string
  notifyStatus: NotifyStatus
  notifyCount: number
  nextNotifyAt?: string
  notifyError: string
  createdAt: string
  expireAt?: string
  paidAt?: string
}

export interface Page<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface OrderQuery {
  keyword?: string
  status?: string
  notify?: string
  type?: string
  pid?: string
  page?: number
  pageSize?: number
}

/** 金额单位均为「分」 */
export interface DailyStat {
  date: string
  count: number
  amount: number
}

export interface Overview {
  summary: {
    todayOrders: number
    todayPaid: number
    todayAmount: number
    totalAmount: number
    notifyPending: number
    notifyFailed: number
    daily: DailyStat[]
  }
  system: {
    version: string
    baseUrl: string
    submitUrl: string
    mapiUrl: string
    apiUrl: string
    merchants: number
    channels: number
    activeChannels: number
    failedChannels: number
    drivers: number
  }
}
