# Build stage - 编译 Go 二进制
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod ./
RUN go mod download

COPY . .

# 编译二进制
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -a \
    -ldflags="-s -w" \
    -tags netgo \
    -o dump1090-mutability \
    ./cmd/dump1090go

# Runtime stage - 使用 Debian（与原版保持一致）
FROM debian:stable-slim

WORKDIR /songyin

# 创建必要的目录（与原版一致）
RUN mkdir -p /run/dump1090-mutability

# 安装运行依赖（仅在需要 RTL-SDR 时）
# 如果只用 --net-only 可以不装这些
# RUN apt-get update && apt-get install -y \
#     rtl-sdr \
#     libusb-1.0-0 \
#     && rm -rf /var/lib/apt/lists/*

# 从构建阶段复制二进制
COPY --from=builder /build/dump1090-mutability /usr/bin/dump1090-mutability

# 暴露端口（与原版一致）
EXPOSE 30001 30002 30003 30004 30005

# 创建非 root 用户
RUN useradd -r -u 1000 -m -d /songyin dump1090 && \
    chown -R dump1090:dump1090 /songyin /run/dump1090-mutability

USER dump1090

# 默认命令（与原版兼容）
CMD ["dump1090-mutability", "--net-only"]
