import * as React from "react"
import { Link } from "react-router"
import { LayoutDashboardIcon, ReceiptTextIcon, StoreIcon, WalletCardsIcon, ZapIcon } from "lucide-react"

import { NavMain } from "@/components/nav-main"
import { NavUser } from "@/components/nav-user"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

const navMain = [
  { title: "概览", url: "/", icon: <LayoutDashboardIcon /> },
  { title: "订单", url: "/orders", icon: <ReceiptTextIcon /> },
  { title: "支付渠道", url: "/channels", icon: <WalletCardsIcon /> },
  { title: "商户", url: "/merchants", icon: <StoreIcon /> },
]

export function AppSidebar({ username, ...props }: React.ComponentProps<typeof Sidebar> & { username: string }) {
  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild className="data-[slot=sidebar-menu-button]:p-1.5!">
              <Link to="/">
                <ZapIcon className="size-5!" />
                <span className="text-base font-semibold">统一支付网关</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={navMain} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser username={username} />
      </SidebarFooter>
    </Sidebar>
  )
}
