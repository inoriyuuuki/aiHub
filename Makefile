.PHONY: help dev frontend build server cli test test-unit integration up down logs clean

help:
	@echo "AIHub 开发任务"
	@echo "  make dev         启动开发依赖（PostgreSQL + MinIO）"
	@echo "  make frontend    构建前端到 internal/web/dist"
	@echo "  make build       构建 aihub-server 与 aihub 二进制"
	@echo "  make test-unit   运行单元测试"
	@echo "  make integration 运行集成测试（需要 docker compose 环境）"
	@echo "  make up          docker compose up -d（完整服务）"
	@echo "  make down        docker compose down"
	@echo "  make logs        查看服务日志"

dev:
	docker compose -f docker-compose.dev.yml up -d

frontend:
	cd frontend && npm install && npm run build

build: frontend
	go build -o bin/aihub-server ./cmd/aihub-server
	go build -o bin/aihub ./cmd/aihub

test-unit:
	go test ./internal/...

integration:
	go test -tags=integration -count=1 ./internal/tests/...

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f aihub

clean:
	rm -rf bin frontend/node_modules frontend/dist internal/web/dist
