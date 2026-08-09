# =============================================================================
# AIHub 多阶段镜像
#   Stage 1: 编译 Go 后端 (aihub-server + aihub CLI, 纯 Go 无 CGO)
#   Stage 2: 精简运行时 (debian bookworm-slim)
# 前端静态资源已构建并提交在 internal/web/dist，直接拷贝，无需 Node 构建阶段。
# 构建参数:
#   VERSION  - 版本号（写入镜像 label，默认 0.1.0）
#   BUILD    - 构建来源（默认 dev）
#   COMMIT   - git commit（默认 unknown）
# =============================================================================

# ---- Stage 1: Backend --------------------------------------------------------
FROM golang:1.26-alpine AS builder

ARG VERSION=0.1.0
ARG BUILD=dev
ARG COMMIT=unknown
ARG TARGETOS
ARG TARGETARCH

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64}

WORKDIR /build

# 先拷贝 go.mod/go.sum 以利用 Docker 层缓存
COPY go.mod go.sum* ./
RUN go mod download

COPY . ./
RUN go build -trimpath -ldflags "-s -w" -o /out/aihub-server ./cmd/aihub-server \
    && go build -trimpath -ldflags "-s -w" -o /out/aihub ./cmd/aihub

# ---- Stage 2: Runtime --------------------------------------------------------
FROM debian:bookworm-slim

ARG VERSION=0.1.0
ARG BUILD=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="aihub" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/inoriyuuuki/aiHub" \
      org.opencontainers.image.description="AIHub server: API + static frontend + Streamable HTTP MCP endpoint"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder /out/aihub-server /app/bin/aihub-server
COPY --from=builder /out/aihub        /app/bin/aihub
COPY internal/web/dist                /app/web/dist

RUN chmod +x /app/bin/aihub-server /app/bin/aihub \
    && mkdir -p /app/data /app/logs

# aihub-server: HTTP API + 静态前端 + Streamable HTTP MCP 端点
EXPOSE 8080

ENV APP_ENV=production \
    AIHUB_DATA_DIR=/app/data \
    AIHUB_LOG_DIR=/app/logs

WORKDIR /app
ENTRYPOINT ["/app/bin/aihub-server"]
