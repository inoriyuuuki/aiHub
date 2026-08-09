# REST API 与错误码

统一前缀 `/api/v1`，响应格式：
```json
{ "data": ..., "error": { "code": "...", "message": "..." } }
```

## 认证
- `POST /auth/login` / `POST /auth/logout` / `GET /auth/me` / `POST /auth/password`
- `GET|POST /tokens`，`DELETE /tokens/{id}`
- Session：HttpOnly Cookie + CSRF（`X-CSRF-Token` 头）；API Token：`Authorization: Bearer <token>`。
- 写路由对 Token 调用者要求对应 scope（`<group>.write` / `write`），删除要求 `<group>.delete` / `delete`。

## 资源端点
| 模块 | 端点 |
| --- | --- |
| core | `/projects`，`/modules`，`/health` |
| prompts | `/prompt-categories`（含 `/schemas`），`/prompts`（含 `/publish`、`/versions/{v}`、`/versions/{v}/diff`、`/rollback`、`/render`、`/resolve?slug=`），`/assets/presign|confirm|{id}/url` |
| skills | `/skills`，`/skills/upload`，`/skills/{id}/versions`，`/skills/install-manifest?slug=`，`/skills/resolve?slug=` |
| experts | `/expert-packs`（含 `/members`、`/build`、`/versions`、`/install-manifest`） |
| mcp_catalog | `/mcp/definitions`，`/mcp/profiles`，`/mcp/install-manifest?profile=`，`/mcp/tools` |

## 错误码
| HTTP | code | 说明 |
| --- | --- | --- |
| 400 | bad_request | 请求参数错误 |
| 401 | unauthorized | 未登录 / Token 无效或已撤销 |
| 403 | forbidden | 缺少所需 scope |
| 404 | not_found | 资源不存在 |
| 409 | conflict | 唯一冲突或状态冲突 |
| 422 | validation_failed | Schema/内容/文件校验失败 |
| 429 | rate_limited | 登录限流 |
| 500 | internal_error | 内部错误 |

## 附件上传协议
1. `POST /assets/presign` → `{objectKey, uploadUrl, expiresIn}`。
2. 客户端 `PUT` 到预签名 URL。
3. `POST /assets/confirm` → 服务端校验对象大小、SHA-256、MIME 白名单并落库；失败对象延迟清理。
