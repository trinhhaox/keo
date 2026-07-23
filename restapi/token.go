package restapi

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// mintAppToken tạo JWT nội bộ (HMAC) cho một danh tính đã xác thực (Zalo/Google).
//
// email_verified CHỈ được set true khi nguồn phát đã xác minh email THẬT (vd Google
// trả email_verified). Token này do backend tự ký nên downstream (ValidateSupabaseJWT)
// tin claim này an toàn để link user theo email — khác hẳn token Supabase trước đây
// (client ghi được user_metadata nên phải hỏi lại GoTrue).
//
// app_metadata.role=admin nhúng để FE hiện tab admin; server VẪN tự kiểm tra cột
// is_admin trong DB ở mọi API admin (AdminMiddleware) nên claim này chỉ phục vụ UX,
// không phải cổng bảo mật.
func (s *Server) mintAppToken(ctx context.Context, sub, email string, emailVerified bool, displayName, avatarURL string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   sub,
		"email": email,
		"user_metadata": map[string]interface{}{
			"full_name":  displayName,
			"avatar_url": avatarURL,
		},
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	if emailVerified {
		claims["email_verified"] = true
	}

	// Tra is_admin theo danh tính (supabase_id lưu cả "zalo:"/"google:" lẫn UUID cũ).
	// Lỗi/không có row (user chưa tồn tại lần đầu login) → coi như không admin.
	var isAdmin bool
	_ = s.pool.QueryRow(ctx, `SELECT is_admin FROM users WHERE supabase_id = $1`, sub).Scan(&isAdmin)
	if isAdmin {
		claims["app_metadata"] = map[string]interface{}{"role": "admin"}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}
