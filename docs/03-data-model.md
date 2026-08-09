# 数据模型与迁移策略

## 核心表
| 表 | 说明 |
| --- | --- |
| users / sessions / api_tokens | 管理员、会话、API Token（只存哈希） |
| projects | 项目空间（global/project） |
| prompt_categories / prompt_schemas | 分类树 + 不可变 Schema 版本 |
| prompts / prompt_versions | 提示词主体 + 不可变版本（version 0 为草稿） |
| assets | MinIO 对象元数据（键、摘要、大小、MIME、引用） |
| skills / skill_versions | Skill 元数据 + 不可变压缩包版本 |
| expert_packs / expert_pack_versions / expert_members | 专家包、清单、锁定成员 |
| mcp_definitions / mcp_definition_versions / mcp_tools（JSONB） | MCP 定义与版本 |
| mcp_profiles / mcp_profile_items | Profile 与工具启停规则 |
| audit_log | 审计日志 |

## 唯一性与作用域解析
- 全局资源（project_id IS NULL）按 slug 唯一；项目资源按 (project_id, slug) 唯一。
- 解析规则：同一 slug 同时存在全局与项目资源时，项目资源优先；安装清单返回最终解析结果与来源（`project`/`global`）。

## 迁移策略
- 模块级 SQL 迁移（`Migration{ID, SQL}` 或 embed FS），`schema_migrations` 表记录。
- 迁移在 `pg_advisory_lock(83451023815)` 下执行，多实例并发只执行一次。
- 迁移按 ID 排序，只增不改；新列/新表以 `IF NOT EXISTS` 保证幂等。

## 审计字段
所有资源包含 `created_at` / `updated_at`；删除默认 `archived=true`（归档）。
