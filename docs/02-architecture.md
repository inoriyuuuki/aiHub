# 系统架构与模块开发规范

## 技术栈
- 后端：Go 1.26 模块化单体；`net/http` ServeMux；`pgx`；`minio-go`；官方 MCP Go SDK（`github.com/modelcontextprotocol/go-sdk`）。
- 前端：React + TypeScript + Vite，构建产物由 Go `embed` 托管。
- 存储：PostgreSQL（业务数据）+ MinIO/S3（对象文件）。

## 目录结构
```
cmd/aihub-server     服务端入口
cmd/aihub            CLI 入口
internal/config      环境变量配置
internal/platform/db PostgreSQL + 模块级迁移
internal/platform/storage  MinIO 封装
internal/platform/httpx    路由/中间件/错误/分页
internal/platform/security 密码与 Token 哈希
internal/modules     业务模块（core/prompts/skills/experts/mcpcat）
internal/mcpx        MCP 工具注册与服务（stdio + Streamable HTTP）
internal/cli         CLI：REST 客户端、Codex 安装器、stdio MCP 桥
internal/skillpack   Skill 压缩包校验
internal/expertpack  专家包确定性构建
internal/server      服务装配
internal/web         前端静态资源 embed
frontend/            React 前端
docs/                工程文档
deploy/              部署辅助
```

## 模块扩展机制
编译期 `Module` 接口（`internal/modules`）：
- `ID()` / `Version()` / `DependsOn()` / `Migrations()` / `Register()` / `MCPTools()` / `Health()`。
- 模块通过环境变量 `AIHUB_MODULES` 启停；新增模块需要写代码并重新构建。
- 前端菜单通过 `GET /api/v1/modules` 获取启用状态。

## 新增模块步骤
1. 新建 `internal/modules/<name>/`，实现 `Module` 接口。
2. 提供迁移 SQL（幂等、`migrations/*.sql` + embed）。
3. 在 `Register` 中注册路由（写路由用 `auth.RequireWrite("<group>")` / `RequireDelete("<group>")`）。
4. 在 `MCPTools` 或 `mcpTools()` 注册工具（`Write`/`Delete` 标记决定 scope 要求）。
5. 在 `internal/server/server.go` 的模块列表中注册。

## 约定
- 默认中文管理界面；数据库统一 UTC；浏览器本地时区显示。
- 所有业务资源默认私有；删除默认归档；只有未被版本引用的 MinIO 对象才允许后台物理清理。
- 列表接口统一分页 `page` / `pageSize`（最大 100），支持关键字/分类/标签/项目/状态筛选。
