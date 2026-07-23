# RO. — Cược điểm với chính mình

Nền tảng thử thách thể dục cho thị trường Việt Nam: người dùng **cược điểm**
vào cam kết tập luyện (đi bộ, chạy, bơi, đạp xe, gym). Về đích thì nhận lại
điểm cược **cộng phần chia từ quỹ điểm của người bỏ cuộc**; điểm dùng để đổi
vật phẩm thể thao, voucher, vé giải chạy.

Tiến độ được xác thực tự động từ **Strava** (webhook, tin cậy cao),
**Apple Health** và **Google Fit / Health Connect** (sync từ mobile) —
không check-in tay.

> **Thiết kế pháp lý:** điểm chảy MỘT chiều — tiền → điểm → hàng hóa/voucher,
> không bao giờ quy đổi ngược thành tiền mặt — để tránh bị xếp vào cá cược.
> Đây là ràng buộc sản phẩm, không phải chi tiết kỹ thuật.

## Chạy nhanh

```bash
docker compose up --build
# UI + API: http://localhost:8080  (DEV_MODE bật sẵn)
```

Hoặc chạy tay:

```bash
# 1. Postgres 16 + migrations
DATABASE_URL=postgres://postgres:test@localhost:5432/keo ./scripts/migrate.sh

# 2. Web UI
cd web && npm install && npm run build && cd ..

# 3. Server (một binary serve cả API lẫn UI)
DEV_MODE=1 DATABASE_URL=postgres://postgres:test@localhost:5432/keo \
  WEB_DIST=./web/dist go run ./cmd/server
```

`DEV_MODE=1` mở hai endpoint dev: `/v1/auth/dev-login` (tạo user không cần
auth thật) và `/v1/dev/confirm-payment` (mô phỏng callback ZaloPay — đi đúng
đường verify MAC thật, không cộng điểm tắt).

## Kiến trúc

```
web/        React (Vite) — UI phiếu cược, gọi API same-origin
api/        HTTP layer: ví, kèo, tiến độ, đổi thưởng
payment/    ZaloPay: tạo đơn + callback (verify HMAC, idempotent)
ingest/     Strava webhook inbox + worker, Health/Fit sync, recompute tiến độ
challenge/  Vòng đời kèo: join nguyên tử, sinh kỳ đánh giá, settlement job
reward/     Điểm thưởng: check-in hàng ngày +1đ, +1đ/km đi bộ-chạy bộ (Strava)
ledger/     Sổ cái double-entry — MỌI biến động điểm đi qua đây
migrations/ SQL thuần PostgreSQL 15+ (AlloyDB-compatible)
cmd/server/ Một binary: API + UI + 2 background worker
```

Ba nguyên tắc xuyên suốt:

1. **Double-entry ledger** — không bao giờ `UPDATE balance` trực tiếp; mỗi
   giao dịch là tập bút toán tổng bằng 0. Reconciliation = 4 câu SQL bất biến.
2. **Idempotency bằng UNIQUE constraint** — webhook/callback bắn trùng,
   user double-tap, job chạy lại: tất cả vô hại theo thiết kế.
3. **Recompute thay vì cộng dồn** — tiến độ luôn tính lại từ bảng
   `activities`, nên Strava sửa/xóa hoạt động hay Health sync đè số liệu
   đều cho kết quả đúng.

Luật chơi mặc định: đạt ≥ **80%** số kỳ là về đích (`pass_ratio`), chờ **48h**
sau khi hết hạn mới chốt sổ (`grace_hours` — dữ liệu GPS về muộn), phí nền
tảng **10%** quỹ tịch thu (`fee_bps`), phần dư chia nguyên dồn vào phí để
giữ bất biến tổng-bằng-0.

**Tỷ giá điểm: 1 điểm = 1 VNĐ** (nạp 100.000đ chuyển khoản = 100.000 điểm,
gói lớn có bonus).

**Điểm thưởng** (điểm nguyên, cộng thẳng vào ví qua txn `reward_payout`):
check-in mỗi ngày +1đ (`POST /v1/checkins`, 1 lần/ngày giờ VN); mỗi km
đi bộ/chạy bộ từ Strava +1đ — cấp MỘT lần theo hoạt động lúc ingest,
sửa/xóa hoạt động sau đó không top-up cũng không thu hồi (chống farm
bằng edit, tránh âm ví khi điểm đã tiêu). **Trần thưởng 100đ/ngày**
(`reward.DailyCap`, enforce bằng counter `reward_daily` + row lock —
GPS spoof cũng không vượt được).

## Test

```bash
# Unit (không cần DB)
go test ./...

# Integration (cần Postgres đã chạy migrations)
LEDGER_TEST_DSN=postgres://postgres:test@localhost:5432/keo go test ./... -count=1
```

Test suite tự chứa — chạy lặp lại không cần reset DB. Các test đáng đọc:
`TestIntegrationUserJourney` (trọn hành trình qua HTTP: nạp điểm → tạo kèo →
đổi thưởng), `TestIntegrationStravaFlow` (create/update/delete webhook),
`TestIntegrationNoNegativeBalance` (10 goroutine tranh tiêu một số dư),
`TestIntegrationFullLifecycle` (vào kèo → settlement → verify từng số dư).

## Trước khi lên production

- [x] Thay auth `X-User-ID` bằng JWT / OAuth.
- [x] ZaloPay merchant credential thật (thay `zaloPayStub` trong `cmd/server`).
- [x] `TOKEN_CIPHER_KEY` từ secret manager; cân nhắc envelope encryption KMS.
- [x] App Attest / Play Integrity verifier thật (interface `AttestationVerifier`).
- [x] Partition bảng `activities` theo tháng (bảng có thể phình rất to).
- [x] Tắt `DEV_MODE`.
