import { useEffect, useState } from "react";
import { Users, ShoppingBag, SlidersHorizontal, Plus, Edit2, Trash2, Check, X, Search, RefreshCw, AlertCircle, ArrowUpRight, ArrowDownRight, Tag } from "lucide-react";
import * as api from "./api.js";
import { T, MONO } from "./theme.js";

export default function AdminDashboard({ showToast }) {
  const [adminSubTab, setAdminSubTab] = useState("users"); // users | redemptions | shop
  const [users, setUsers] = useState([]);
  const [redemptions, setRedemptions] = useState([]);
  const [shopItems, setShopItems] = useState([]);
  const [loading, setLoading] = useState(false);
  
  // Search states
  const [userQuery, setUserQuery] = useState("");
  const [redeemQuery, setRedeemQuery] = useState("");

  // Modals state
  const [adjustingUser, setAdjustingUser] = useState(null); // { id, name, balance }
  const [adjustPoints, setAdjustPoints] = useState("");
  const [adjustReason, setAdjustReason] = useState("");
  const [processingRedeem, setProcessingRedeem] = useState(null); // { id, sku, user }
  const [redeemStatus, setRedeemStatus] = useState("fulfilled");
  const [fulfillmentText, setFulfillmentText] = useState("");
  const [editingItem, setEditingItem] = useState(null); // null (create) or item object
  const [itemForm, setItemForm] = useState({ sku: "", name: "", cost: "", stock: "", status: "active" });
  const [itemModalOpen, setItemModalOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const loadData = async () => {
    setLoading(true);
    try {
      if (adminSubTab === "users") {
        const data = await api.adminListUsers();
        setUsers(data);
      } else if (adminSubTab === "redemptions") {
        const data = await api.adminListRedemptions();
        setRedemptions(data);
      } else if (adminSubTab === "shop") {
        const data = await api.adminListShopItems();
        setShopItems(data);
      }
    } catch (err) {
      showToast(`Không thể tải dữ liệu: ${err.message}`, "error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [adminSubTab]);

  const handleAdjustPoints = async (e) => {
    e.preventDefault();
    const pts = parseInt(adjustPoints, 10);
    if (isNaN(pts) || pts === 0) {
      showToast("Số điểm điều chỉnh phải khác 0", "error");
      return;
    }
    setBusy(true);
    try {
      await api.adminAdjustUserPoints(adjustingUser.id, pts, adjustReason);
      showToast("Điều chỉnh điểm thành công!", "success");
      setAdjustingUser(null);
      setAdjustPoints("");
      setAdjustReason("");
      loadData();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleUpdateRedeemStatus = async (e) => {
    e.preventDefault();
    setBusy(true);
    try {
      let fulfillment = {};
      if (fulfillmentText.trim()) {
        try {
          fulfillment = JSON.parse(fulfillmentText);
        } catch {
          // Nếu không phải JSON, lưu dưới dạng string object
          fulfillment = { code: fulfillmentText };
        }
      }
      await api.adminUpdateRedemptionStatus(processingRedeem.id, redeemStatus, fulfillment);
      showToast("Cập nhật trạng thái đơn quà thành công!", "success");
      setProcessingRedeem(null);
      setFulfillmentText("");
      loadData();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleSaveShopItem = async (e) => {
    e.preventDefault();
    if (!itemForm.sku || !itemForm.name || !itemForm.cost) {
      showToast("Vui lòng điền đầy đủ SKU, Tên và Giá", "error");
      return;
    }
    const costPts = parseInt(itemForm.cost, 10);
    const stockQty = parseInt(itemForm.stock || "0", 10);
    if (isNaN(costPts) || costPts <= 0) {
      showToast("Giá điểm phải > 0", "error");
      return;
    }
    setBusy(true);
    try {
      const payload = {
        sku: itemForm.sku,
        name: itemForm.name,
        cost: costPts,
        stock: stockQty,
        status: itemForm.status
      };
      if (editingItem) {
        await api.adminUpdateShopItem(editingItem.id, payload);
        showToast("Cập nhật sản phẩm thành công!", "success");
      } else {
        await api.adminCreateShopItem(payload);
        showToast("Tạo sản phẩm mới thành công!", "success");
      }
      setItemModalOpen(false);
      setEditingItem(null);
      loadData();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleDeleteShopItem = async (id, name) => {
    if (!confirm(`Bạn có chắc chắn muốn xóa sản phẩm "${name}"?`)) return;
    setBusy(true);
    try {
      await api.adminDeleteShopItem(id);
      showToast("Đã xóa sản phẩm", "success");
      loadData();
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  const openEditItem = (item) => {
    setEditingItem(item);
    setItemForm({
      sku: item.sku,
      name: item.name,
      cost: item.cost.toString(),
      stock: item.stock.toString(),
      status: item.status
    });
    setItemModalOpen(true);
  };

  const openCreateItem = () => {
    setEditingItem(null);
    setItemForm({ sku: "", name: "", cost: "", stock: "100", status: "active" });
    setItemModalOpen(true);
  };

  // Filters
  const filteredUsers = users.filter(u => 
    u.display_name.toLowerCase().includes(userQuery.toLowerCase()) ||
    u.email.toLowerCase().includes(userQuery.toLowerCase())
  );

  const filteredRedemptions = redemptions.filter(r => 
    r.user_display_name.toLowerCase().includes(redeemQuery.toLowerCase()) ||
    r.item_sku.toLowerCase().includes(redeemQuery.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Title & Reload */}
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold uppercase tracking-wider flex items-center gap-2" style={{ color: T.text }}>
          <SlidersHorizontal size={20} className="text-[#CCFF00]" />
          Bảng Quản Trị
        </h2>
        <button
          onClick={loadData}
          disabled={loading}
          className="p-2.5 rounded-xl transition-all active:scale-95 hover:bg-white/5"
          style={{ background: T.card, border: `1px solid ${T.line}`, color: T.textDim }}
        >
          <RefreshCw size={14} className={loading ? "animate-spin" : ""} />
        </button>
      </div>

      {/* Sub Tabs Navigation */}
      <div className="flex rounded-2xl p-1 gap-1" style={{ background: T.bgDim, border: `1px solid ${T.line}` }}>
        {[
          { k: "users", label: "Thành viên", icon: Users },
          { k: "redemptions", label: "Đơn quà", icon: ShoppingBag },
          { k: "shop", label: "Cửa hàng", icon: Tag },
        ].map(t => {
          const Icon = t.icon;
          const active = adminSubTab === t.k;
          return (
            <button
              key={t.k}
              onClick={() => setAdminSubTab(t.k)}
              className="flex-1 py-3 px-2 rounded-xl text-xs font-bold transition-all flex items-center justify-center gap-1.5"
              style={{
                background: active ? T.card : "transparent",
                border: `1px solid ${active ? T.line : "transparent"}`,
                color: active ? T.brand : T.textDim,
              }}
            >
              <Icon size={14} />
              {t.label}
            </button>
          );
        })}
      </div>

      {/* Loading state */}
      {loading && (
        <div className="py-20 text-center text-sm font-semibold animate-pulse" style={{ color: T.brand }}>
          Đang tải dữ liệu...
        </div>
      )}

      {/* Tab: Users */}
      {!loading && adminSubTab === "users" && (
        <div className="space-y-4">
          <div className="relative">
            <Search size={16} className="absolute left-4 top-1/2 -translate-y-1/2" style={{ color: T.textDim }} />
            <input
              type="text"
              placeholder="Tìm theo tên hoặc email..."
              value={userQuery}
              onChange={e => setUserQuery(e.target.value)}
              className="w-full pl-11 pr-4 py-3 rounded-2xl text-sm"
              style={{ background: T.card, border: `1px solid ${T.line}`, color: T.text }}
            />
          </div>

          <div className="space-y-3">
            {filteredUsers.length === 0 ? (
              <div className="text-center py-10 text-xs" style={{ color: T.textDim }}>Không tìm thấy thành viên nào</div>
            ) : (
              filteredUsers.map(u => (
                <div key={u.id} className="rounded-2xl p-4 flex items-center justify-between gap-4" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-bold truncate" style={{ color: T.text }}>{u.display_name}</div>
                    <div className="text-[10px] truncate" style={{ color: T.textDim }}>ID: {u.id} · {u.email || "Không có email"}</div>
                    <div className="flex gap-4 mt-2">
                      <div className="text-xs">
                        <span style={{ color: T.textDim }}>Ví: </span>
                        <strong style={{ ...MONO, color: T.brand }}>{u.balance_available.toLocaleString()} ⭐</strong>
                      </div>
                      <div className="text-xs">
                        <span style={{ color: T.textDim }}>Khóa: </span>
                        <strong style={{ ...MONO, color: T.text }}>{u.balance_locked.toLocaleString()} ⭐</strong>
                      </div>
                    </div>
                  </div>
                  <button
                    onClick={() => setAdjustingUser(u)}
                    className="px-3.5 py-2 rounded-xl text-xs font-bold btn-neon shrink-0"
                    style={{ background: T.brand, color: T.bg }}
                  >
                    Bơm/Trừ
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* Tab: Redemptions */}
      {!loading && adminSubTab === "redemptions" && (
        <div className="space-y-4">
          <div className="relative">
            <Search size={16} className="absolute left-4 top-1/2 -translate-y-1/2" style={{ color: T.textDim }} />
            <input
              type="text"
              placeholder="Tìm theo tên hoặc SKU sản phẩm..."
              value={redeemQuery}
              onChange={e => setRedeemQuery(e.target.value)}
              className="w-full pl-11 pr-4 py-3 rounded-2xl text-sm"
              style={{ background: T.card, border: `1px solid ${T.line}`, color: T.text }}
            />
          </div>

          <div className="space-y-3">
            {filteredRedemptions.length === 0 ? (
              <div className="text-center py-10 text-xs" style={{ color: T.textDim }}>Không có đơn đổi quà nào</div>
            ) : (
              filteredRedemptions.map(r => (
                <div key={r.id} className="rounded-2xl p-4 space-y-3" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-xs font-bold" style={{ color: T.textDim }}>Đơn #{r.id}</div>
                      <div className="text-sm font-black mt-0.5" style={{ color: T.text }}>{r.item_sku}</div>
                      <div className="text-[11px] mt-1" style={{ color: T.textDim }}>Bởi: <span className="font-semibold text-white">{r.user_display_name}</span></div>
                    </div>
                    <div className="text-right">
                      <div className="text-sm font-bold" style={{ ...MONO, color: T.brand }}>-{r.cost_points.toLocaleString()} ⭐</div>
                      <div className="text-[10px] mt-1" style={{ color: T.textDim }}>{new Date(r.createdAt).toLocaleDateString("vi-VN")}</div>
                    </div>
                  </div>

                  {/* Fulfillment details */}
                  {r.fulfillment && Object.keys(r.fulfillment).length > 0 && (
                    <div className="rounded-xl p-2.5 text-[11px] leading-relaxed" style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.textDim }}>
                      <strong className="text-white">Thông tin gửi quà:</strong>
                      <pre className="mt-1 overflow-x-auto text-[10px] text-lime-400 font-mono">{JSON.stringify(r.fulfillment, null, 2)}</pre>
                    </div>
                  )}

                  {/* Status & Action */}
                  <div className="flex items-center justify-between pt-2 border-t" style={{ borderColor: T.line }}>
                    <div>
                      <span className="text-[10px] font-bold uppercase tracking-wider px-2.5 py-1 rounded-full"
                        style={{
                          background: r.status === "fulfilled" ? "rgba(52,199,89,0.1)" : r.status === "cancelled" ? "rgba(255,59,48,0.1)" : "rgba(204,255,0,0.1)",
                          color: r.status === "fulfilled" ? T.green : r.status === "cancelled" ? T.red : T.brand
                        }}
                      >
                        {r.status === "fulfilled" ? "Đã gửi quà" : r.status === "cancelled" ? "Đã hủy" : "Chờ xử lý"}
                      </span>
                    </div>
                    <button
                      onClick={() => {
                        setProcessingRedeem(r);
                        setRedeemStatus(r.status);
                        setFulfillmentText(r.fulfillment ? JSON.stringify(r.fulfillment) : "");
                      }}
                      className="px-3 py-1.5 rounded-lg text-[11px] font-bold transition-all active:scale-95"
                      style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                    >
                      Cập nhật
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* Tab: Shop/Catalog CRUD */}
      {!loading && adminSubTab === "shop" && (
        <div className="space-y-4">
          <button
            onClick={openCreateItem}
            className="w-full py-3.5 rounded-2xl font-bold text-sm flex items-center justify-center gap-1.5 btn-neon active:scale-95 transition-transform"
            style={{ background: T.brand, color: T.bg }}
          >
            <Plus size={16} />
            Thêm sản phẩm mới
          </button>

          <div className="space-y-3">
            {shopItems.length === 0 ? (
              <div className="text-center py-10 text-xs" style={{ color: T.textDim }}>Chưa có sản phẩm nào trong cửa hàng</div>
            ) : (
              shopItems.map(item => (
                <div key={item.id} className="rounded-2xl p-4 flex items-center justify-between gap-4" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-bold tracking-wider uppercase text-glow" style={{ color: T.brand }}>{item.sku}</span>
                      <span className="text-[10px] px-2 py-0.5 rounded-md"
                        style={{
                          background: item.status === "active" ? "rgba(52,199,89,0.08)" : "rgba(255,255,255,0.05)",
                          color: item.status === "active" ? T.green : T.textDim
                        }}
                      >
                        {item.status === "active" ? "Đang bán" : "Đã ẩn"}
                      </span>
                    </div>
                    <div className="text-sm font-bold mt-1 truncate" style={{ color: T.text }}>{item.name}</div>
                    <div className="flex gap-4 mt-1.5 text-xs text-glow">
                      <div>
                        <span style={{ color: T.textDim }}>Giá: </span>
                        <strong style={{ ...MONO, color: T.brand }}>{item.cost.toLocaleString()} ⭐</strong>
                      </div>
                      <div>
                        <span style={{ color: T.textDim }}>Tồn kho: </span>
                        <strong style={{ ...MONO, color: T.text }}>{item.stock}</strong>
                      </div>
                    </div>
                  </div>
                  <div className="flex gap-1.5 shrink-0">
                    <button
                      onClick={() => openEditItem(item)}
                      className="p-2 rounded-xl border transition-all active:scale-95"
                      style={{ background: T.bg, borderColor: T.line, color: T.text }}
                    >
                      <Edit2 size={13} />
                    </button>
                    <button
                      onClick={() => handleDeleteShopItem(item.id, item.name)}
                      className="p-2 rounded-xl border transition-all active:scale-95"
                      style={{ background: "rgba(255,59,48,0.05)", borderColor: "rgba(255,59,48,0.2)", color: T.red }}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* Modal: Bơm/Trừ Điểm (Adjust Points) */}
      {adjustingUser && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-6 backdrop-blur-md" style={{ background: "rgba(0,0,0,0.8)" }}>
          <div className="w-full max-w-[340px] rounded-3xl p-6 relative" style={{ background: T.card, border: `1px solid ${T.line}` }}>
            <h3 className="text-base font-bold mb-1" style={{ color: T.text }}>Điều chỉnh ví điểm</h3>
            <p className="text-xs mb-4" style={{ color: T.textDim }}>Thành viên: <strong className="text-white">{adjustingUser.display_name}</strong></p>
            
            <form onSubmit={handleAdjustPoints} className="space-y-4">
              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Số điểm thay đổi</label>
                <input
                  type="number"
                  required
                  placeholder="Ví dụ: +50000 hoặc -10000"
                  value={adjustPoints}
                  onChange={e => setAdjustPoints(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl text-sm font-bold"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text, ...MONO }}
                />
                <span className="text-[10px] mt-1 block" style={{ color: T.textDim }}>Nhập dấu âm (-) để trừ điểm của user</span>
              </div>
              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Lý do điều chỉnh</label>
                <input
                  type="text"
                  required
                  placeholder="Ví dụ: Cộng điểm bù sự kiện"
                  value={adjustReason}
                  onChange={e => setAdjustReason(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl text-sm"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                />
              </div>

              <div className="flex gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setAdjustingUser(null)}
                  className="flex-1 py-3 rounded-xl text-xs font-bold transition-all active:scale-95"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                >
                  Hủy
                </button>
                <button
                  type="submit"
                  disabled={busy}
                  className="flex-1 py-3 rounded-xl text-xs font-bold btn-neon active:scale-95 transition-transform"
                  style={{ background: T.brand, color: T.bg }}
                >
                  Xác nhận
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Modal: Cập nhật Trạng thái Đơn Quà */}
      {processingRedeem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-6 backdrop-blur-md" style={{ background: "rgba(0,0,0,0.8)" }}>
          <div className="w-full max-w-[340px] rounded-3xl p-6 relative" style={{ background: T.card, border: `1px solid ${T.line}` }}>
            <h3 className="text-base font-bold mb-1" style={{ color: T.text }}>Xử lý đơn đổi quà</h3>
            <p className="text-xs mb-4" style={{ color: T.textDim }}>Đơn #{processingRedeem.id} · <strong className="text-white">{processingRedeem.item_sku}</strong></p>

            <form onSubmit={handleUpdateRedeemStatus} className="space-y-4">
              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Trạng thái</label>
                <select
                  value={redeemStatus}
                  onChange={e => setRedeemStatus(e.target.value)}
                  className="w-full px-3 py-3 rounded-xl text-sm"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                >
                  <option value="created">Chờ xử lý</option>
                  <option value="fulfilled">Đã gửi quà (Thành công)</option>
                  <option value="cancelled">Hủy đơn (Không hoàn điểm)</option>
                </select>
              </div>

              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Thông tin vận đơn / Voucher (JSON/Text)</label>
                <textarea
                  placeholder='Ví dụ: {"code": "SPORT500K", "note": "Gửi qua Zalo"}'
                  value={fulfillmentText}
                  onChange={e => setFulfillmentText(e.target.value)}
                  rows={4}
                  className="w-full px-4 py-3 rounded-xl text-xs font-mono"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                />
              </div>

              <div className="flex gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setProcessingRedeem(null)}
                  className="flex-1 py-3 rounded-xl text-xs font-bold transition-all active:scale-95"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                >
                  Hủy
                </button>
                <button
                  type="submit"
                  disabled={busy}
                  className="flex-1 py-3 rounded-xl text-xs font-bold btn-neon active:scale-95 transition-transform"
                  style={{ background: T.brand, color: T.bg }}
                >
                  Cập nhật đơn
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Modal: Thêm / Sửa Sản phẩm (Shop Item Create/Edit) */}
      {itemModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-6 backdrop-blur-md" style={{ background: "rgba(0,0,0,0.8)" }}>
          <div className="w-full max-w-[340px] rounded-3xl p-6 relative" style={{ background: T.card, border: `1px solid ${T.line}` }}>
            <h3 className="text-base font-bold mb-4" style={{ color: T.text }}>
              {editingItem ? "Sửa thông tin sản phẩm" : "Thêm sản phẩm mới"}
            </h3>

            <form onSubmit={handleSaveShopItem} className="space-y-4 text-xs">
              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Mã SKU sản phẩm</label>
                <input
                  type="text"
                  required
                  placeholder="Ví dụ: voucher-sport-500k"
                  disabled={!!editingItem}
                  value={itemForm.sku}
                  onChange={e => setItemForm({ ...itemForm, sku: e.target.value })}
                  className="w-full px-4 py-3 rounded-xl text-sm font-bold"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                />
              </div>

              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Tên sản phẩm hiển thị</label>
                <input
                  type="text"
                  required
                  placeholder="Ví dụ: Voucher cửa hàng thể thao 500k"
                  value={itemForm.name}
                  onChange={e => setItemForm({ ...itemForm, name: e.target.value })}
                  className="w-full px-4 py-3 rounded-xl text-sm"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                />
              </div>

              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Giá điểm (VND)</label>
                  <input
                    type="number"
                    required
                    placeholder="480000"
                    value={itemForm.cost}
                    onChange={e => setItemForm({ ...itemForm, cost: e.target.value })}
                    className="w-full px-3 py-3 rounded-xl text-sm font-bold text-lime-400"
                    style={{ background: T.bg, border: `1px solid ${T.line}`, ...MONO }}
                  />
                </div>
                <div>
                  <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Số lượng tồn kho</label>
                  <input
                    type="number"
                    required
                    placeholder="100"
                    value={itemForm.stock}
                    onChange={e => setItemForm({ ...itemForm, stock: e.target.value })}
                    className="w-full px-3 py-3 rounded-xl text-sm font-bold"
                    style={{ background: T.bg, border: `1px solid ${T.line}`, ...MONO }}
                  />
                </div>
              </div>

              <div>
                <label className="block text-[10px] uppercase font-bold tracking-widest mb-1.5" style={{ color: T.textDim }}>Trạng thái bán</label>
                <select
                  value={itemForm.status}
                  onChange={e => setItemForm({ ...itemForm, status: e.target.value })}
                  className="w-full px-3 py-3 rounded-xl text-sm"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                >
                  <option value="active">Đang mở bán (Active)</option>
                  <option value="inactive">Tạm ẩn (Inactive)</option>
                </select>
              </div>

              <div className="flex gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setItemModalOpen(false)}
                  className="flex-1 py-3 rounded-xl text-xs font-bold transition-all active:scale-95"
                  style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}
                >
                  Hủy
                </button>
                <button
                  type="submit"
                  disabled={busy}
                  className="flex-1 py-3 rounded-xl text-xs font-bold btn-neon active:scale-95 transition-transform"
                  style={{ background: T.brand, color: T.bg }}
                >
                  Lưu lại
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
