package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/ledger"
)

type Pack struct {
	Points   int64
	Bonus    int64
	PriceVND int64
}

// Tỷ giá 1 điểm = 1 VNĐ; bonus giữ tỷ lệ khuyến khích gói lớn như cũ.
var Packs = map[int64]Pack{
	100_000:   {Points: 100_000, Bonus: 0, PriceVND: 100_000},
	300_000:   {Points: 300_000, Bonus: 15_000, PriceVND: 300_000},
	500_000:   {Points: 500_000, Bonus: 40_000, PriceVND: 500_000},
	1_000_000: {Points: 1_000_000, Bonus: 120_000, PriceVND: 1_000_000},
}

type Service struct {
	pool      *pgxpool.Pool
	ledger    *ledger.PGStore
	apiKey    string
	accountNo string
	bankCode  string
	log       *slog.Logger
}

func NewService(pool *pgxpool.Pool, l *ledger.PGStore, apiKey, accountNo, bankCode string, log *slog.Logger) *Service {
	return &Service{pool: pool, ledger: l, apiKey: apiKey, accountNo: accountNo, bankCode: bankCode, log: log}
}

// CreateOrder tạo đơn pending và trả về link ảnh VietQR của SePay
func (s *Service) CreateOrder(ctx context.Context, userID, packPoints int64) (orderURL, appTransID string, err error) {
	pack, ok := Packs[packPoints]
	if !ok {
		return "", "", fmt.Errorf("gói %d điểm không tồn tại", packPoints)
	}

	appTransID = fmt.Sprintf("KEO%d%d", userID, time.Now().Unix()%100000)

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO point_purchases
			(user_id, pack_points, bonus_points, price_vnd, payment_provider, provider_txn_id)
		VALUES ($1, $2, $3, $4, 'sepay', $5)`,
		userID, pack.Points, pack.Bonus, pack.PriceVND, appTransID,
	); err != nil {
		return "", "", fmt.Errorf("create purchase: %w", err)
	}

	// https://qr.sepay.vn/img?acc={acc}&bank={bank}&amount={amount}&des={des}
	qrURL := fmt.Sprintf("https://qr.sepay.vn/img?acc=%s&bank=%s&amount=%d&des=%s",
		url.QueryEscape(s.accountNo),
		url.QueryEscape(s.bankCode),
		pack.PriceVND,
		url.QueryEscape(appTransID),
	)

	return qrURL, appTransID, nil
}

type SePayWebhookBody struct {
	ID             int64  `json:"id"`
	Gateway        string `json:"gateway"`
	TransactionDate string `json:"transactionDate"`
	AccountNumber  string `json:"accountNumber"`
	SubAccount     string `json:"subAccount"`
	Code           string `json:"code"`
	Content        string `json:"content"`
	TransferType   string `json:"transferType"`
	TransferAmount int64  `json:"transferAmount"`
	Accumulated    int64  `json:"accumulated"`
	Channel        string `json:"channel"`
	ReferenceCode  string `json:"referenceCode"`
}

func (s *Service) HandleCallback(ctx context.Context, apiKeyHeader string, body []byte) map[string]any {
	// Verify API Key
	if s.apiKey == "" {
		if os.Getenv("DEV_MODE") != "1" {
			s.log.Error("SePay API Key rỗng trên môi trường production!")
			return map[string]any{"success": false, "message": "unauthorized"}
		}
	} else if apiKeyHeader != "Apikey "+s.apiKey {
		s.log.Warn("SePay webhook sai apikey", "got", apiKeyHeader)
		return map[string]any{"success": false, "message": "unauthorized"}
	}

	var data SePayWebhookBody
	if err := json.Unmarshal(body, &data); err != nil {
		s.log.Error("SePay webhook lỗi JSON", "err", err)
		return map[string]any{"success": false, "message": "bad data"}
	}

	// Chỉ quan tâm tiền VÀO
	if data.TransferType != "in" {
		return map[string]any{"success": true, "message": "ignored out type"}
	}

	// Extract appTransID from Content
	content := strings.ToUpper(data.Content)
	
	// Tìm chuỗi bắt đầu bằng KEO
	var appTransID string
	words := strings.Fields(strings.ReplaceAll(content, "-", " "))
	for _, w := range words {
		if strings.HasPrefix(w, "KEO") {
			appTransID = w
			break
		}
	}

	if appTransID == "" {
		s.log.Warn("SePay webhook không tìm thấy KEO trong content", "content", content)
		return map[string]any{"success": true, "message": "no valid prefix found"} // Vẫn báo success để SePay không gửi lại
	}

	if err := s.complete(ctx, appTransID, data.TransferAmount, data.ReferenceCode); err != nil {
		s.log.Error("SePay webhook complete lỗi", "err", err, "transID", appTransID)
		// Return 500 error equivalent payload so SePay retries if it's a DB issue
		return map[string]any{"success": false, "message": err.Error()}
	}

	return map[string]any{"success": true, "message": "ok"}
}

func (s *Service) complete(ctx context.Context, appTransID string, amount int64, refCode string) error {
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
		WHERE payment_provider = 'sepay' AND provider_txn_id = $1
		FOR UPDATE`,
		appTransID,
	).Scan(&purchaseID, &userID, &packPts, &bonusPts, &priceVND, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("đơn %s không tồn tại", appTransID)
	}
	if err != nil {
		return fmt.Errorf("load purchase: %w", err)
	}

	if status == "paid" {
		return nil
	}
	if amount < priceVND {
		return fmt.Errorf("amount %d < price %d cho đơn %s", amount, priceVND, appTransID)
	}

	res, err := s.ledger.PostTx(ctx, tx,
		ledger.PurchaseRequest(userID, packPts+bonusPts, "sepay", appTransID))
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

	mux.HandleFunc("POST /webhooks/sepay", func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		
		apiKey := r.Header.Get("Authorization")
		
		w.Header().Set("Content-Type", "application/json")
		res := s.HandleCallback(r.Context(), apiKey, body)
		if success, _ := res["success"].(bool); !success {
			w.WriteHeader(http.StatusBadRequest)
		}
		json.NewEncoder(w).Encode(res)
	})
}
