import { useMatches } from "react-router"

import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import type { RouteHandle } from "@/router"

export function SiteHeader() {
  const matches = useMatches()
  const title = (matches.at(-1)?.handle as RouteHandle | undefined)?.title ?? ""

  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" aria-label="切换侧边栏" />
        {/*
          Separator 组件的竖向基础样式带 self-stretch，与这里固定的 h-4 冲突时会贴顶（stretch + 定高 ≈ flex-start）；
          该样式用属性选择器书写、优先级高于普通 class，因此这里用 ! 强制居中。
        */}
        <Separator orientation="vertical" className="mx-2 self-center! data-[orientation=vertical]:h-4" />
        <h1 className="text-base font-medium">{title}</h1>
      </div>
    </header>
  )
}
