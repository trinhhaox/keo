# Kèo — Kiến trúc hệ thống & Danh sách tính năng

> App "cược điểm tập luyện": thử thách vận động (đi bộ/chạy) cùng bạn bè, đặt cược bằng điểm,
> đồng bộ hoạt động từ Strava, có yếu tố từ thiện và shop đổi quà.
> Prod: **app.xox.vn** (Vercel serverless). Cập nhật: 2026-07-19.

## 1. Kiến trúc tổng quan

```mermaid
flowchart TB
    User(["Người dùng"])
    subgraph FE["Frontend — React 18 + Vite + Tailwind (SPA)"]
        WEB["web/ — Vercel static"]
    end
    subgraph BE["Backend — Go 1.25 serverless"]
        API["api/index.go (Vercel entrypoint)"]
    end
    DB[("PostgreSQL / Supabase\ndouble-entry ledger")]
    STRAVA["Strava\n(OAuth + Webhook)"]
    SEPAY["SePay\n(nạp điểm)"]
    ZALO["Zalo OAuth"]
    CFCRON["Cloudflare Worker\ncron */5"]
    VCRON["Vercel cron\ndaily (backstop)"]

    User --> WEB --> API --> DB
    ZALO -. đăng nhập .-> API
    STRAVA -. webhook hoạt động .-> API
    SEPAY -. webhook thanh toán .-> API
    API -. fetch activity .-> STRAVA
    CFCRON -->|/api/cron/strava| API
    VCRON -->|/api/cron/strava, /settle| API
```

- **Frontend**: React 18 + Vite + Tailwind (build-time), SPA phục vụ static qua Vercel.
- **Backend**: Go 1.25 serverless (`api/index.go`); cũng chạy được **binary VPS** (`cmd/server`) qua docker-compose.
- **DB**: PostgreSQL (Supabase) + pgx/v5, dùng **sổ cái kép (double-entry ledger)** cho mọi biến động điểm.
- **Auth**: Zalo OAuth → JWT HS256 (ký nội bộ bằng `jwtSecret`).
- **Dịch vụ ngoài**: Strava (webhook + OAuth), SePay (nạp điểm), Cloudflare (domain + Worker cron).

## 2. Cấu trúc mã nguồn (Go packages)

| Package | Vai trò |
|---|---|
| `api/` | Entrypoint Vercel: khởi tạo services, gắn routes, 2 cron |
| `restapi/` | HTTP handlers: auth, zalo, challenges, wallet, shop, admin |
| `challenge/` | Logic kèo + `SettlementJob` (giải quyết kèo đến hạn) |
| `ledger/` | Sổ cái kép: `ledger_accounts`, `ledger_entries`, `ledger_transactions` |
| `reward/` | Thưởng check-in + thưởng quãng đường (1 điểm/km tròn, có DailyCap) |
| `ingest/` | Đồng bộ Strava/health: webhook inbox, worker, KMS/cipher mã hoá token |
| `payment/` | Nạp điểm qua SePay |
| `migrations/` | SQL migrations nhúng (`embed.go`) + runner |
| `cmd/server/` | Bản chạy binary trên VPS (long-running) |
| `cloudflare/strava-cron/` | Cloudflare Worker cron `*/5` drain hàng đợi webhook |
| `web/` | Frontend React |

**Frontend modules** (`web/src/`): `App.jsx` (core) · `activity-feed` · `create-sheet` · `delivery-modal` · `leaderboard-sheet` · `admin-dashboard` · `notification` · `ui-primitives` · `api.js` · `theme.js`.
**5 tab**: Khám phá · Của tôi · Shop · Ví · Tài khoản.

## 3. Mô hình dữ liệu (18 bảng)

| Nhóm | Bảng |
|---|---|
| Người dùng | `users`, `user_integrations` |
| **Sổ cái kép** | `ledger_accounts`, `ledger_entries`, `ledger_transactions`, `account_balances` |
| Kèo | `challenges`, `enrollments`, `enrollment_periods` |
| Hoạt động & thưởng | `activities`, `checkins`, `reward_events`, `reward_daily` |
| Shop | `shop_items`, `point_purchases`, `redemptions` |
| Ingest | `webhook_inbox` (hàng đợi retry — H2b claim-then-process) |
| Hệ thống | `schema_migrations` |

> Điểm là tài sản có giá trị → mọi biến động đi qua sổ cái kép (tài khoản `user_available` / `user_locked`);
> khoá cược = chuyển available→locked, hoàn/giải quyết = `stake_release` locked→available. Không bao giờ DELETE thẳng trên ledger.

## 4. Danh sách tính năng (theo API)

### 🔐 Xác thực
- Đăng nhập **Zalo** OAuth: `POST /v1/auth/zalo` → `POST /v1/auth/zalo/verify`; dev-login
- Kết nối **Strava** OAuth: `GET /v1/oauth/strava/callback`

### 🎯 Kèo (thử thách)
- Xem danh sách kèo, nhóm theo trạng thái mở / đang chạy / kết thúc: `GET /v1/challenges`
- Tạo kèo (đặt tên, bộ môn, mục tiêu, số kỳ, tiền cược) — atomic create+join: `POST /v1/challenges`
- Tham gia kèo, khoá điểm cược: `POST /v1/challenges/{id}/join`
- Bảng xếp hạng từng kèo: `GET /v1/challenges/{id}/leaderboard`
- "Của tôi": `GET /v1/me/challenges`, thống kê `GET /v1/me/stats`

### 🏃 Hoạt động & thưởng
- Đồng bộ Strava tự động (webhook): `POST /webhooks/strava` (verify `GET /webhooks/strava`)
- Đồng bộ health data mobile: `POST /v1/health-sync`
- Hoạt động gần đây: `GET /v1/me/activities`
- **Check-in** thưởng điểm: `POST /v1/checkins`
- Lịch sử điểm thưởng (phân biệt check-in vs tập luyện): `GET /v1/rewards`

### 💰 Ví điểm
- Số dư ví: `GET /v1/wallet` · Lịch sử giao dịch: `GET /v1/wallet/transactions`
- Nạp điểm: `POST /v1/wallet/purchase` → xác nhận qua `POST /webhooks/sepay`

### 🛍️ Shop & từ thiện
- Xem sản phẩm: `GET /v1/shop` · Đổi quà + giao hàng: `POST /v1/redemptions`, `GET /v1/redemptions`
- Quỹ từ thiện: `GET /v1/charities/stats` (kèo có thể quyên góp)

### 🛠️ Quản trị (admin)
- Quản lý user + điều chỉnh điểm: `GET /v1/admin/users`, `POST /v1/admin/users/{id}/adjust`
- CRUD sản phẩm shop: `GET/POST/PUT/DELETE /v1/admin/shop-items`
- Duyệt đơn đổi quà: `GET /v1/admin/redemptions`, `POST /v1/admin/redemptions/{id}/status`

### ⚙️ Nền (cron)
- `/api/cron/strava` — drain hàng đợi webhook Strava (Cloudflare Worker `*/5`)
- `POST /api/cron/settle` — giải quyết kèo đến hạn (chia thưởng / hoàn cược)

## 5. Luồng đồng bộ Strava

```mermaid
flowchart LR
    A["Strava: user tập"] -->|webhook POST| B["/webhooks/strava\n(verify subscription_id)"]
    B --> C[("webhook_inbox\nstatus=pending")]
    C -->|inline ~2.5s HOẶC cron */5| D["ProcessOnce\nclaim-then-process (H2b)"]
    D -->|GetActivity| A
    D --> E[("activities\n(upsert)")]
    E --> F["recompute kèo + reward\n(1đ/km, DailyCap)"]
    F --> G[("ledger + reward_events")]
```

- **Real-time**: webhook xử lý inline ~2.5s. **Lưới an toàn**: Cloudflare Worker `*/5` vét event lỡ cửa sổ. **Backstop**: Vercel cron daily.
- Lỗi tạm thời (timeout cold-start) → tự requeue theo backoff (`next_attempt_at`).

## 6. Vận hành & bảo mật

- **Hạ tầng**: Vercel (`app.xox.vn`, region iad1) + Supabase (`odqgtnyjwpoyzhxqrciq`) + Cloudflare Worker cron.
- **Đã hardening**: sổ cái kép an toàn (`stake_release`), bỏ tin `email_verified` (verify GoTrue), gate secret prod + chặn KEK toàn-0, verify `subscription_id` webhook Strava, HTTP timeout + body limit, pgxpool config, graceful drain + panic recover worker, atomic create+join kèo, constant-time SePay key, JWT bắt buộc `exp`, rate-limit `/v1/auth` (binary), index/pagination/cache-control.
- **Điểm treo bảo mật**: Zalo id-spoofing (Graph API chặn IP ngoài VN nên backend tin `id` từ client — cần VN proxy); JWT `aud`/`iss` enforcement.

## 7. Biến môi trường quan trọng (Vercel)

`DATABASE_URL`, `JWT_SECRET`, `SEPAY_API_KEY`/`SEPAY_ACCOUNT_NO`/`SEPAY_BANK_CODE`, `STRAVA_CLIENT_ID`/`STRAVA_CLIENT_SECRET`/`STRAVA_VERIFY_TOKEN`/`STRAVA_SUBSCRIPTION_ID`, `TOKEN_CIPHER_KEY`, `ZALO_APP_ID`/`ZALO_SECRET_KEY`, `VITE_SUPABASE_URL`/`VITE_SUPABASE_ANON_KEY`.

> ⚠️ Đổi env trên Vercel PHẢI deploy tươi (git push) — nút "Redeploy" có thể tái dùng snapshot env cũ.
> ⚠️ Migration KHÔNG tự chạy khi deploy — phải áp tay (endpoint `/api/admin/migrate` hoặc Supabase trực tiếp).
