# ---- frontend build ----
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---- go build ----
FROM golang:1.26-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Frontend dist must exist for the embedded FS.
COPY --from=frontend /app/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aihub-server ./cmd/aihub-server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aihub ./cmd/aihub

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 aihub
COPY --from=build /out/aihub-server /usr/local/bin/aihub-server
COPY --from=build /out/aihub /usr/local/bin/aihub
USER aihub
EXPOSE 8080
ENTRYPOINT ["aihub-server"]
