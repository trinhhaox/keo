#!/usr/bin/env bash
# Chạy migration theo thứ tự tên file, idempotent qua bảng schema_migrations.
# Mỗi file chạy trong MỘT transaction (-1): fail giữa chừng = rollback sạch,
# không có trạng thái nửa vời.
set -euo pipefail

: "${DATABASE_URL:?cần biến môi trường DATABASE_URL}"
DIR="${MIGRATIONS_DIR:-migrations}"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qc \
  "CREATE TABLE IF NOT EXISTS schema_migrations (
     version TEXT PRIMARY KEY,
     applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
   );"

for f in $(ls "$DIR"/*.sql | sort); do
  v=$(basename "$f")
  if [ -n "$(psql "$DATABASE_URL" -tAc "SELECT 1 FROM schema_migrations WHERE version='$v'")" ]; then
    echo "skip  $v"
    continue
  fi
  echo "apply $v"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -1 -f "$f"
  psql "$DATABASE_URL" -qc "INSERT INTO schema_migrations (version) VALUES ('$v')"
done
echo "migrations OK"
