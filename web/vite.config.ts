import fs from "node:fs"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// 管理后台挂载在网关的 /admin/ 路径下，构建产物由 Go 通过 embed 打包进二进制。
export default defineConfig({
  base: "/admin/",
  plugins: [
    react(),
    tailwindcss(),
    {
      // 构建会清空 dist，这里补回占位文件，保证未构建前端时 Go 的 //go:embed 仍能编译。
      name: "keep-dist-placeholder",
      closeBundle() {
        fs.writeFileSync(path.resolve(__dirname, "dist/.gitkeep"), "")
      },
    },
  ],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    // 开发时把接口请求代理到本地网关
    proxy: { "/admin/api": "http://127.0.0.1:8080" },
  },
})
