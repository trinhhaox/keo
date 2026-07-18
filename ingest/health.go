package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
)

// ===== Apple Health / Health Connect: sync từ mobile =====
//
// Cả hai nguồn này chỉ đọc được on-device (HealthKit không có REST API,
// Google Fit REST đã bị khai tử — thay bằng Health Connect, cũng on-device).
// App mobile đọc dữ liệu → gửi bucket summary theo ngày kèm attestation token.

// HealthBucket là tổng kết một (ngày, bộ môn) từ thiết bị.
type HealthBucket struct {
	Date       string  `json:"date"` // "2026-07-04" theo giờ VN
	Sport      string  `json:"sport"`
	Steps      int     `json:"steps"`
	DistanceKm float64 `json:"distance_km"`
	Sessions   int     `json:"sessions"`
}

// AttestationVerifier verify token App Attest (iOS) / Play Integrity (Android)
// — chặn request giả từ script. Implementation thật gọi API của Apple/Google.
type AttestationVerifier interface {
	Verify(ctx context.Context, userID int64, token string) error
}

type HealthSyncService struct {
	pool     *pgxpool.Pool
	verifier AttestationVerifier
}

func NewHealthSyncService(pool *pgxpool.Pool, v AttestationVerifier) *HealthSyncService {
	return &HealthSyncService{pool: pool, verifier: v}
}

// Sync ghi các bucket từ thiết bị. Ngữ nghĩa: OVERWRITE theo
// (user, source, sport, date) — sync lại là đè, không cộng dồn, nên client
// retry / gửi trùng / gửi lại số liệu đã sửa đều cho kết quả đúng.
func (s *HealthSyncService) Sync(ctx context.Context, userID int64, source, attestation string, buckets []HealthBucket) error {
	if source != ProviderGoogleFit && source != ProviderAppleHealth {
		return fmt.Errorf("source %q không sync qua endpoint này", source)
	}
	if err := s.verifier.Verify(ctx, userID, attestation); err != nil {
		return fmt.Errorf("attestation: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, b := range buckets {
		date, err := time.ParseInLocation("2006-01-02", b.Date, challenge.VNLocation)
		if err != nil {
			return fmt.Errorf("bucket date %q: %w", b.Date, err)
		}
		act := Activity{
			UserID: userID,
			Source: source,
			// ID tổng hợp: mỗi (user, source, sport, ngày) đúng một row —
			// đây chính là cơ chế overwrite qua UNIQUE(source, external_activity_id).
			ExternalID: fmt.Sprintf("%d:%s:%s", userID, b.Sport, b.Date),
			Sport:      b.Sport,
			DistanceM:  b.DistanceKm * 1000,
			Steps:      b.Steps,
			Sessions:   max(b.Sessions, 1),
			StartedAt:  date,
		}
		// started_at của bucket cố định theo ngày nên stale luôn rỗng ở đây;
		// vẫn recompute cho chắc nếu định nghĩa external ID đổi về sau.
		stale, err := upsertActivity(ctx, tx, act)
		if err != nil {
			return err
		}
		for _, p := range stale {
			if err := recompute(ctx, tx, p.userID, p.sport, source, p.date); err != nil {
				return err
			}
		}
		if err := recompute(ctx, tx, userID, b.Sport, source, date); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ===== HTTP wiring =====

// NewMux gắn các handler: webhook Strava validation + event ingest, và Fit sync.
func NewMux(pool *pgxpool.Pool, health *HealthSyncService, stravaVerifyToken string, authUserID func(*http.Request) (int64, error), worker *StravaWorker) *http.ServeMux {
	mux := http.NewServeMux()

	// Strava validation handshake: GET với hub.challenge phải echo lại.
	mux.HandleFunc("GET /webhooks/strava", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hub.verify_token") != stravaVerifyToken {
			http.Error(w, "bad verify token", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"hub.challenge": r.URL.Query().Get("hub.challenge"),
		})
	})

	// Strava event: chỉ enqueue rồi 200 ngay (yêu cầu <2s).
	mux.HandleFunc("POST /webhooks/strava", func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			slog.Error("read webhook body failed", "err", err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		
		slog.Info("received strava webhook payload", "payload", string(payload))

		if err := EnqueueStravaEvent(r.Context(), pool, payload); err != nil {
			slog.Error("enqueue strava event failed", "err", err, "payload", string(payload))
			// 5xx để Strava retry — event không được phép mất.
			http.Error(w, "enqueue failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

		slog.Info("strava webhook enqueued successfully")

		// Xử lý tức thì (bất đồng bộ) sự kiện vừa enqueue
		if worker != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				slog.Info("triggering real-time process for strava event")
				n, err := worker.ProcessOnce(ctx)
				if err != nil {
					slog.Error("real-time strava process failed", "err", err)
				} else {
					slog.Info("real-time strava process completed", "processed", n)
				}
			}()
		}
	})

	// Health/Fit sync từ mobile.
	mux.HandleFunc("POST /v1/health-sync", func(w http.ResponseWriter, r *http.Request) {
		userID, err := authUserID(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Source      string         `json:"source"`
			Attestation string         `json:"device_attestation"`
			Buckets     []HealthBucket `json:"buckets"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := health.Sync(r.Context(), userID, body.Source, body.Attestation, body.Buckets); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return mux
}
