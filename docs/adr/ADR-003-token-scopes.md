# ADR-003：API Token 作用域与 MCP 写工具门控

- 日期：2026-08-09
- 状态：已接受

## 决策
- Token 支持 `read`/`write`/`delete` 通用作用域与 `<group>.read|write|delete` 模块作用域；数据库只存哈希。
- REST 写路由对 Token 调用者要求写/删除作用域；Session 调用者不受限。
- MCP 写/删除工具只有 Token 具备对应作用域才可见（BuildServer 按 scope 过滤）。

## 后果
撤销 Token 后 REST 与 MCP 立即失效（每次请求实时查询）。
