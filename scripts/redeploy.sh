#!/usr/bin/env bash
# Bước build TRÊN VPS: rebuild + restart app rồi in log. Idempotent.
#
# Được GitHub Actions gọi sau khi rsync code mới lên /opt/ro
# (xem .github/workflows/ci.yml, job "deploy"). Chạy tay cũng được:
#   cd /opt/ro && bash scripts/redeploy.sh
#
# Cấu hình bí mật (SEPAY_*, JWT_SECRET, DB...) nằm ở /opt/ro/.env — script KHÔNG
# đụng tới. Caddyfile trên VPS là host-specific (phục vụ nhiều domain) nên deploy
# cũng KHÔNG ghi đè (rsync loại trừ).
set -euo pipefail

cd "$(dirname "$0")/.."   # về gốc repo (vd /opt/ro)

COMPOSE="docker compose -f docker-compose.prod.yml"

# Rebuild image (frontend Vite + Go binary) và khởi động lại đúng service app.
# db/migrate là dependency — compose tự chạy migrate rồi mới lên app.
$COMPOSE up -d --build app

echo "--- log app (30 dòng cuối) ---"
$COMPOSE logs --tail=30 app
