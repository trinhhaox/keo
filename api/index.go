package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ingest"
	"github.com/hao/keo/ledger"
	"github.com/hao/keo/migrations"
	"github.com/hao/keo/payment"
	"github.com/hao/keo/restapi"
	"github.com/hao/keo/reward"
)

var (
	pool         *pgxpool.Pool
	globalMux    *http.ServeMux
	stravaWorker *ingest.StravaWorker
	settleJob    *challenge.SettlementJob
	once         sync.Once
	log          *slog.Logger
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// allowAllVerifier is used for local dev/testing
type allowAllVerifier struct{}

func (allowAllVerifier) Verify(ctx context.Context, userID int64, token string) error { return nil }

func initApp() {
	log = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	devMode := os.Getenv("DEV_MODE") == "1"

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		panic("missing DATABASE_URL")
	}

	// Ngoài DEV_MODE, secret BẮT BUỘC từ env: JWT_SECRET default nằm công khai
	// trong repo (forge token = chiếm mọi ví), SEPAY_API_KEY rỗng làm webhook
	// bỏ verify (POST giả chuyển khoản = mint điểm). Guard này ở cmd/server đã có;
	// api/index.go mới là entrypoint chạy thật trên Vercel nên phải lặp lại.
	if !devMode {
		for _, k := range []string{"JWT_SECRET", "SEPAY_API_KEY"} {
			if os.Getenv(k) == "" {
				panic(fmt.Sprintf("thiếu biến môi trường bắt buộc %s (ngoài DEV_MODE)", k))
			}
		}
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		panic(fmt.Sprintf("parse db url: %v", err))
	}
	// Supabase Pooler (Transaction mode) does not support prepared statements.
	// Use simple protocol to avoid "prepared statement already exists" errors.
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		panic(fmt.Sprintf("connect db: %v", err))
	}

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
		base64.StdEncoding.EncodeToString(make([]byte, 32))))
	if err != nil {
		panic(fmt.Sprintf("decode TOKEN_CIPHER_KEY: %v", err))
	}
	localKMS, err := ingest.NewLocalKMS(tokenKey)
	if err != nil {
		panic(fmt.Sprintf("local KMS: %v", err))
	}
	cph := ingest.NewEnvelopeCipher(localKMS)
	stravaClient := &ingest.HTTPStravaClient{
		Pool:         pool,
		Cipher:       cph,
		ClientID:     envOr("STRAVA_CLIENT_ID", "dev"),
		ClientSecret: envOr("STRAVA_CLIENT_SECRET", "dev"),
	}

	// ===== Middlewares =====
	secretStr := envOr("JWT_SECRET", "dev-jwt-secret-do-not-use-in-prod")
	jwtSecret, err := base64.StdEncoding.DecodeString(secretStr)
	if err != nil {
		jwtSecret = []byte(secretStr)
	}
	authUserID := restapi.AuthMiddleware(jwtSecret, pool)

	var verifier ingest.AttestationVerifier
	if devMode {
		verifier = allowAllVerifier{}
		log.Warn("DEV_MODE bật: AttestationVerifier bị vô hiệu hóa")
	} else {
		fbAppID := envOr("FIREBASE_APP_ID", "1:1234567890:android:abcdef123456")
		v, err := ingest.NewFirebaseAppCheckVerifier(fbAppID, log)
		if err != nil {
			panic(fmt.Sprintf("Firebase App Check: %v", err))
		}
		verifier = v
	}

	// ===== Payment Gateway =====
	sepayAPIKey := envOr("SEPAY_API_KEY", "")
	sepayAccountNo := envOr("SEPAY_ACCOUNT_NO", "000000000")
	sepayBankCode := envOr("SEPAY_BANK_CODE", "MB")

	paySvc := payment.NewService(pool, ledgerStore, sepayAPIKey, sepayAccountNo, sepayBankCode, log)
	healthSvc := ingest.NewHealthSyncService(pool, verifier)
	adminAuthUserID := restapi.AdminMiddleware(jwtSecret, pool)
	apiSrv := restapi.NewServer(pool, ledgerStore, challengeStore, authUserID, adminAuthUserID, jwtSecret)

	// ===== HTTP =====
	mux := http.NewServeMux()
	apiSrv.Routes(mux)

	// WithRewards: thiếu là thưởng km không bao giờ được cấp trên prod —
	// cron /api/cron/strava là đường ingest duy nhất ở môi trường serverless.
	stravaWorker = ingest.NewStravaWorker(pool, stravaClient, log).
		WithRewards(reward.NewService(pool, ledgerStore))
	settleJob = challenge.NewSettlementJob(challengeStore, log)

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



	// Migration từ runtime (bảo vệ bằng MIGRATE_KEY) — xem migrations/runner.go.
	migrations.RegisterMigrateRoute(mux, pool)

	// CRON Endpoints
	mux.HandleFunc("/api/cron/strava", func(w http.ResponseWriter, r *http.Request) {
		// Vercel Cron headers check can be added here if needed
		ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
		defer cancel()
		
		processed := 0
		for {
			n, err := stravaWorker.ProcessOnce(ctx)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if n == 0 {
				break // Queue empty
			}
			processed += n
		}
		w.Write([]byte(fmt.Sprintf("Processed %d strava webhooks", processed)))
	})

	mux.HandleFunc("POST /api/cron/settle", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
		defer cancel()

		if err := settleJob.Run(ctx, time.Now(), 100); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write([]byte("Settlement check OK"))
	})

	globalMux = mux
}

// Handler is the Vercel Go Serverless Function entrypoint
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(initApp)
	globalMux.ServeHTTP(w, r)
}
