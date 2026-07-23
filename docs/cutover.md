# Pha 4 — Cutover sang ro.xox.vn (VPS)

> Chuyển các dịch vụ ngoài trỏ về domain mới, rồi ngừng Vercel. Làm SAU khi Pha 2
> (stack chạy) + Pha 3 (dữ liệu) xong và `https://ro.xox.vn` đã lên.

## 1. Google OAuth (đã làm khi tạo credential)
Đảm bảo trong Google Cloud → Credentials → OAuth client "RO Web":
- **Authorized JavaScript origins** có `https://ro.xox.vn`.
- (Redirect URI không bắt buộc với luồng ID token.)

## 2. Strava — đăng ký lại webhook về ro.xox.vn
Push subscription của Strava gắn với 1 callback URL cố định → phải xoá cái cũ (trỏ
app.xox.vn) và tạo mới. Chạy trên máy có `curl` (điền CLIENT_ID/SECRET/VERIFY_TOKEN thật):

```bash
CID=<STRAVA_CLIENT_ID>; CSEC=<STRAVA_CLIENT_SECRET>; VTOK=<STRAVA_VERIFY_TOKEN>

# 2a. Xem subscription hiện có → lấy {id}
curl -sG https://www.strava.com/api/v3/push_subscriptions \
  -d client_id=$CID -d client_secret=$CSEC

# 2b. Xoá subscription cũ (trỏ app.xox.vn)
curl -s -X DELETE "https://www.strava.com/api/v3/push_subscriptions/<OLD_ID>" \
  -d client_id=$CID -d client_secret=$CSEC

# 2c. Tạo subscription mới trỏ ro.xox.vn (app phải đang chạy để trả verify challenge)
curl -s -X POST https://www.strava.com/api/v3/push_subscriptions \
  -d client_id=$CID -d client_secret=$CSEC \
  -d callback_url=https://ro.xox.vn/webhooks/strava \
  -d verify_token=$VTOK
# → trả về {"id": <NEW_ID>}
```

Cập nhật `.env` trên VPS: `STRAVA_SUBSCRIPTION_ID=<NEW_ID>` rồi:
```bash
docker compose -f docker-compose.prod.yml up -d   # nạp env mới
```
> Webhook handler verify `subscription_id` như shared secret — sai id sẽ bị bỏ qua.

## 3. SePay — đổi webhook URL
Trong dashboard SePay → Webhook/Tích hợp: đổi URL nhận biến động số dư sang
`https://ro.xox.vn/webhooks/sepay` (giữ nguyên API key = `SEPAY_API_KEY`).

## 4. DNS
`ro.xox.vn` đã trỏ A-record về IP VPS (116.118.2.40) — đã xong.

## 5. Ngừng hạ tầng cũ
- **Cloudflare Worker cron**: đã GỠ khỏi repo (worker chạy in-process trên VPS). Nếu
  còn deploy trên Cloudflare: `wrangler delete` hoặc xoá worker `keo-strava-cron` trong
  dashboard (nó đang gọi app.xox.vn — vô hại nhưng thừa).
- **Vercel**: sau khi xác nhận ro.xox.vn chạy ổn vài ngày → xoá project hoặc gỡ domain
  `app.xox.vn` trong Vercel. `vercel.json` và `api/` đã gỡ khỏi repo.
- **Supabase**: sau khi chắc chắn dữ liệu đã sang VPS đầy đủ → có thể pause/xoá project.

## 6. Smoke test (sau cutover)
- [ ] `https://ro.xox.vn` mở được, cert hợp lệ.
- [ ] Đăng nhập **Google** → vào app, thấy đúng ví/kèo.
- [ ] Đăng nhập **Zalo** → OK.
- [ ] Tài khoản admin thấy tab Quản trị (đã set `is_admin`).
- [ ] Nạp điểm SePay (chuyển khoản test) → webhook cộng điểm.
- [ ] Kết nối Strava + tập thử → hoạt động về, tiến độ/điểm km cập nhật.
- [ ] Tạo kèo ngắn hạn → chờ settlement worker chốt (log `settlement job`).
