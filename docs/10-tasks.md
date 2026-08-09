# 任务拆分

## 阶段 0：需求和工程基线
- [x] docs 固化需求/非目标/架构/验收
- [x] go.mod、前端壳、配置、日志、错误响应
- [x] 模块注册协议与 /api/v1/modules
- [x] .env.example、ignore、开发 Compose、Makefile

## 阶段 1：平台基础设施
- [x] PostgreSQL 连接、迁移锁、事务、健康检查
- [x] MinIO Bucket、预签名上传、摘要校验、延迟清理接口
- [x] 统一分页、筛选、审计字段、错误码
- [x] 前端布局、模块菜单、表格、表单、上传、错误组件

## 阶段 2：单管理员认证
- [x] 首次启动管理员初始化
- [x] 登录/退出/Session/修改密码/登录限流
- [x] API Token（作用域/过期/撤销）
- [x] 前端登录页、安全设置页

## 阶段 3：动态 Schema 与提示词
- [x] 分类树、Schema 编辑器与后端校验器
- [x] 对话/生图/代码种子模板
- [x] 草稿/发布版本/差异/回滚/标签/搜索
- [x] MinIO 效果文件上传、预览、版本引用
- [x] 变量声明/校验/渲染预览

## 阶段 4：Skill 与专家包
- [x] Skill 元数据/分类/版本/压缩包校验
- [x] CLI/Web 发布（.aihubignore）
- [x] 专家包编辑器、成员锁定、协调 Skill 生成
- [x] 确定性构建、摘要、下载清单、回滚信息

## 阶段 5：MCP 目录与 Codex CLI 适配
- [x] 定义/版本/环境变量模板/工具清单
- [x] Profile 组合与工具粒度启停
- [x] Codex 全局/项目配置适配器
- [x] 配置段合并、备份、冲突检测、更新/卸载
- [x] Skill/专家包原子安装
- [x] 本地凭据文件与 Secret 注入

## 阶段 6：AIHub MCP 服务
- [x] 统一 MCP Tool Registry
- [x] 只读工具（提示词/Skill/专家包/MCP 目录/项目）
- [x] 写工具分组与 Token Scope 校验
- [x] /mcp Streamable HTTP endpoint
- [x] aihub mcp serve stdio endpoint
- [x] REST/stdio/HTTP 结果与权限一致性验证

## 评审修复（多子代理 review 后）
- [x] MCP 删除工具不再被 write 作用域放行；Token 不能越权铸造更高 scope
- [x] CSRF 双提交校验实际生效（Cookie + X-CSRF-Token）
- [x] 附件上传：MIME 白名单补全 effect-file、魔数嗅探、objectKey 绑定、SVG 从图片白名单移除
- [x] 登录限流 map 有界 + 未知用户等时返回；Session 定期清理；HTTP 超时补齐
- [x] GET /projects 分页参数修复；createPrompt 事务化；版本号加锁（FOR UPDATE）
- [x] 归档语义统一（已归档资源拒绝写操作）；回滚同步草稿；分类-项目范围校验
- [x] slug 服务端/CLI 双端校验（防路径穿越）；CLI 下载包 SHA-256 校验
- [x] CLI：配置/备份 0600、RemoveProfile 原子+备份、嵌套排除、logout 撤销 Token、stdin 不回显、TOML 安全转义

## 阶段 7：交付与发布
- [x] 多阶段 Dockerfile
- [x] Docker Compose + 健康检查 + 持久卷
- [x] PR CI（Go 测试、前端检查、构建、Docker build）
- [x] Tag/手动发布工作流（多架构镜像、SBOM、签名、CLI 二进制 + SHA-256）
