# 测试计划与验收记录

## 单元测试
- `internal/platform/security`：argon2id 哈希往返、Token 哈希确定性。
- `internal/skillpack`：合法包、缺失 SKILL.md、路径穿越、绝对路径、嵌套根目录、超限。
- `internal/expertpack`：确定性构建、清单往返、非法输入。
- `internal/modules/prompts`：Schema 校验、内容校验、变量声明/渲染。
- `internal/cli`：TOML 合并保留未托管配置、更新/冲突检测。

## 集成测试（真实 PostgreSQL + MinIO，`-tags=integration`）
- `TestFullFlow`：登录 → 项目 → 分类/Schema → 提示词草稿/发布 ×2/差异/回滚/by-slug/渲染 → 附件（预签名 + SHA 校验 + 非法拒绝）→ Skill（合法上传 + 穿越拒绝）→ 专家包（成员 + 确定性构建）→ MCP 定义/Profile/清单 → Token 作用域（只读不能写、MCP 工具可见性、撤销立即失效）→ CLI 安装器（Skill 安装/更新/移除/恢复，MCP Profile 安装/重装保留未托管/移除）。
- `TestCLIStdioMCP`：构建 CLI 二进制，用官方 MCP 客户端经 stdio 连接 `aihub mcp serve`，验证 tools/list 与只读工具调用、写工具对只读 Token 不可见。

## 验收记录
- ✅ Compose 三服务健康、管理员登录。
- ✅ 动态分类/重复组表单、Schema 升级后旧提示词按旧 Schema 展示。
- ✅ 多图上传、多版本、差异、回滚；不调用 AI 模型。
- ✅ 非法 MIME/超限/穿越压缩包/缺失 SKILL.md 拒绝。
- ✅ 专家包确定性构建（两次构建摘要一致）。
- ✅ CLI 全局/项目 Skill 安装、更新、卸载、恢复。
- ✅ MCP Profile 写入正确、未托管配置不被覆盖、移除干净。
- ✅ MCP stdio/HTTP 只读可用、写工具需 scope、撤销 Token 立即失效。
- ⏳ Git tag 发布（需要真实 GitHub 仓库与 Docker Hub 凭据执行）。
