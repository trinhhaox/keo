# Triển khai RO trên VPS (Ubuntu + Docker + Caddy)

> Domain: **ro.xox.vn** (DNS đã trỏ về IP VPS). Stack: Postgres 16 + app (Go binary
> serve API + SPA) + Caddy (TLS tự động). App KHÔNG mở cổng ra ngoài — chỉ Caddy.

## 0. Chuẩn bị (một lần)

SSH vào VPS:

```bash
ssh root@116.118.2.40
```

### Cài Docker + Compose plugin (Ubuntu)

```bash
# Idempotent — chạy lại vô hại.
curl -fsSL https://get.docker.com | sh
docker --version && docker compose version
```

### Mở firewall cho web (nếu bật ufw)

```bash
ufw allow 22/tcp    # SSH — giữ lại kẻo tự khoá mình
ufw allow 80/tcp    # Caddy: ACME HTTP-01 + redirect
ufw allow 443/tcp   # HTTPS
ufw --force enable
ufw status
```

> Cổng Postgres 5432 KHÔNG mở ra ngoài (compose không map cổng db ra host).

## 1. Lấy mã nguồn lên VPS

```bash
cd /opt
git clone <URL-repo> ro && cd ro
# hoặc: rsync -av --exclude node_modules --exclude .git ./ root@116.118.2.40:/opt/ro
```

> Lưu ý: `web/package-lock.json` PHẢI có trong repo (Dockerfile dùng `npm ci`).

## 2. Tạo file `.env` (bí mật — không commit)

```bash
cp env.prod.sample .env
nano .env
```

Điền tối thiểu (thiếu các biến "BẮT BUỘC" thì app tự thoát ở prod):

- `POSTGRES_PASSWORD` — mật khẩu mạnh, ngẫu nhiên
- `JWT_SECRET` — `openssl rand -base64 48`
- `TOKEN_CIPHER_KEY` — `openssl rand -base64 32` (đúng 32 byte)
- `GOOGLE_OAUTH_CLIENT_ID` — đã điền sẵn trong sample
- `ZALO_APP_ID`, `ZALO_SECRET_KEY`
- `STRAVA_CLIENT_ID`, `STRAVA_CLIENT_SECRET`, `STRAVA_VERIFY_TOKEN`, `STRAVA_SUBSCRIPTION_ID`
- `SEPAY_API_KEY`, `SEPAY_ACCOUNT_NO`, `SEPAY_BANK_CODE`

> Các giá trị này lấy từ cấu hình Vercel/Supabase cũ (trừ DB — dùng Postgres mới trên VPS).

## 3. Lên stack

```bash
docker compose -f docker-compose.prod.yml up -d --build
```

Thứ tự tự động: `db` (healthy) → `migrate` (chạy hết migration rồi thoát) → `app` → `caddy`.

## 4. Kiểm tra

```bash
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs -f caddy   # xem cấp cert Let's Encrypt
docker compose -f docker-compose.prod.yml logs -f app     # "RO backend listening :8080"
curl -I https://ro.xox.vn                                 # 200/301, cert hợp lệ
```

Mở trình duyệt `https://ro.xox.vn` → thử đăng nhập Google + Zalo.

## 5. Vận hành

```bash
# Cập nhật code mới:
git pull && docker compose -f docker-compose.prod.yml up -d --build

# Xem log, restart:
docker compose -f docker-compose.prod.yml logs -f app
docker compose -f docker-compose.prod.yml restart app

# Backup DB:
docker compose -f docker-compose.prod.yml exec db \
  pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB" > backup-$(date +%F).sql
```

## Ghi chú

- **Settlement + Strava ingest chạy in-process** trong binary (worker mỗi 15p / 5s) →
  KHÔNG cần cron ngoài (Cloudflare Worker cũ đã gỡ khỏi repo).
- **Di trú dữ liệu Supabase → Postgres VPS**: xem Pha 3 (chạy lần đầu với DB rỗng để
  test TLS + login trước cũng được, sau đó import dữ liệu thật).
- Sau khi cài xong: **đổi mật khẩu root**, cân nhắc SSH key + tắt password auth.
