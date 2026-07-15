// Package migrations nhúng các file SQL vào binary để chạy migration từ chính
// runtime serverless (endpoint /api/admin/migrate) — môi trường duy nhất nhìn
// thấy DATABASE_URL production (biến sensitive trên Vercel, không pull về máy
// dev được). Nguồn sự thật vẫn là các file *.sql; embed chỉ là cách vận chuyển.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
