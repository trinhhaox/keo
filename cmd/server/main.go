// cmd/server nối toàn bộ backend KÈO thành một binary:
// API cho mobile + webhook Strava + callback ZaloPay + 2 background worker
// (settlement job, strava ingestion). Một binary cho gọn giai đoạn đầu;
// tách worker ra deployment riêng khi cần scale độc lập.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/api"
	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ingest"
	"github.com/hao/keo/ledger"
	"github.com/hao/keo/payment"
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

	// ===== Services =====
	ledgerStore := ledger.NewPGStore(pool)
	challengeStore := challenge.NewStore(pool, ledgerStore)

	tokenKey, err := base64.StdEncoding.DecodeString(envOr("TOKEN_CIPHER_KEY",
		base64.StdEncoding.EncodeToString(make([]byte, 32)))) // DEV: key 0 — prod BẮT BUỘC set
	if err != nil {
		return fmt.Errorf("decode TOKEN_CIPHER_KEY: %w", err)
	}
	cph, err := ingest.NewAESGCMCipher(tokenKey)
	if err != nil {
		return fmt.Errorf("cipher: %w", err)
	}
	stravaClient := &ingest.HTTPStravaClient{
		Pool:         pool,
		Cipher:       cph,
		ClientID:     envOr("STRAVA_CLIENT_ID", "dev"),
		ClientSecret: envOr("STRAVA_CLIENT_SECRET", "dev"),
	}

	// TODO: thay bằng session/JWT thật. Skeleton đọc X-User-ID cho dev.
	authUserID := func(r *http.Request) (int64, error) {
		return strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	}
	// TODO: verify App Attest / Play Integrity thật.
	verifier := allowAllVerifier{}

	paySvc := payment.NewService(pool, ledgerStore, zaloPayStub{log: log}, envOr("ZALOPAY_KEY2", "dev-key2"))
	healthSvc := ingest.NewHealthSyncService(pool, verifier)
	apiSrv := api.NewServer(pool, ledgerStore, challengeStore, authUserID)

	// ===== HTTP =====
	mux := http.NewServeMux()
	apiSrv.Routes(mux)
	paySvc.Routes(mux, authUserID)
	ingestMux := ingest.NewMux(pool, healthSvc, envOr("STRAVA_VERIFY_TOKEN", "dev-verify"), authUserID)
	mux.Handle("/webhooks/", ingestMux)
	mux.Handle("/v1/health-sync", ingestMux)

	// OAuth redirect từ Strava sau khi user ủy quyền.
	mux.HandleFunc("GET /oauth/strava/callback", func(w http.ResponseWriter, r *http.Request) {
		userID, err := authUserID(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := stravaClient.ExchangeCode(r.Context(), userID, r.URL.Query().Get("code")); err != nil {
			log.Error("strava exchange", "err", err)
			http.Error(w, "exchange failed", http.StatusBadGateway)
			return
		}
		w.Write([]byte("Đã kết nối Strava. Quay lại app để tiếp tục."))
	})

	// ===== Background workers =====
	go ingest.NewStravaWorker(pool, stravaClient, log).RunLoop(ctx, 5*time.Second)
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
		key2 := envOr("ZALOPAY_KEY2", "dev-key2")
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
			data, _ := json.Marshal(map[string]any{
				"app_trans_id": body.AppTransID, "amount": priceVND,
				"zp_trans_id": time.Now().UnixNano(),
			})
			h := hmac.New(sha256.New, []byte(key2))
			h.Write(data)
			cb, _ := json.Marshal(map[string]any{
				"data": string(data), "mac": hex.EncodeToString(h.Sum(nil)), "type": 1,
			})
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(paySvc.HandleCallback(r.Context(), cb))
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

// zaloPayStub: thay bằng client gọi https://openapi.zalopay.vn/v2/create
// (MAC key1) khi có credential merchant thật.
type zaloPayStub struct{ log *slog.Logger }

func (z zaloPayStub) CreateOrder(_ context.Context, appTransID string, amountVND int64, desc string) (string, error) {
	z.log.Info("zalopay stub create order", "app_trans_id", appTransID, "amount", amountVND, "desc", desc)
	return "https://sb-openapi.zalopay.vn/pay/" + appTransID, nil
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
