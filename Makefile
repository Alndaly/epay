.PHONY: build web run dev test fmt docker

# 完整构建：先构建管理后台前端，再编译嵌入了前端的 Go 二进制
build: web
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/epay ./cmd/epay

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

run:
	go run ./cmd/epay -config config.yaml

# 前端开发：先 make run 启动网关（监听 8080），再执行本命令，接口会代理到网关
dev:
	cd web && pnpm dev

test:
	go test ./...
	cd web && pnpm exec tsc -b

fmt:
	gofmt -w .
	cd web && pnpm format

docker:
	docker build -t epay-gateway .
