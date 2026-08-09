# AIHub

AIHub 是一个可自托管的 AI 资产管理平台。第一版提供单管理员账号、提示词动态分类与版本管理、Skill 与专家包目录、第三方 MCP 配置目录，以及 `aihub` 本地 CLI 和 AIHub 自身的 MCP（stdio + Streamable HTTP）接口。

> 第一版明确不调用任何对话/生图模型，只管理提示词、模型元数据与效果文件；不开放注册；不在服务端启动或代理第三方 MCP 进程；不在服务端保存第三方 MCP 的真实密钥。

## 功能清单

- **单管理员认证**：首次启动通过 `ADMIN_USERNAME` + `ADMIN_PASSWORD_FILE` 初始化；HttpOnly/Secure/SameSite Session + CSRF；登录限流；可命名、可过期、带作用域（scope）的 API Token（数据库只存哈希）。
- **项目空间**：`global` / `project` 两种范围；同 slug 同时存在时项目资源优先。
- **动态提示词**：树形分类 + 版本化 JSON Schema（`x-aihub-*` UI 扩展）；草稿/发布不可变版本/差异/回滚；`{{variable}}` 模板变量声明与渲染预览；效果图与附件上传到 MinIO（预签名上传、SHA-256/MIME/大小校验）。
- **Skill 目录**：上传压缩包（校验 `SKILL.md`、路径穿越/符号链接/超限），版本发布后不可变；安装清单按项目优先解析。
- **专家包**：锁定多个 Skill 版本，生成协调 Skill，构建结果确定性（相同成员版本 → 相同摘要）；原子安装 + 可回滚备份。
- **MCP 目录**：stdio / HTTP 传输定义、环境变量模板（敏感值不落库）、工具清单、Profile 组合与工具粒度启停；生成 Codex 安装清单。
- **`aihub` CLI**：登录/登出、`skill publish/install/update/remove/restore`、`expert install/remove`、`mcp install-profile/remove-profile`、`mcp serve`（本地 stdio MCP）、`status`。
- **AIHub MCP**：统一工具注册；默认只读，写工具需显式 scope；HTTP MCP 使用带作用域 Bearer Token，stdio MCP 使用 CLI 本地凭据；撤销 Token 立即失效。
- **部署**：多阶段 Dockerfile + Docker Compose（AIHub + PostgreSQL + MinIO，健康检查、持久卷）；GitHub Actions CI 与 Tag 发布（amd64/arm64 镜像 + 三平台 CLI 二进制 + SHA-256）。

## 快速启动（Docker Compose）

```bash
cp admin_password.txt.example admin_password.txt   # 修改为强密码
docker compose up -d --build
# 打开 http://localhost:8080 ，使用 ADMIN_USERNAME 与 admin_password.txt 内容登录
```

`docker compose ps` 应显示三个服务均 healthy。

## 必需环境变量

| 变量 | 说明 | 默认 |
| --- | --- | --- |
| `AIHUB_HTTP_ADDR` | HTTP 监听地址 | `:8080` |
| `AIHUB_DATABASE_URL` | PostgreSQL DSN | `postgres://aihub:aihub@localhost:5432/aihub?sslmode=disable` |
| `AIHUB_MINIO_ENDPOINT` / `AIHUB_MINIO_ACCESS_KEY` / `AIHUB_MINIO_SECRET_KEY` / `AIHUB_MINIO_USE_SSL` / `AIHUB_MINIO_BUCKET` | MinIO/S3 兼容存储 | 见 `.env.example` |
| `ADMIN_USERNAME` | 管理员用户名 | `admin` |
| `ADMIN_PASSWORD_FILE` | 管理员初始密码文件路径（首次启动必填） | 无 |
| `AIHUB_SESSION_TTL` / `AIHUB_LOGIN_MAX_ATTEMPTS` / `AIHUB_LOGIN_WINDOW` | 会话与限流 | `24h` / `5` / `5m` |
| `AIHUB_MAX_UPLOAD_MB` | 上传大小上限 | `100` |
| `AIHUB_MODULES` | 启停模块（逗号分隔，留空=全部） | 全部 |

## 基本用法

### Web

登录后可在「提示词」维护分类与表单、编辑草稿、发布版本、上传效果图；在「Skills」上传压缩包；在「专家包」选择 Skill 版本并构建；在「MCP 目录」维护定义与 Profile；在「安全设置」修改密码、创建/撤销 API Token。

### CLI

```bash
aihub login --server http://localhost:8080 --username admin --password <密码>
aihub skill publish ./my-skill --slug my-skill
aihub skill install my-skill --scope global
aihub mcp install-profile default --scope global
aihub status
```

- 全局安装目标：`~/.codex`（或 `$CODEX_HOME`）；项目安装目标：`<项目>/.codex`。
- MCP 配置只写入带 AIHub 标记的 `[mcp_servers.aihub-*]` 段；同名非托管配置拒绝覆盖；每次修改前自动备份到 `.aihub-backups/`。

### MCP

- **HTTP**：`POST /mcp`，`Authorization: Bearer <api-token>`（Token 需要 `mcp` 或 `*.read` 作用域）。
- **stdio**：`aihub mcp serve`（使用 CLI 已登录凭据）。
- 写工具（`*.write` / `*.delete`）只有在 Token 具备对应 scope 时才可见。

## Docker Hub 镜像与升级

镜像：`aihub/aihub`（Tag：版本号、`latest`、commit SHA；支持 `linux/amd64`、`linux/arm64`；含 SBOM 与 provenance）。

升级：

```bash
docker compose pull aihub
docker compose up -d
```

数据库迁移使用模块级 SQL + advisory lock，多实例并发启动只会执行一次；升级前建议备份数据卷。

## 安全注意事项

- 生产环境请使用强管理员密码、HTTPS 反向代理，并将 `AIHUB_MINIO_*` 与数据库凭据改为随机值。
- API Token 只显示一次，服务端只保存哈希；撤销后 REST 与 MCP 立即失效。
- Skill/专家包压缩包会校验路径穿越、绝对路径、符号链接与大小；`aihub skill publish` 应用安全默认排除项（`.git`、Secret、依赖缓存、超大文件不可反向包含）。
- 第三方 MCP 密钥不保存在服务端；CLI 安装 Profile 时本地询问或从 `--env-file` 读取后写入 Codex 配置。

## 项目状态

- 阶段 0-6 已实现：工程基线、平台基础设施、单管理员认证、动态 Schema 与提示词、Skill 与专家包、MCP 目录与 Codex 适配、AIHub MCP 服务。
- 阶段 7（交付与发布）：Dockerfile/Compose/CI/Release 工作流已就绪。
- 测试：单元测试 + 真实 PostgreSQL/MinIO 集成测试（含 REST、MCP HTTP、MCP stdio、CLI 安装器端到端）。
- 详细需求、架构、数据模型、API、Schema 规范、Skill/专家包协议、MCP 规范与部署文档见 [docs/](docs/)。

## 开发

```bash
make dev        # 启动 PostgreSQL + MinIO（docker compose）
make frontend   # 构建前端到 internal/web/dist
make build      # 构建 aihub-server / aihub 二进制
make test-unit  # 单元测试
make integration  # 集成测试（需要本地 PostgreSQL + MinIO）
```
