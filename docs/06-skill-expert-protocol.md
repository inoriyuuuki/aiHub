# Skill / 专家包格式与安装协议

## Skill 包
- 格式：zip，根目录或单一顶层目录内必须包含合法 `SKILL.md`（frontmatter 含 `name`、`description`）。
- 安全校验：拒绝路径穿越（`..`、绝对路径）、符号链接、缺失 `SKILL.md`、超过大小限制。
- 发布：Web 上传或 `aihub skill publish <目录>`（应用安全默认排除项 + 项目 `.aihubignore`；`.git`、Secret、依赖缓存、超大文件不可反向包含）。
- 版本不可变：只能新建版本或归档。
- 安装清单：`GET /skills/install-manifest?slug=&project=` 返回最终解析结果（项目优先）与来源。

## 专家包
- 管理员选择多个 Skill 及其精确版本；构建时不改写成员 Skill 源码。
- 生成协调 Skill：`SKILL.md` 描述成员能力、适用场景与选择规则。
- 构建产物布局：`<pack>/SKILL.md`（协调）+ `<pack>/members/<memberSlug>/...`（成员）。
- 清单锁定协调 Skill 与全部成员的 slug、版本、SHA-256 与安装顺序。
- 确定性构建：成员按 slug 排序、zip 条目排序、固定时间戳 → 相同输入产生相同摘要。

## 安装协议（Codex）
- 全局 Skill → `~/.codex/skills/<slug>/`；项目 Skill → `<项目>/.codex/skills/<slug>/`。
- 每个安装目录写入 `.aihub-managed.json`（slug/version/sha256/source/installedAt）。
- 更新/卸载前备份到 `.aihub-backups/`；`aihub skill restore` 可恢复最近备份。
- 同名非托管目录拒绝覆盖。
- 专家包安装为一次原子操作：协调 Skill + 全部成员；任一失败回滚。

## Ignore 文件
- `.aihubignore`（Gitignore 风格）：发布排除规则。
- 安全默认排除：`.git/`、`node_modules/`、`vendor/`、`__pycache__/`、`.env`、`*.pem`、`*.key`、`id_rsa`、超大文件（>10MiB）等，且不能被 `!` 反向包含。
