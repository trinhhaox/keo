#!/usr/bin/env bash
# Redeploy RO trên VPS trong MỘT lệnh: đồng bộ cấu hình SePay → kéo code mới →
# build lại → xem log. Idempotent, chạy lại vô hại.
#
# Dùng (trên VPS, trong /opt/ro):
#   bash scripts/redeploy.sh                 # dùng STK/bank mặc định bên dưới
#   bash scripts/redeploy.sh 0977496222 MB   # hoặc truyền STK + mã ngân hàng
#
# Lưu ý: STK + mã ngân hàng là thông tin nhận tiền công khai (in trên QR), không
# phải bí mật. SEPAY_API_KEY là bí mật — script KHÔNG đụng tới, giữ nguyên trong .env.
set -euo pipefail

cd "$(dirname "$0")/.."   # về gốc repo (vd /opt/ro)

ACC="${1:-0977496222}"                 # SEPAY_ACCOUNT_NO
BANK="${2:-MB}"                        # SEPAY_BANK_CODE (mã ngắn: MB / Vietcombank / TPBank ...)
COMPOSE="docker compose -f docker-compose.prod.yml"

if [ ! -f .env ]; then
  echo "!! Không thấy .env — tạo từ env.prod.sample trước (xem docs/deploy-vps.md)" >&2
  exit 1
fi

# ---- 1. Đồng bộ cấu hình SePay trong .env (idempotent) ----
# Ghi giá trị sạch, không dấu nháy/khoảng trắng — acc/bank dính khoảng trắng khiến
# endpoint ảnh qr.sepay.vn trả HTML lỗi thay vì QR (ô QR trống).
set_env() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" .env; then
    sed -i "s|^${key}=.*|${key}=${val}|" .env
  else
    printf '%s=%s\n' "$key" "$val" >> .env
  fi
}
set_env SEPAY_ACCOUNT_NO "$ACC"
set_env SEPAY_BANK_CODE  "$BANK"
echo "✓ .env: SEPAY_ACCOUNT_NO=$ACC  SEPAY_BANK_CODE=$BANK"

# ---- 2. Kéo code mới ----
git pull --ff-only

# ---- 3. Build lại & khởi động (db/migrate/app/caddy theo thứ tự) ----
$COMPOSE up -d --build

# ---- 4. Kiểm tra log app ----
echo "--- log app (30 dòng cuối) ---"
$COMPOSE logs --tail=30 app
