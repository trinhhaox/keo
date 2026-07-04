// Package payment xử lý mua điểm qua cổng thanh toán (ZaloPay).
//
// Luồng: app gọi POST /v1/wallet/purchase → tạo đơn pending + gọi ZaloPay
// lấy order_url → user thanh toán trong app ZaloPay → ZaloPay bắn callback
// → verify MAC → cộng điểm qua ledger (idempotent) → đánh dấu paid.
//
// Callback là nguồn sự thật duy nhất về việc "đã trả tiền" — app KHÔNG bao
// giờ tự báo thành công. Đây đúng là pattern callback ZaloPay kinh điển:
// verify chữ ký, xử lý idempotent, trả về nhanh.
package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/ledger"
)

// Pack là gói điểm bán trong app. Nguồn sự thật về giá nằm server-side —
// client chỉ gửi pack_points, không bao giờ gửi giá.
type Pack struct {
	Points   int64
	Bonus    int64
	PriceVND int64
}

var Packs = map[int64]Pack{
	100:  {Points: 100, Bonus: 0, PriceVND: 100_000},
	300:  {Points: 300, Bonus: 15, PriceVND: 300_000},
	500:  {Points: 500, Bonus: 40, PriceVND: 500_000},
	1000: {Points: 1000, Bonus: 120, PriceVND: 1_000_000},
}

// Gateway trừu tượng hóa ZaloPay Create Order API — implementation thật gọi
// https://openapi.zalopay.vn/v2/create với MAC key1; test dùng fake.
type Gateway interface {
	CreateOrder(ctx context.Context, appTransID string, amountVND int64, description string) (orderURL string, err error)
}

type Service struct {
	pool    *pgxpool.Pool
	ledger  *ledger.PGStore
	gateway Gateway
	key2    string // MAC key cho callback (khác key1 dùng cho create order)
}

func NewService(pool *pgxpool.Pool, l *ledger.PGStore, gw Gateway, key2 string) *Service {
	return &Service{pool: pool, ledger: l, gateway: gw, key2: key2}
}

// CreateOrder tạo đơn mua điểm: ghi point_purchases pending rồi lấy order_url.
func (s *Service) CreateOrder(ctx context.Context, userID, packPoints int64) (orderURL, appTransID string, err error) {
	pack, ok := Packs[packPoints]
	if !ok {
		return "", "", fmt.Errorf("gói %d điểm không tồn tại", packPoints)
	}
	// ZaloPay yêu cầu app_trans_id prefix yymmdd và duy nhất theo ngày.
	appTransID = fmt.Sprintf("%s_keo_%d", time.Now().Format("060102"), time.Now().UnixNano())

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO point_purchases
			(user_id, pack_points, bonus_points, price_vnd, payment_provider, provider_txn_id)
		VALUES ($1, $2, $3, $4, 'zalopay', $5)`,
		userID, pack.Points, pack.Bonus, pack.PriceVND, appTransID,
	); err != nil {
		return "", "", fmt.Errorf("create purchase: %w", err)
	}

	orderURL, err = s.gateway.CreateOrder(ctx, appTransID,
		pack.PriceVND, fmt.Sprintf("KEO - goi %d diem", pack.Points))
	if err != nil {
		// Đơn pending không có callback sẽ tự chết già — có thể dọn bằng job.
		return "", "", fmt.Errorf("gateway: %w", err)
	}
	return orderURL, appTransID, nil
}

// callbackBody là envelope callback v2 của ZaloPay:
// data = JSON string, mac = hex(hmac_sha256(key2, data)).
type callbackBody struct {
	Data string `json:"data"`
	MAC  string `json:"mac"`
	Type int    `json:"type"`
}

type callbackData struct {
	AppTransID string `json:"app_trans_id"`
	Amount     int64  `json:"amount"`
	ZPTransID  int64  `json:"zp_trans_id"`
}

// HandleCallback verify + hoàn tất đơn. Trả về body JSON đúng format ZaloPay
// mong đợi: return_code 1 = đã nhận, đừng bắn lại; khác 1 = sẽ retry.
//
// Idempotent hai lớp: FOR UPDATE + check status='paid' xử lý callback trùng
// tuần tự; idempotency key của ledger (purchase:zalopay:{app_trans_id}) là
// chốt chặn cuối nếu vẫn có kịch bản lọt qua.
func (s *Service) HandleCallback(ctx context.Context, body []byte) map[string]any {
	var cb callbackBody
	if err := json.Unmarshal(body, &cb); err != nil {
		return map[string]any{"return_code": -1, "return_message": "bad json"}
	}
	if !s.verifyMAC(cb.Data, cb.MAC) {
		// MAC sai = không phải ZaloPay. Không xử lý, không lộ thông tin.
		return map[string]any{"return_code": -1, "return_message": "mac not equal"}
	}
	var data callbackData
	if err := json.Unmarshal([]byte(cb.Data), &data); err != nil {
		return map[string]any{"return_code": -1, "return_message": "bad data"}
	}

	if err := s.complete(ctx, data); err != nil {
		// Lỗi tạm (DB...) → return_code 0 để ZaloPay retry; event không được mất.
		return map[string]any{"return_code": 0, "return_message": err.Error()}
	}
	return map[string]any{"return_code": 1, "return_message": "success"}
}

func (s *Service) complete(ctx context.Context, data callbackData) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		purchaseID, userID, packPts, bonusPts, priceVND int64
		status                                          string
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, pack_points, bonus_points, price_vnd, status
		FROM point_purchases
		WHERE payment_provider = 'zalopay' AND provider_txn_id = $1
		FOR UPDATE`,
		data.AppTransID,
	).Scan(&purchaseID, &userID, &packPts, &bonusPts, &priceVND, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("đơn %s không tồn tại", data.AppTransID)
	}
	if err != nil {
		return fmt.Errorf("load purchase: %w", err)
	}

	if status == "paid" {
		return nil // callback trùng — đã xử lý, trả success cho ZaloPay ngừng bắn
	}
	if data.Amount != priceVND {
		// Số tiền lệch = có vấn đề nghiêm trọng, không cộng điểm, giữ pending
		// để điều tra thủ công.
		return fmt.Errorf("amount %d != price %d cho đơn %s", data.Amount, priceVND, data.AppTransID)
	}

	res, err := s.ledger.PostTx(ctx, tx,
		ledger.PurchaseRequest(userID, packPts+bonusPts, "zalopay", data.AppTransID))
	if err != nil {
		return fmt.Errorf("post ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE point_purchases
		SET status = 'paid', ledger_txn_id = $1, paid_at = now()
		WHERE id = $2`,
		res.TxnID, purchaseID,
	); err != nil {
		return fmt.Errorf("mark paid: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Service) verifyMAC(data, mac string) bool {
	h := hmac.New(sha256.New, []byte(s.key2))
	h.Write([]byte(data))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(mac))
}

// Routes gắn endpoint mua điểm + callback vào mux.
func (s *Service) Routes(mux *http.ServeMux, authUserID func(*http.Request) (int64, error)) {
	mux.HandleFunc("POST /v1/wallet/purchase", func(w http.ResponseWriter, r *http.Request) {
		userID, err := authUserID(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			PackPoints int64 `json:"pack_points"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		orderURL, appTransID, err := s.CreateOrder(r.Context(), userID, body.PackPoints)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"order_url":    orderURL,
			"app_trans_id": appTransID,
		})
	})

	mux.HandleFunc("POST /callbacks/zalopay", func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.HandleCallback(r.Context(), body))
	})
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	total := 0
	for {
		n, err := r.Body.Read(tmp)
		total += n
		if total > 1<<20 {
			return nil, fmt.Errorf("body too large")
		}
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}
