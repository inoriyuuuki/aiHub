# 需求与里程碑

## 目标
AIHub 是可自托管的 AI 资产管理平台，第一版提供单管理员认证、提示词动态分类/动态表单/版本管理/效果附件、Skill 与专家包目录、第三方 MCP 配置目录、`aihub` CLI、AIHub 自身 MCP（stdio + Streamable HTTP）、模块化单体架构、Docker Compose 部署与 GitHub Actions 发布。

## 非目标（第一版不做）
- 不调用对话模型或生图模型。
- 不开放注册、邮箱验证和找回密码。
- 不代理或托管第三方 MCP 进程。
- 不实现运行时插件上传，不做微服务拆分。
- 不支持 Claude Code、Cursor 等其他客户端（保留适配器接口）。
- 不在服务端保存第三方 MCP 的真实密钥。

## 里程碑（阶段）
| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 0 | 需求和工程基线（go.mod、前端壳、ignore、Compose、Makefile、docs） | ✅ |
| 1 | 平台基础设施（PostgreSQL、MinIO、分页/错误码、前端布局组件） | ✅ |
| 2 | 单管理员认证（引导、Session、CSRF、限流、API Token） | ✅ |
| 3 | 动态 Schema 与提示词（分类、Schema 版本、草稿/发布/差异/回滚、附件、变量渲染） | ✅ |
| 4 | Skill 与专家包（压缩包校验、版本、专家包确定性构建） | ✅ |
| 5 | MCP 目录与 Codex CLI 适配（定义、Profile、安装清单、安全写入） | ✅ |
| 6 | AIHub MCP 服务（工具注册、只读/写分组、HTTP + stdio） | ✅ |
| 7 | 交付与发布（Dockerfile、Compose、CI、Tag 发布、CLI 二进制） | ✅（工作流已就绪，发布需在真实仓库执行） |

## 验收标准摘要
- Compose 启动后三服务健康，管理员可登录。
- 自定义分类/重复组表单可用；Schema 升级后旧提示词仍按旧 Schema 展示。
- 多图上传、多版本发布、差异、回滚可用；系统不调用任何 AI 模型。
- 非法 MIME、超限、路径穿越压缩包、缺失 `SKILL.md` 被拒绝。
- 专家包构建结果可重复。
- CLI 可安装/更新/卸载/恢复全局与项目 Skill。
- MCP Profile 多 MCP 组合写入正确，未托管配置不被覆盖。
- MCP stdio/HTTP 可搜索与读取；写工具默认不可用，需显式 scope；撤销 Token 立即失效。
