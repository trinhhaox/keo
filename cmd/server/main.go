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
	"strconv"
	"strings"
	"sync"
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

	// Cấu hình pool tường minh: default MaxConns = max(4, numCPU) quá thấp cho
	// API + webhook + 2 worker dùng chung, mà worker giữ conn suốt lúc gọi Strava.
	// Override bằng env DB_MAX_CONNS khi cần.
	poolCfg, err := pgxpool.ParseConfig(mustEnv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.MaxConns = int32(envInt("DB_MAX_CONNS", 20))
	poolCfg.MinConns = int32(envInt("DB_MIN_CONNS", 2))
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	devMode := os.Getenv("DEV_MODE") == "1"
	// Prod BẮT BUỘC set các secret nhạy cảm — mọi default đều nằm công khai trong repo:
	//   JWT_SECRET default = ai cũng forge được token → chiếm mọi ví.
	//   SEPAY_API_KEY rỗng = webhook bỏ verify → POST giả chuyển khoản = mint điểm tự do.
	//   TOKEN_CIPHER_KEY = KEK bọc refresh-token Strava; default toàn-0 = coi như plaintext.
	//   STRAVA_VERIFY_TOKEN / STRAVA_CLIENT_SECRET default "dev" = webhook giả + OAuth hỏng.
	if !devMode {
		mustEnv("JWT_SECRET")
		mustEnv("SEPAY_API_KEY")
		mustEnv("TOKEN_CIPHER_KEY")
		mustEnv("STRAVA_VERIFY_TOKEN")
		mustEnv("STRAVA_CLIENT_SECRET")
	}

	// DDL khởi động (autocommit — KHÔNG đưa vào migration được: runner bọc mỗi
	// file trong transaction, mà ALTER TYPE ADD VALUE không chạy trong tx). Trước
	// đây nuốt lỗi bằng `_, _ =` → schema thiếu cột mà app vẫn chạy, lỗi khó lần.
	// Giờ log rõ (IF NOT EXISTS nên chạy lại vô hại).
	ddlBoot(ctx, pool, log, "enum daily_distance_km",
		`ALTER TYPE goal_type ADD VALUE IF NOT EXISTS 'daily_distance_km'`)
	ddlBoot(ctx, pool, log, "cột is_charity",
		`ALTER TABLE challenges ADD COLUMN IF NOT EXISTS is_charity boolean DEFAULT false`)
	ddlBoot(ctx, pool, log, "cột charity_id",
		`ALTER TABLE challenges ADD COLUMN IF NOT EXISTS charity_id integer DEFAULT 0`)
	ddlBoot(ctx, pool, log, "seed tài khoản quỹ", `
		INSERT INTO users (id, email, display_name, password_hash, created_at)
		VALUES
			(1001, 'charity.smile@keo.vn', 'Quỹ Phẫu Thuật Nụ Cười', '', now()),
			(1002, 'charity.forest@keo.vn', 'Quỹ Trồng Rừng Gieo Mầm Xanh', '', now()),
			(1003, 'charity.organic@keo.vn', 'Quỹ Run Organic', '', now())
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
	// Prod không được chạy với KEK toàn-0: refresh-token Strava trong DB sẽ bị
	// "mã hóa" bằng khóa 0 = plaintext, ai đọc được DB là chiếm Strava mọi user.
	if !devMode && isAllZero(tokenKey) {
		return errors.New("TOKEN_CIPHER_KEY là khóa toàn-0 (mặc định DEV) — prod BẮT BUỘC đặt khóa 32-byte ngẫu nhiên")
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

	// ===== Middlewares =====
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
	// wg để drain worker trước khi pool.Close() (defer) — nếu không, đóng pool
	// lúc worker còn giữ transaction sẽ hỏng dở dang.
	var wg sync.WaitGroup
	rewardSvc := reward.NewService(pool, ledgerStore)
	stravaWorker := ingest.NewStravaWorker(pool, stravaClient, log).WithRewards(rewardSvc)
	supervise(ctx, &wg, log, "strava", func(c context.Context) {
		stravaWorker.RunLoop(c, 5*time.Second)
	})

	// ===== HTTP =====
	mux := http.NewServeMux()
	apiSrv.Routes(mux)
	
	paySvc.Routes(mux, authUserID)
	stravaSubID, _ := strconv.ParseInt(os.Getenv("STRAVA_SUBSCRIPTION_ID"), 10, 64)
	ingestMux := ingest.NewMux(pool, healthSvc, envOr("STRAVA_VERIFY_TOKEN", "dev-verify"), stravaSubID, authUserID, stravaWorker)
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
	supervise(ctx, &wg, log, "settlement", func(c context.Context) {
		job := challenge.NewSettlementJob(challengeStore, log)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			if err := job.Run(c, time.Now(), 100); err != nil {
				log.Error("settlement job", "err", err)
			}
			select {
			case <-c.Done():
				return
			case <-ticker.C:
			}
		}
	})

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
				// Asset Vite có hash trong tên (/assets/*) → cache vĩnh viễn.
				if strings.HasPrefix(r.URL.Path, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA fallback: index.html KHÔNG cache (mỗi deploy trỏ asset hash mới).
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
		})
		log.Info("serving web UI", "dist", dist)
	}

	// H7: chặn body khổng lồ (DoS cạn RAM) cho MỌI endpoint tại một chỗ. Webhook/
	// health-sync đã tự LimitReader 1MiB nên trần 2MiB ở đây trong suốt với chúng.
	// Rate-limit brute-force cho /v1/auth (30 req/phút/IP, burst 10). Chỉ hiệu
	// quả trên binary chạy dài; serverless/edge nên rate-limit tại Cloudflare.
	authLimiter := newRateLimiter(0.5, 10)
	supervise(ctx, &wg, log, "ratelimit-cleanup", func(c context.Context) {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-c.Done():
				return
			case <-ticker.C:
				authLimiter.cleanup(time.Now(), 10*time.Minute)
			}
		}
	})

	handler := secureHeaders(authLimiter.middleware(maxBodyBytes(mux, 2<<20), "/v1/auth"))

	// H1: đủ bộ timeout — ReadHeaderTimeout đơn lẻ không chặn slow-loris ở body,
	// request treo giữ goroutine vô hạn, và ghi response không có trần thời gian.
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
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
	// H4: HTTP đã dừng — chờ background worker thoát TRƯỚC khi defer pool.Close()
	// chạy, tránh đóng pool lúc worker còn giữ transaction dở dang.
	log.Info("HTTP dừng, chờ worker drain")
	wg.Wait()
	log.Info("worker đã drain — đóng pool")
	return nil
}

// supervise chạy một worker loop trong goroutine có: (1) recover panic + tự
// restart — net/http chỉ recover panic của handler, KHÔNG bảo vệ background
// goroutine, một panic là worker chết im lặng vĩnh viễn; (2) đăng ký WaitGroup
// để main drain sạch trước khi đóng pool.
func supervise(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, name string, loop func(context.Context)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error("worker panic — restart", "worker", name, "panic", r)
					}
				}()
				loop(ctx)
			}()
			// loop trả về: ctx hủy → thoát; panic → nghỉ ngắn rồi restart.
			if ctx.Err() == nil {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
		}
	}()
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

// secureHeaders gắn header bảo mật cơ bản cho mọi response. SAMEORIGIN (không
// DENY) để không chặn nhúng cùng origin (web UI có thể chạy trong webview).
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// maxBodyBytes bọc mọi request để giới hạn kích thước body — vượt trần thì lần
// đọc tiếp theo trả lỗi, handler tự trả 400. GET/không body: no-op.
func maxBodyBytes(next http.Handler, n int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ddlBoot chạy một câu DDL khởi động (autocommit), log lỗi thay vì nuốt.
func ddlBoot(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, step, sql string) {
	if _, err := pool.Exec(ctx, sql); err != nil {
		log.Warn("startup DDL lỗi (bỏ qua — có thể đã áp)", "step", step, "err", err)
	}
}

func isAllZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
