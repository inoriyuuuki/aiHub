# MCP 工具、权限与 Codex 适配规范

## AIHub MCP 工具
按组注册，`GET /api/v1/mcp/tools` 返回完整清单：
- 只读（默认）：`projects.read`、`prompts.read`、`skills.read`、`experts.read`、`mcp_catalog.read`，另含 `prompts.render`。
- 写（需 `write` 或 `<group>.write` scope）：`prompts.write`、`skills.write`、`experts.write`、`mcp_catalog.write`。
- 删除（需 `delete` 或 `<group>.delete` scope，默认关闭）：`prompts.delete`、`skills.delete`、`experts.delete`、`mcp_catalog.delete`。

## 传输
- **Streamable HTTP**：`POST /mcp`，Bearer Token；`AuthenticateMCP` 要求 Token 至少具备 `read`/`mcp`/`mcp.read` 或任意 `*.read` scope。写工具是否可见取决于 Token scope；删除工具只对具备 `delete`/`<group>.delete` scope 的 Token 可见。
- **stdio**：`aihub mcp serve` 使用 CLI 本地凭据（Token scope 同样决定写工具可见性），工具实现桥接到 REST API。

## 第三方 MCP 目录
- 定义保存 transport（stdio/http）、command/args/url/workdir 模板、环境变量模板（名称/说明/敏感/必填）、工具清单。
- 不保存真实密钥；CLI 安装时本地询问或从 `--env-file` 读取。

## Codex 适配规范
- 全局 MCP → `~/.codex/config.toml`；项目 MCP → `<项目>/.codex/config.toml`。
- 仅编辑带 AIHub 标记的 `[mcp_servers.aihub-*]` 段；同名非托管配置拒绝覆盖。
- 每次修改前备份到 `.aihub-backups/config-<ts>.toml`；原子写入（临时文件 + rename）。
- 段内写入：`command`/`args` 或 `url`、`workdir`、`env = { KEY = "value" }`、`enabled_tools`/`disabled_tools`。
- 已安装 Profile 的段名记录在 `.aihub-mcp-managed.json`，卸载时按前缀清理。
