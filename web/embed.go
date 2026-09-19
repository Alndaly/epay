// Package web 嵌入管理后台前端的构建产物（web/dist），使网关保持单文件部署。
//
// dist 目录由 `pnpm build` 生成；仓库中只保留占位文件 .gitkeep，
// 未构建前端时网关仍可正常编译运行，访问 /admin/ 会提示先构建前端。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// UI 返回前端构建产物的文件系统（根目录即 dist）。
func UI() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
