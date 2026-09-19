# ---- 前端：管理后台 ----
FROM node:22-alpine AS web
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- 后端：Go 二进制（嵌入前端构建产物）----
FROM golang:1.25-alpine AS build
WORKDIR /src
# 国内构建可取消注释：
# ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /out/epay ./cmd/epay

# ---- 运行 ----
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 epay
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/epay /app/epay
RUN mkdir -p /app/data && chown epay /app/data
USER epay
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/epay", "-config", "/app/config.yaml"]
