# fantwo-core 常用开发命令
# 用法：make help

SHELL := /bin/bash
APP := fantwo-core
PORT ?= 3000

.PHONY: help build run test vet fmt lint cover swag route-audit clean docker-build docker-up docker-down

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## 编译到 bin/（静态二进制）
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(APP) ./cmd/server

run: ## 本地运行（需 PG/Redis）
	go run ./cmd/server

test: ## 跑全部单测
	go test ./...

vet: ## 静态检查
	go vet ./...

fmt: ## 格式化全部 Go 代码
	gofmt -w internal cmd migrations

lint: fmt vet ## 格式化 + 静态检查

cover: ## 生成覆盖率报告
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

swag: ## 重新生成 Swagger 文档
	swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal --exclude tmp,internal/dto

route-audit: ## 打印全部注册路由 + 总数（对照 docs/02 用）
	go run ./tmp/routeaudit 2>/dev/null || echo "（tmp/routeaudit 已清理，可参考 docs/06 的比对脚本）"

clean: ## 清理构建产物
	rm -rf bin coverage.out
	rm -f tmp/fantwo*.exe

docker-build: ## 构建镜像
	docker build -t $(APP):latest .

docker-up: ## 起本地全栈（PG + Redis + ES + app）
	docker compose up -d --build

docker-down: ## 停本地全栈并清理卷
	docker compose down -v
