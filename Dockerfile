# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src
# 国内构建可取消注释：
# ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/epay ./cmd/epay

# ---- run ----
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
