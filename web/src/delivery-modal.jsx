// Modal nhập thông tin giao hàng khi đổi quà vật lý.
import { useState } from "react";
import { T, MONO } from "./theme.js";
import { Label } from "./ui-primitives.jsx";

export default function DeliveryModal({ item, busy, onClose, onConfirm }) {
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [address, setAddress] = useState("");
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");   // lỗi validation inline thay cho alert() native

  const handleSubmit = (e) => {
    e.preventDefault();
    if (!name.trim() || !phone.trim() || !address.trim()) {
      setErr("Vui lòng điền đầy đủ Họ tên, Số điện thoại và Địa chỉ nhận hàng.");
      return;
    }
    setErr("");
    onConfirm({ name, phone, address, note });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={onClose}>
      <form className="w-full max-w-sm rounded-3xl p-6 relative overflow-y-auto max-h-[85vh] scale-in"
        style={{ background: T.card, border: `1px solid ${T.line}` }}
        onClick={e => e.stopPropagation()} onSubmit={handleSubmit}>
        <h2 className="text-xl font-black mb-4 uppercase tracking-wider text-center" style={{ color: T.text }}>Thông tin giao hàng</h2>
        <div className="text-sm font-semibold mb-4 text-center" style={{ color: T.brand }}>
          Đổi quà: {item.name} ({Number(item.cost).toLocaleString("vi-VN")} điểm)
        </div>

        <Label htmlFor="dl-name">Họ tên người nhận</Label>
        <input id="dl-name" type="text" autoComplete="name" value={name} onChange={e => setName(e.target.value)} required
          placeholder="Nhập họ và tên"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-3"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label htmlFor="dl-phone">Số điện thoại</Label>
        <input id="dl-phone" type="tel" inputMode="tel" autoComplete="tel" value={phone} onChange={e => setPhone(e.target.value)} required
          placeholder="Nhập số điện thoại"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-3"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label htmlFor="dl-address">Địa chỉ nhận hàng</Label>
        <textarea id="dl-address" autoComplete="street-address" value={address} onChange={e => setAddress(e.target.value)} required rows={3}
          placeholder="Số nhà, tên đường, phường/xã, quận/huyện, tỉnh/thành phố"
          className="w-full px-3 py-2 rounded-xl text-sm font-semibold outline-none mb-3 resize-none"
          style={{ background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label htmlFor="dl-note">Ghi chú thêm (Size, mùi hương, v.v.)</Label>
        <input id="dl-note" type="text" value={note} onChange={e => setNote(e.target.value)}
          placeholder="Ví dụ: Size M, Mùi hương Bạc Hà"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-5"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        {err && (
          <div className="text-xs font-semibold mb-3 px-3 py-2 rounded-lg" role="alert" aria-live="polite"
            style={{ background: "rgba(255,59,48,0.1)", color: T.red, border: `1px solid ${T.red}33` }}>
            {err}
          </div>
        )}
        <div className="flex gap-3">
          <button type="button" onClick={onClose} disabled={busy}
            className="flex-1 py-3 rounded-xl font-bold text-xs uppercase tracking-widest text-center active:scale-[.98] transition-transform"
            style={{ background: "rgba(255,255,255,0.05)", border: `1px solid ${T.line}`, color: T.textDim }}>
            Hủy
          </button>
          <button type="submit" disabled={busy}
            className="flex-1 py-3 rounded-xl font-bold text-xs uppercase tracking-widest text-center btn-neon active:scale-[.98] transition-transform"
            style={{ background: T.brand, color: T.bg, opacity: busy ? 0.6 : 1 }}>
            Xác nhận đổi
          </button>
        </div>
      </form>
    </div>
  );
}
