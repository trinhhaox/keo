package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
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

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		panic("missing DATABASE_URL")
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
	if os.Getenv("DEV_MODE") == "1" {
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
	apiSrv := restapi.NewServer(pool, ledgerStore, challengeStore, authUserID, jwtSecret)

	// ===== HTTP =====
	mux := http.NewServeMux()
	apiSrv.Routes(mux)

	paySvc.Routes(mux, authUserID)
	ingestMux := ingest.NewMux(pool, healthSvc, envOr("STRAVA_VERIFY_TOKEN", "dev-verify"), authUserID)
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

	// Endpoint tạm thời để kiểm tra hoạt động Strava thực tế của user từ server Vercel
	mux.HandleFunc("GET /v1/dev/check-strava", func(w http.ResponseWriter, r *http.Request) {
		var userID int64
		var externalUserID string
		var accessTokenEnc []byte

		err := pool.QueryRow(r.Context(), `
			SELECT user_id, external_user_id, access_token_enc
			FROM user_integrations
			WHERE provider = 'strava'
			LIMIT 1`,
		).Scan(&userID, &externalUserID, &accessTokenEnc)
		if err != nil {
			http.Error(w, fmt.Sprintf("query DB lỗi: %v", err), http.StatusInternalServerError)
			return
		}

		// Giải mã token bằng cipherKey của server
		accessTokenBytes, err := cph.Decrypt(r.Context(), accessTokenEnc)
		if err != nil {
			http.Error(w, fmt.Sprintf("giải mã lỗi: %v", err), http.StatusInternalServerError)
			return
		}
		accessToken := string(accessTokenBytes)

		// Gọi Strava API
		req, _ := http.NewRequest("GET", "https://www.strava.com/api/v3/athlete/activities?per_page=5", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("gọi Strava lỗi: %v", err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	})

	// WithRewards: thiếu là thưởng km không bao giờ được cấp trên prod —
	// cron /api/cron/strava là đường ingest duy nhất ở môi trường serverless.
	stravaWorker = ingest.NewStravaWorker(pool, stravaClient, log).
		WithRewards(reward.NewService(pool, ledgerStore))
	settleJob = challenge.NewSettlementJob(challengeStore, log)

	// Migration từ runtime (bảo vệ bằng MIGRATE_KEY) — xem migrations/runner.go.
	migrations.RegisterMigrateRoute(mux, pool)

	// CRON Endpoints
	mux.HandleFunc("POST /api/cron/strava", func(w http.ResponseWriter, r *http.Request) {
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
