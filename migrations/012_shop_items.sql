-- 012_shop_items.sql
-- Tạo bảng quản lý danh mục sản phẩm đổi thưởng của hệ thống.

CREATE TABLE IF NOT EXISTS shop_items (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sku         TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    cost_points BIGINT NOT NULL CHECK (cost_points > 0),
    stock       INT NOT NULL DEFAULT 0 CHECK (stock >= 0),
    status      TEXT NOT NULL DEFAULT 'active', -- 'active' | 'inactive'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Kích hoạt RLS cho bảng shop_items để bảo vệ dữ liệu.
ALTER TABLE shop_items ENABLE ROW LEVEL SECURITY;

-- Nhập dữ liệu mặc định ban đầu từ Catalog cứng trong code Go để đảm bảo tính nhất quán dữ liệu.
INSERT INTO shop_items (sku, name, cost_points, stock, status) VALUES
('soap-sinh-duoc', 'Xà bông Sinh Dược', 39000, 100, 'active'),
('voucher-sport-500k', 'Voucher cửa hàng thể thao 500k', 480000, 50, 'active'),
('gear-trail-shoes', 'Giày chạy bộ trail', 2500000, 10, 'active'),
('ticket-hn-marathon', 'Vé Marathon Hà Nội 2026 · 21km', 900000, 20, 'active'),
('ticket-sg-night-run', 'Vé Night Run Sài Gòn · 10km', 600000, 30, 'active')
ON CONFLICT (sku) DO NOTHING;
