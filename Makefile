# AIHub 构建入口
# 默认通过 Docker 构建（无需本机 Go/Node 工具链）
SHELL := /bin/bash

IMAGE      ?= aihub:latest
PLATFORMS  ?= linux/amd64,linux/arm64

.PHONY: all docker-build docker-artifacts build test run clean help

all: docker-build

## 通过 Docker 构建镜像（默认 aihub:latest，可用 IMAGE=xxx 覆盖）
docker-build:
	docker build --platform $(PLATFORMS) -t $(IMAGE) .

## 通过 Docker 构建二进制产物到 bin/（本机可用）
docker-artifacts:
	docker build --target builder --output type=local,dest=bin .

## 本地构建（需要本机 Go 工具链）
build:
	go build -trimpath -ldflags "-s -w" -o bin/aihub-server ./cmd/aihub-server
	go build -trimpath -ldflags "-s -w" -o bin/aihub ./cmd/aihub

## 本地测试
test:
	go test ./... -count=1

## 直接运行本地编译的服务器（AIHUB_PORT 可覆盖端口）
run:
	AIHUB_PORT=$(AIHUB_PORT) go run ./cmd/aihub-server

## 清理构建与运行产物
clean:
	rm -rf bin data logs

help:
	@grep -E '^[a-zA-Z_-]+:' Makefile | sed 's/:.*//' | sort -u
