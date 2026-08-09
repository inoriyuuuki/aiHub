# 部署、备份、恢复与升级

## 部署
- `docker compose up -d --build`：postgres（16-alpine）+ minio + aihub；三服务均带健康检查与持久卷（`pgdata`、`miniodata`）。
- 管理员密码通过 Docker secret `admin_password` 注入 `ADMIN_PASSWORD_FILE`。
- 反向代理：对外建议 HTTPS，设置 `AIHUB_PUBLIC_BASE_URL`。

## 备份
- 数据库：`docker compose exec postgres pg_dump -U aihub aihub > backup.sql`。
- MinIO：备份数据卷或使用 `mc mirror` 同步 `aihub` bucket。
- 建议同时备份 `admin_password.txt`（不落库的引导密码）。

## 恢复
```bash
docker compose exec -T postgres psql -U aihub -d aihub < backup.sql
```

## 升级
1. `docker compose pull aihub`
2. `docker compose up -d`
3. 迁移自动执行（advisory lock 保证单实例执行）。
4. 升级前建议备份数据卷。

## 发布（GitHub Actions）
- `ci.yml`：前端构建、Go build/vet/单元测试、集成测试（Postgres + MinIO service 容器）、Docker build。
- `release.yml`（tag `v*`）：构建 darwin/linux/windows × amd64/arm64 CLI 二进制 + SHA-256；构建并推送 `aihub/aihub` 多架构镜像（版本/latest/SHA 标签、SBOM、provenance）；创建 GitHub Release。
