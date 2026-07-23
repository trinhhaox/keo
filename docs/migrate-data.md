# Pha 3 — Di trú dữ liệu Supabase → Postgres VPS

> Chuyển toàn bộ dữ liệu app (users, sổ cái, kèo, hoạt động, shop...) từ Supabase
> Postgres sang Postgres tự host trên VPS. Chạy TRÊN VPS, dùng image `postgres:16`
> để `pg_dump` (khỏi cài client). Mọi lệnh ở thư mục `/opt/ro`.

## Nguyên tắc
- **Chỉ dữ liệu schema `public`** của app — KHÔNG đụng schema `auth`/`storage` của
  Supabase (danh tính Supabase không cần nữa; đã chuyển sang Google/Zalo native).
- Schema trên VPS do **migrations + boot DDL** dựng (đầy đủ cột `is_admin`, enum
  `daily_distance_km`, cột `is_charity`...). Vì vậy phải để stack **chạy 1 lần** cho
  schema hoàn chỉnh, rồi mới xoá seed và nạp dữ liệu thật.

## Điều kiện tiên quyết
- Đã làm xong Pha 2: `docker compose -f docker-compose.prod.yml up -d --build` chạy
  ít nhất 1 lần thành công (log app hiện `RO backend listening`).
- Có **connection string Supabase** (Dashboard → Project Settings → Database →
  Connection string → **URI**, dùng bản **Session pooler** nếu VPS chỉ có IPv4).
  Dạng: `postgresql://postgres.<ref>:<PW>@aws-...pooler.supabase.com:5432/postgres`

## Các bước

### 1. Đóng băng app (tránh ghi đè lúc nạp)
```bash
cd /opt/ro
docker compose -f docker-compose.prod.yml stop app caddy
# db vẫn chạy để nạp dữ liệu
```

### 2. Dump dữ liệu từ Supabase (chỉ data, schema public, trừ schema_migrations)
```bash
# THAY <SUPABASE_URI> bằng connection string thật. --disable-triggers để nạp
# không vướng thứ tự khoá ngoại. Không lấy owner/privileges của Supabase.
docker run --rm postgres:16 pg_dump \
  "<SUPABASE_URI>?sslmode=require" \
  --data-only --no-owner --no-privileges --disable-triggers \
  --schema=public --exclude-table=schema_migrations \
  > keo-data.sql

# Kiểm nhanh dung lượng + vài dòng đầu:
ls -lh keo-data.sql && head -20 keo-data.sql
```

### 3. Xoá seed → nạp dữ liệu → chuẩn hoá danh tính
```bash
# 3a. Xoá dữ liệu seed (charity users, shop_items)
docker compose -f docker-compose.prod.yml exec -T db \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 < deploy/03-truncate.sql

# 3b. Nạp dữ liệu thật
docker compose -f docker-compose.prod.yml exec -T db \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 < keo-data.sql

# 3c. Chuẩn hoá danh tính (NULL supabase_id cũ + chỗ cấp admin)
docker compose -f docker-compose.prod.yml exec -T db \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 < deploy/03-fixups.sql
```
> `$POSTGRES_USER`/`$POSTGRES_DB` lấy từ `.env` — nếu shell không có, thay bằng giá trị thật (vd `-U keo -d keo`).

### 4. Cấp quyền admin (thay email của bạn)
```bash
docker compose -f docker-compose.prod.yml exec db \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "UPDATE users SET is_admin=true WHERE email='admin@example.com';"
```

### 5. Bật lại app + kiểm tra
```bash
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml exec db \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "SELECT count(*) users FROM users; " \
  -c "SELECT count(*) entries FROM ledger_entries;"
```
Vào `https://ro.xox.vn` đăng nhập lại (Google/Zalo) → kiểm tra ví, kèo, lịch sử.

## Dọn dẹp
```bash
rm -f keo-data.sql   # chứa dữ liệu người dùng — xoá sau khi xác nhận OK
```

## Lỗi thường gặp
- **`duplicate key`** khi nạp → chưa chạy bước 3a (truncate). Chạy lại 3a rồi 3b.
- **`type "goal_type" ... "daily_distance_km"` invalid** → stack chưa chạy boot DDL.
  Bật app 1 lần (`up -d`) cho ddlBoot thêm enum, rồi lặp lại từ bước 1.
- **pg_dump không kết nối được** → dùng bản **Session pooler** (IPv4) của Supabase,
  hoặc thêm `?sslmode=require`.
