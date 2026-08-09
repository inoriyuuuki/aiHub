#!/usr/bin/env bash
# AIHub 端到端冒烟脚本：验证 Web API、CLI、Skill/专家包/MCP Profile 安装与 stdio MCP。
# 前置：本机已运行 PostgreSQL(:5432) 与 MinIO(:9000)，以及 aihub-server(:8080)。
set -euo pipefail

SERVER="${AIHUB_SERVER:-http://localhost:8080}"
USER="${AIHUB_USERNAME:-admin}"
PASS="${AIHUB_PASSWORD:-e2e-password-123}"
WORK="$(mktemp -d)"
export AIHUB_CONFIG_DIR="$WORK/cliconfig"
export CODEX_HOME="$WORK/codexhome"
BIN="${BIN:-$(cd "$(dirname "$0")/.." && pwd)/bin/aihub}"

cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

echo "== 1. 健康检查 =="
curl -fsS "$SERVER/api/v1/health" | grep -q '"status":"ok"' || { echo "health failed"; exit 1; }

echo "== 2. CLI 登录 =="
"$BIN" login --server "$SERVER" --username "$USER" --password "$PASS"

echo "== 3. 发布并安装 Skill =="
SKILL_SLUG="e2e-skill-$$"
mkdir -p "$WORK/my-skill/scripts"
cat > "$WORK/my-skill/SKILL.md" <<MD
---
name: "$SKILL_SLUG"
description: e2e
---
# E2E
MD
echo '#!/bin/sh' > "$WORK/my-skill/scripts/run.sh"
"$BIN" skill publish "$WORK/my-skill" --slug "$SKILL_SLUG" --name "E2E Skill" --description e2e
"$BIN" skill install "$SKILL_SLUG" --scope global --dir "$WORK"
test -f "$CODEX_HOME/skills/$SKILL_SLUG/SKILL.md" || { echo "skill install failed"; exit 1; }
echo "Skill OK"

echo "== 4. MCP Profile 安装 =="
TOKEN="$(python3 -c "import json;print(json.load(open('$AIHUB_CONFIG_DIR/config.json'))['token'])")"
MCP_SLUG="e2e-mcp-$$"
PROF_SLUG="e2e-prof-$$"
DEF=$(curl -fsS -X POST "$SERVER/api/v1/mcp/definitions" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d "{\"name\":\"Demo\",\"slug\":\"$MCP_SLUG\",\"transport\":\"stdio\"}")
DEFID=$(echo "$DEF" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
curl -fsS -X POST "$SERVER/api/v1/mcp/definitions/$DEFID/versions" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"config":{"command":"npx","args":["-y","demo"]},"envVars":[],"tools":[]}' >/dev/null
VERID=$(curl -fsS "$SERVER/api/v1/mcp/definitions/$DEFID/versions" -H "Authorization: Bearer $TOKEN" | \
  python3 -c "import sys,json;print(json.load(sys.stdin)['data'][0]['id'])")
PID=$(curl -fsS -X POST "$SERVER/api/v1/mcp/profiles" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d "{\"name\":\"E2E\",\"slug\":\"$PROF_SLUG\",\"scope\":\"global\"}" | \
  python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
curl -fsS -X POST "$SERVER/api/v1/mcp/profiles/$PID/items" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d "{\"definitionId\":$DEFID,\"definitionVersionId\":$VERID}" >/dev/null
"$BIN" mcp install-profile "$PROF_SLUG" --scope global
grep -q "\[mcp_servers.aihub-$PROF_SLUG-$MCP_SLUG\]" "$CODEX_HOME/config.toml" || { echo "mcp install failed"; exit 1; }
echo "MCP Profile OK"

echo "== 5. stdio MCP 冒烟 =="
python3 - "$BIN" <<'PY'
import json, os, subprocess, sys, time
binp = sys.argv[1]
env = dict(os.environ, AIHUB_CONFIG_DIR=os.environ["AIHUB_CONFIG_DIR"])
p = subprocess.Popen([binp, "mcp", "serve"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, env=env)

def send(obj):
    p.stdin.write(json.dumps(obj) + "\n"); p.stdin.flush()

def recv(timeout=10):
    deadline = time.time() + timeout
    line = ""
    while time.time() < deadline:
        ch = p.stdout.read(1)
        if ch == "":
            return None
        line += ch
        if line.endswith("\n"):
            try:
                return json.loads(line)
            except Exception:
                line = ""
    return None

send({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}})
r1 = recv()
send({"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}})
r2 = recv()
names = [t["name"] for t in (r2 or {}).get("result", {}).get("tools", [])]
p.terminate()
assert r1 is not None and r2 is not None, "stdio MCP 无响应"
assert "prompts.read" in names, "缺少 prompts.read"
print("stdio MCP OK, tools:", len(names))
PY

echo "== 全部通过 =="
