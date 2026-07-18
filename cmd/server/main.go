// cmd/server nối toàn bộ backend KÈO thành một binary:
// API cho mobile + webhook Strava + callback ZaloPay + 2 background worker
// (settlement job, strava ingestion). Một binary cho gọn giai đoạn đầu;
// tách worker ra deployment riêng khi cần scale độc lập.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ingest"
	"github.com/hao/keo/ledger"
	"github.com/hao/keo/payment"
	"github.com/hao/keo/restapi"
	"github.com/hao/keo/reward"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	// Đảm bảo enum goal_type hỗ trợ daily_distance_km
	_, _ = pool.Exec(ctx, `ALTER TYPE goal_type ADD VALUE IF NOT EXISTS 'daily_distance_km'`)

	// Đảm bảo cấu trúc cột từ thiện và tài khoản quỹ
	_, _ = pool.Exec(ctx, `ALTER TABLE challenges ADD COLUMN IF NOT EXISTS is_charity boolean DEFAULT false`)
	_, _ = pool.Exec(ctx, `ALTER TABLE challenges ADD COLUMN IF NOT EXISTS charity_id integer DEFAULT 0`)
	_, _ = pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, created_at)
		VALUES 
			(1001, 'charity.smile@keo.vn', 'Quỹ Phẫu Thuật Nụ Cười', '', now()),
			(1002, 'charity.forest@keo.vn', 'Quỹ Trồng Rừng Gieo Mầm Xanh', '', now())
		ON CONFLICT (id) DO NOTHING
	`)

	// ===== Services =====
	ledgerStore := ledger.NewPGStore(pool)
	challengeStore := challenge.NewStore(pool, ledgerStore)

	tokenKey, err := base64.StdEncoding.DecodeString(envOr("TOKEN_CIPHER_KEY",
		base64.StdEncoding.EncodeToString(make([]byte, 32)))) // DEV: key 0 — prod BẮT BUỘC set
	if err != nil {
		return fmt.Errorf("decode TOKEN_CIPHER_KEY: %w", err)
	}
	localKMS, err := ingest.NewLocalKMS(tokenKey)
	if err != nil {
		return fmt.Errorf("local KMS: %w", err)
	}
	cph := ingest.NewEnvelopeCipher(localKMS)
	stravaClient := &ingest.HTTPStravaClient{
		Pool:         pool,
		Cipher:       cph,
		ClientID:     envOr("STRAVA_CLIENT_ID", "dev"),
		ClientSecret: envOr("STRAVA_CLIENT_SECRET", "dev"),
	}

	devMode := os.Getenv("DEV_MODE") == "1"

	// ===== Middlewares =====
	// Ngoài DEV_MODE, secret bắt buộc từ env: JWT_SECRET default nằm công khai
	// trong repo (forge token = chiếm mọi ví), SEPAY_API_KEY rỗng làm webhook
	// bỏ verify (POST giả chuyển khoản = mint điểm tự do).
	if !devMode {
		mustEnv("JWT_SECRET")
		mustEnv("SEPAY_API_KEY")
	}
	jwtSecret := []byte(envOr("JWT_SECRET", "dev-jwt-secret-do-not-use-in-prod"))
	authUserID := restapi.AuthMiddleware(jwtSecret, pool)
	
	var verifier ingest.AttestationVerifier
	if os.Getenv("DEV_MODE") == "1" {
		verifier = allowAllVerifier{}
		log.Warn("DEV_MODE bật: AttestationVerifier bị vô hiệu hóa (Cho phép mọi thiết bị)")
	} else {
		fbAppID := envOr("FIREBASE_APP_ID", "1:1234567890:android:abcdef123456")
		v, err := ingest.NewFirebaseAppCheckVerifier(fbAppID, log)
		if err != nil {
			return fmt.Errorf("khởi tạo Firebase App Check: %w", err)
		}
		verifier = v
	}

	// ===== Payment Gateway (SePay) =====
	sepayAPIKey := envOr("SEPAY_API_KEY", "")
	sepayAccountNo := envOr("SEPAY_ACCOUNT_NO", "000000000")
	sepayBankCode := envOr("SEPAY_BANK_CODE", "MB")
	
	paySvc := payment.NewService(pool, ledgerStore, sepayAPIKey, sepayAccountNo, sepayBankCode, log)
	healthSvc := ingest.NewHealthSyncService(pool, verifier)
	adminAuthUserID := restapi.AdminMiddleware(jwtSecret, pool)
	apiSrv := restapi.NewServer(pool, ledgerStore, challengeStore, authUserID, adminAuthUserID, jwtSecret)

	// ===== Background workers =====
	rewardSvc := reward.NewService(pool, ledgerStore)
	stravaWorker := ingest.NewStravaWorker(pool, stravaClient, log).WithRewards(rewardSvc)
	go stravaWorker.RunLoop(ctx, 5*time.Second)

	// ===== HTTP =====
	mux := http.NewServeMux()
	apiSrv.Routes(mux)
	
	paySvc.Routes(mux, authUserID)
	ingestMux := ingest.NewMux(pool, healthSvc, envOr("STRAVA_VERIFY_TOKEN", "dev-verify"), authUserID, stravaWorker)
	mux.Handle("/webhooks/", ingestMux)
	mux.Handle("/v1/health-sync", ingestMux)

	// OAuth redirect từ Strava sau khi user ủy quyền.
	mux.HandleFunc("GET /v1/oauth/strava/callback", func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string
		state := r.URL.Query().Get("state")
		if state != "" {
			tokenStr = state
		} else {
			// Fallback nếu có header
			authHeader := r.Header.Get("Authorization")
			if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				tokenStr = authHeader[7:]
			}
		}

		if tokenStr == "" {
			http.Error(w, "unauthorized: missing state token", http.StatusUnauthorized)
			return
		}

		userID, err := restapi.ValidateSupabaseJWT(r.Context(), tokenStr, jwtSecret, pool)
		if err != nil {
			log.Error("verify jwt error", "err", err)
			http.Error(w, "unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		if err := stravaClient.ExchangeCode(r.Context(), userID, r.URL.Query().Get("code")); err != nil {
			log.Error("strava exchange", "err", err)
			http.Error(w, "exchange failed", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`
			<html>
			<body style="background:#15171B;color:#FFF;font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;">
				<div style="text-align:center;">
					<h2 style="color:#CCFF00;">Kết nối Strava thành công!</h2>
					<p>Trình duyệt sẽ tự động quay lại ứng dụng sau 3 giây...</p>
					<script>
						setTimeout(function() {
							window.location.href = "/";
						}, 3000);
					</script>
				</div>
			</body>
			</html>
		`))
	})

	// ===== Background workers =====
	go func() {
		job := challenge.NewSettlementJob(challengeStore, log)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			if err := job.Run(ctx, time.Now(), 100); err != nil {
				log.Error("settlement job", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	addr := envOr("LISTEN_ADDR", ":8080")

	// ===== Dev mode: login nhanh + mô phỏng callback ZaloPay =====
	if os.Getenv("DEV_MODE") == "1" {
		apiSrv.RegisterDevRoutes(mux)
		// Mô phỏng ZaloPay bắn callback cho một đơn — đi ĐÚNG đường
		// HandleCallback thật (verify MAC, idempotent) chứ không cộng điểm tắt.
		mux.HandleFunc("POST /v1/dev/confirm-payment", func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				AppTransID string `json:"app_trans_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			var priceVND int64
			err := pool.QueryRow(r.Context(),
				`SELECT price_vnd FROM point_purchases WHERE payment_provider='zalopay' AND provider_txn_id=$1`,
				body.AppTransID).Scan(&priceVND)
			if err != nil {
				http.Error(w, "đơn không tồn tại", http.StatusNotFound)
				return
			}
			cb, _ := json.Marshal(map[string]any{
				"gateway": "DevMock",
				"transactionDate": time.Now().Format("2006-01-02 15:04:05"),
				"content": body.AppTransID,
				"transferType": "in",
				"transferAmount": priceVND,
			})
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(paySvc.HandleCallback(r.Context(), "", cb))
		})
		log.Warn("DEV_MODE bật: /v1/auth/dev-login và /v1/dev/confirm-payment đang mở")
	}

	// ===== Serve web UI (SPA) nếu có build =====
	if dist := envOr("WEB_DIST", ""); dist != "" {
		fileServer := http.FileServer(http.Dir(dist))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			p := filepath.Join(dist, filepath.Clean(r.URL.Path))
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(dist, "index.html")) // SPA fallback
		})
		log.Info("serving web UI", "dist", dist)
	}

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info("KÈO backend listening", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}


type allowAllVerifier struct{}

func (allowAllVerifier) Verify(context.Context, int64, string) error { return nil }

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "thiếu biến môi trường %s\n", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
