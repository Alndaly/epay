import { createBrowserRouter } from "react-router"

import { AppLayout } from "@/pages/layout"
import { LoginPage } from "@/pages/login"

/** 页面标题通过路由 handle 声明，由顶栏统一展示。 */
export interface RouteHandle {
  title: string
}

// 业务页面按路由懒加载，图表库等较大的依赖只在用到的页面加载。
export const router = createBrowserRouter(
  [
    { path: "/login", element: <LoginPage /> },
    {
      path: "/",
      element: <AppLayout />,
      children: [
        {
          index: true,
          handle: { title: "概览" },
          lazy: () => import("@/pages/overview").then((m) => ({ Component: m.OverviewPage })),
        },
        {
          path: "orders",
          handle: { title: "订单" },
          lazy: () => import("@/pages/orders").then((m) => ({ Component: m.OrdersPage })),
        },
        {
          path: "orders/:tradeNo",
          handle: { title: "订单详情" },
          lazy: () => import("@/pages/order-detail").then((m) => ({ Component: m.OrderDetailPage })),
        },
        {
          path: "channels",
          handle: { title: "支付渠道" },
          lazy: () => import("@/pages/channels").then((m) => ({ Component: m.ChannelsPage })),
        },
        {
          path: "channels/new",
          handle: { title: "新建支付渠道" },
          lazy: () => import("@/pages/channel-edit").then((m) => ({ Component: m.ChannelEditPage })),
        },
        {
          path: "channels/:id",
          handle: { title: "编辑支付渠道" },
          lazy: () => import("@/pages/channel-edit").then((m) => ({ Component: m.ChannelEditPage })),
        },
        {
          path: "merchants",
          handle: { title: "商户" },
          lazy: () => import("@/pages/merchants").then((m) => ({ Component: m.MerchantsPage })),
        },
      ],
    },
  ],
  { basename: "/admin" },
)
