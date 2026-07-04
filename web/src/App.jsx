import { useCallback, useEffect, useState } from "react";
import * as api from "./api.js";

// ===== Design tokens (giữ nguyên từ prototype v3) =====
const T = {
  ink: "#15171B", paper: "#F1F3F1", card: "#FFFFFF",
  brand: "#FFD338", brandDark: "#E8B800",
  green: "#149A52", red: "#E5484D", strava: "#FC4C02",
  gray: "#7A7F87", line: "#E4E6E4",
};
const MONO = { fontFamily: "'IBM Plex Mono', monospace" };

const SPORTS = {
  walk: { label: "Đi bộ", icon: "🚶" }, run: { label: "Chạy bộ", icon: "🏃" },
  swim: { label: "Bơi lội", icon: "🏊" }, bike: { label: "Đạp xe", icon: "🚴" },
  gym: { label: "Gym", icon: "🏋️" },
};
const SOURCES = {
  strava: { label: "Strava", icon: "🟠" },
  google_fit: { label: "Google Fit", icon: "🟢" },
  apple_health: { label: "Apple Health", icon: "🍎" },
};
const GOALS = {
  daily_steps: { label: "bước/ngày", sports: ["walk"] },
  weekly_distance_km: { label: "km/tuần", sports: ["run", "bike", "swim"] },
  weekly_sessions: { label: "buổi/tuần", sports: ["gym", "swim"] },
};
const SKU_ICONS = { "voucher-sport-500k": "🎟️", "gear-trail-shoes": "👟", "ticket-hn-marathon": "🏅", "ticket-sg-night-run": "🌉" };
const PACKS = [
  { pts: 100, price: "100.000đ", bonus: 0 }, { pts: 300, price: "300.000đ", bonus: 15 },
  { pts: 500, price: "500.000đ", bonus: 40 }, { pts: 1000, price: "1.000.000đ", bonus: 120 },
];

const fmtP = (n) => Number(n).toLocaleString("vi-VN") + " điểm";
const daysLeft = (endAt) => Math.max(0, Math.ceil((new Date(endAt) - Date.now()) / 86400000));

function Notch({ side }) {
  return <div className="absolute w-4 h-4 rounded-full"
    style={{ background: T.paper, top: "50%", [side]: -8, transform: "translateY(-50%)" }} />;
}

function SourceBadge({ source }) {
  const s = SOURCES[source] || { label: source, icon: "❓" };
  const isStrava = source === "strava";
  return <span className="inline-flex items-center gap-1 text-[10px] font-semibold px-2 py-0.5 rounded-full"
    style={{ background: isStrava ? "#FEEDE5" : T.paper, color: isStrava ? T.strava : T.gray }}>
    {s.icon} {s.label}{isStrava ? " · webhook ⚡" : ""}
  </span>;
}

// ===== Màn đăng nhập (dev) =====
function Login({ onDone }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const go = async () => {
    if (!name.trim()) return;
    setBusy(true); setErr("");
    try { await api.devLogin(name.trim()); onDone(); }
    catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  };
  return (
    <div className="flex-1 flex flex-col items-center justify-center px-8">
      <div className="text-5xl mb-2" style={{ fontFamily: "'Archivo Black', sans-serif", color: T.ink }}>
        KÈO<span style={{ color: T.brandDark }}>.</span>
      </div>
      <div className="text-xs uppercase tracking-widest mb-8" style={{ color: T.gray }}>Cược điểm với chính mình</div>
      <input value={name} onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && go()}
        placeholder="Tên của bạn" autoFocus
        className="w-full px-4 py-3.5 rounded-2xl text-[15px] font-semibold outline-none mb-3 text-center"
        style={{ background: T.card, color: T.ink }} />
      <button onClick={go} disabled={busy}
        className="w-full py-3.5 rounded-2xl font-bold text-[15px] active:scale-[.98] transition-transform"
        style={{ background: T.brand, color: T.ink, opacity: busy ? 0.6 : 1 }}>
        {busy ? "Đang vào..." : "Vào sàn kèo"}
      </button>
      {err && <div className="text-xs mt-3" style={{ color: T.red }}>{err}</div>}
      <div className="text-[10px] mt-6 text-center leading-relaxed" style={{ color: T.gray }}>
        Đăng nhập dev — tạo user mới qua /v1/auth/dev-login.<br />Bản thật thay bằng OTP/JWT.
      </div>
    </div>
  );
}

// ===== Thẻ kèo (phiếu cược) =====
function ChallengeCard({ c, onJoin }) {
  const s = SPORTS[c.sport] || { icon: "🏆" };
  const pot = c.stake_points * c.participants;
  return (
    <div className="relative rounded-2xl mb-4" style={{ background: T.card, boxShadow: "0 1px 4px rgba(21,23,27,.06)" }}>
      <div className="p-4 pb-3 flex items-start gap-3">
        <div className="w-11 h-11 rounded-xl flex items-center justify-center text-xl shrink-0" style={{ background: T.paper }}>{s.icon}</div>
        <div className="min-w-0">
          <div className="text-[15px] font-bold leading-tight" style={{ color: T.ink }}>{c.title}</div>
          <div className="text-xs mt-1 mb-1.5" style={{ color: T.gray }}>
            còn {daysLeft(c.end_at)} ngày · {c.participants} người
          </div>
          <SourceBadge source={c.source} />
        </div>
      </div>
      <div className="relative mx-4" style={{ borderTop: `2px dashed ${T.line}` }}><Notch side="left" /><Notch side="right" /></div>
      <div className="p-4 pt-3 flex items-end justify-between">
        <div>
          <div className="text-[10px] uppercase tracking-widest" style={{ color: T.gray }}>Điểm cược</div>
          <div className="text-lg font-bold" style={{ ...MONO, color: T.ink }}>{c.stake_points}</div>
          <div className="text-[11px] mt-0.5" style={{ color: T.green }}>Về đích: hoàn cược + chia quỹ người rớt</div>
        </div>
        <div className="text-right">
          <div className="text-[10px] uppercase tracking-widest mb-1" style={{ color: T.gray }}>Quỹ {fmtP(pot)}</div>
          {c.joined
            ? <span className="inline-block text-xs font-bold px-3 py-2 rounded-full" style={{ background: T.paper, color: T.gray }}>Đã tham gia</span>
            : <button onClick={() => onJoin(c)} className="text-sm font-bold px-4 py-2 rounded-full active:scale-95 transition-transform"
                style={{ background: T.brand, color: T.ink }}>Vào kèo</button>}
        </div>
      </div>
    </div>
  );
}

// ===== Kèo của tôi =====
function MyChallengeCard({ c, onSync, busy }) {
  const s = SPORTS[c.sport] || { icon: "🏆" };
  const pct = c.periods_total ? Math.round((c.periods_passed / c.periods_total) * 100) : 0;
  const settled = c.status !== "active";
  const won = c.status === "completed";
  return (
    <div className="rounded-2xl p-4 mb-4" style={{ background: T.card, boxShadow: "0 1px 4px rgba(21,23,27,.06)" }}>
      <div className="flex items-center gap-3 mb-2">
        <div className="w-10 h-10 rounded-xl flex items-center justify-center text-lg" style={{ background: T.paper }}>{s.icon}</div>
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold leading-tight" style={{ color: T.ink }}>{c.title}</div>
          <div className="text-xs" style={{ color: T.gray }}>
            {settled ? "đã chốt sổ" : `còn ${daysLeft(c.end_at)} ngày`} · cược {fmtP(c.stake_points)}
          </div>
        </div>
        {settled && <span className="text-xs font-bold px-2.5 py-1 rounded-full shrink-0"
          style={{ background: won ? "#E7F5EC" : "#FDEBEC", color: won ? T.green : T.red }}>
          {won ? "✓ Về đích" : "✗ Rớt kèo"}
        </span>}
      </div>
      <div className="mb-2"><SourceBadge source={c.source} /></div>

      {!settled && (c.source === "strava" ? (
        <div className="rounded-xl px-3 py-2.5 mb-3 text-[11px] font-semibold" style={{ background: "#FEEDE5", color: T.strava }}>
          ⚡ Hoạt động Strava tự đẩy về qua webhook — không cần đồng bộ tay
        </div>
      ) : (
        <div className="flex items-center justify-between rounded-xl px-3 py-2.5 mb-3" style={{ background: T.paper }}>
          <div className="text-[12px] font-semibold" style={{ color: T.ink }}>
            Dữ liệu từ {SOURCES[c.source]?.label || c.source}
          </div>
          <button onClick={() => onSync(c)} disabled={busy}
            className="text-xs font-bold px-3 py-1.5 rounded-full active:scale-95 transition-transform"
            style={{ background: T.ink, color: T.brand, opacity: busy ? 0.6 : 1 }}>
            ⟳ Đồng bộ (demo)
          </button>
        </div>
      ))}

      <div className="relative h-4 rounded-full overflow-hidden mb-2" style={{ background: T.paper }}>
        <div className="absolute inset-y-0 left-0 rounded-full transition-all duration-500"
          style={{
            width: `${Math.min(pct, 100)}%`,
            background: won || pct >= 80 ? T.green : T.brand,
            backgroundImage: "repeating-linear-gradient(135deg, rgba(21,23,27,.12) 0 6px, transparent 6px 12px)",
          }} />
        <div className="absolute inset-y-0 right-1 flex items-center text-[10px]">🏁</div>
      </div>
      <div className="text-xs" style={{ color: pct >= 80 ? T.green : T.gray }}>
        Đạt {c.periods_passed}/{c.periods_total} kỳ ({pct}%) · cần ≥80% để về đích
      </div>
    </div>
  );
}

// ===== Modal vào kèo =====
function JoinModal({ c, wallet, busy, onConfirm, onClose }) {
  if (!c) return null;
  const enough = wallet.available >= c.stake_points;
  const src = SOURCES[c.source] || { label: c.source, icon: "" };
  return (
    <div className="absolute inset-0 z-30 flex items-end" style={{ background: "rgba(21,23,27,.55)" }} onClick={onClose}>
      <div className="w-full rounded-t-3xl p-5 pb-8" style={{ background: T.card }} onClick={(e) => e.stopPropagation()}>
        <div className="w-10 h-1 rounded-full mx-auto mb-4" style={{ background: T.line }} />
        <div className="text-lg font-bold mb-1" style={{ color: T.ink }}>Chốt kèo?</div>
        <div className="text-sm mb-4" style={{ color: T.gray }}>{c.title}</div>
        <div className="rounded-2xl p-4 mb-3 space-y-2" style={{ background: T.paper }}>
          <Row label="Điểm cược (khóa trong ví)" value={fmtP(c.stake_points)} />
          <Row label="Không đạt cam kết" value={`mất ${fmtP(c.stake_points)}`} color={T.red} />
          <Row label="Xác thực tiến độ" value={`${src.icon} ${src.label}`} />
        </div>
        <div className="text-[11px] mb-4 leading-relaxed" style={{ color: T.gray }}>
          Điểm của người không hoàn thành chia đều cho người về đích (sau 10% phí).
          Điểm chỉ dùng đổi vật phẩm, voucher, vé giải chạy — <b>không quy đổi ngược thành tiền mặt</b>.
        </div>
        <button disabled={!enough || busy} onClick={() => onConfirm(c)}
          className="w-full py-3.5 rounded-2xl font-bold text-[15px] active:scale-[.98] transition-transform"
          style={{ background: enough ? T.brand : T.line, color: enough ? T.ink : T.gray, opacity: busy ? 0.6 : 1 }}>
          {enough ? (busy ? "Đang chốt..." : `Đặt cược ${fmtP(c.stake_points)}`) : "Không đủ điểm — mua thêm ở tab Ví"}
        </button>
      </div>
    </div>
  );
}

function Row({ label, value, color }) {
  return <div className="flex justify-between text-sm gap-3">
    <span style={{ color: T.gray }}>{label}</span>
    <span className="font-bold text-right" style={{ ...MONO, color: color || T.ink }}>{value}</span>
  </div>;
}

// ===== Tạo kèo =====
function CreateSheet({ open, busy, onClose, onCreate }) {
  const [sport, setSport] = useState("run");
  const [goalType, setGoalType] = useState("weekly_distance_km");
  const [goal, setGoal] = useState(20);
  const [days, setDays] = useState(30);
  const [stake, setStake] = useState(200);
  const [source, setSource] = useState("strava");
  if (!open) return null;
  const pickSport = (k) => {
    setSport(k);
    const gt = Object.entries(GOALS).find(([, v]) => v.sports.includes(k));
    if (gt) setGoalType(gt[0]);
  };
  return (
    <div className="absolute inset-0 z-30 flex items-end" style={{ background: "rgba(21,23,27,.55)" }} onClick={onClose}>
      <div className="w-full rounded-t-3xl p-5 pb-8 max-h-[88%] overflow-y-auto" style={{ background: T.card }} onClick={(e) => e.stopPropagation()}>
        <div className="w-10 h-1 rounded-full mx-auto mb-4" style={{ background: T.line }} />
        <div className="text-lg font-bold mb-4" style={{ color: T.ink }}>Tạo kèo mới</div>

        <Label>Bộ môn</Label>
        <div className="flex flex-wrap gap-2 mb-4">
          {Object.entries(SPORTS).map(([k, v]) => (
            <Chip key={k} active={sport === k} onClick={() => pickSport(k)}>{v.icon} {v.label}</Chip>
          ))}
        </div>

        <Label>Kiểu mục tiêu</Label>
        <div className="flex flex-wrap gap-2 mb-4">
          {Object.entries(GOALS).map(([k, v]) => (
            <Chip key={k} active={goalType === k} onClick={() => setGoalType(k)}>{v.label}</Chip>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-3 mb-4">
          <div>
            <Label>Mục tiêu ({GOALS[goalType].label})</Label>
            <input type="number" value={goal} onChange={(e) => setGoal(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.paper, color: T.ink }} />
          </div>
          <div>
            <Label>Thời hạn (ngày)</Label>
            <input type="number" value={days} onChange={(e) => setDays(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.paper, color: T.ink }} />
          </div>
        </div>

        <Label>Nguồn xác thực</Label>
        <div className="flex gap-2 mb-4">
          {Object.entries(SOURCES).map(([k, v]) => (
            <Chip key={k} active={source === k} onClick={() => setSource(k)}>{v.icon} {v.label}</Chip>
          ))}
        </div>

        <Label>Điểm cược mỗi người</Label>
        <div className="flex gap-2 mb-5">
          {[100, 200, 500, 1000].map((v) => (
            <button key={v} onClick={() => setStake(v)}
              className="flex-1 py-2.5 rounded-xl text-[13px] font-bold"
              style={{ ...MONO, background: stake === v ? T.brand : T.paper, color: T.ink }}>{v}</button>
          ))}
        </div>

        <button disabled={busy}
          onClick={() => onCreate({
            title: `${SPORTS[sport].label} ${goal.toLocaleString("vi-VN")} ${GOALS[goalType].label}`,
            sport, goal_type: goalType, goal_value: goal, source,
            stake_points: stake, duration_days: days,
          })}
          className="w-full py-3.5 rounded-2xl font-bold text-[15px] active:scale-[.98] transition-transform"
          style={{ background: T.brand, color: T.ink, opacity: busy ? 0.6 : 1 }}>
          {busy ? "Đang tạo..." : `Tạo kèo · cược ${fmtP(stake)}`}
        </button>
      </div>
    </div>
  );
}

const Label = ({ children }) => (
  <div className="text-xs font-bold uppercase tracking-widest mb-2" style={{ color: T.gray }}>{children}</div>
);
const Chip = ({ active, onClick, children }) => (
  <button onClick={onClick} className="px-3 py-2 rounded-full text-sm font-semibold"
    style={{ background: active ? T.ink : T.paper, color: active ? T.brand : T.ink }}>{children}</button>
);

// ===== App =====
export default function App() {
  const [loggedIn, setLoggedIn] = useState(!!api.currentUserID());
  const [tab, setTab] = useState("discover");
  const [wallet, setWallet] = useState({ available: 0, locked: 0 });
  const [challenges, setChallenges] = useState([]);
  const [mine, setMine] = useState([]);
  const [shop, setShop] = useState([]);
  const [txs, setTxs] = useState([]);
  const [joining, setJoining] = useState(null);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState(null);

  const showToast = (msg) => { setToast(msg); setTimeout(() => setToast(null), 2600); };

  const load = useCallback(async () => {
    try {
      const [w, cs, m, sh, tx] = await Promise.all([
        api.getWallet(), api.listChallenges(), api.myChallenges(), api.getShop(), api.getTransactions(),
      ]);
      setWallet(w); setChallenges(cs); setMine(m); setShop(sh); setTxs(tx);
    } catch (e) {
      if (e.status === 401) { api.logout(); setLoggedIn(false); }
      else showToast(`Lỗi tải dữ liệu: ${e.message}`);
    }
  }, []);

  useEffect(() => { if (loggedIn) load(); }, [loggedIn, load]);

  const act = async (fn, okMsg) => {
    setBusy(true);
    try { await fn(); await load(); if (okMsg) showToast(okMsg); }
    catch (e) { showToast(e.status === 402 ? "Không đủ điểm — mua thêm ở tab Ví ⭐" : `Lỗi: ${e.message}`); }
    finally { setBusy(false); }
  };

  const totalPot = challenges.reduce((s, c) => s + c.stake_points * c.participants, 0);

  return (
    <div className="min-h-screen flex justify-center" style={{ background: "#DEE1DE", fontFamily: "'Be Vietnam Pro', sans-serif" }}>
      <div className="relative w-full max-w-[400px] min-h-screen flex flex-col" style={{ background: T.paper }}>
        {!loggedIn ? <Login onDone={() => setLoggedIn(true)} /> : (<>
          {/* Header */}
          <div className="px-5 pt-6 pb-4 flex items-center justify-between" style={{ background: T.ink }}>
            <div>
              <div className="text-2xl leading-none" style={{ fontFamily: "'Archivo Black', sans-serif", color: T.brand }}>
                KÈO<span style={{ color: "#fff" }}>.</span>
              </div>
              <div className="text-[10px] mt-1 uppercase tracking-widest" style={{ color: "#9BA1A8" }}>Cược điểm với chính mình</div>
            </div>
            <button onClick={() => setTab("wallet")} className="text-right rounded-xl px-3 py-2" style={{ background: "rgba(255,255,255,.08)" }}>
              <div className="text-[10px] uppercase tracking-widest" style={{ color: "#9BA1A8" }}>Ví điểm</div>
              <div className="text-sm font-bold" style={{ ...MONO, color: "#fff" }}>{Number(wallet.available).toLocaleString("vi-VN")} ⭐</div>
            </button>
          </div>

          {/* Nội dung */}
          <div className="flex-1 overflow-y-auto px-4 pt-4 pb-28">
            {tab === "discover" && (<>
              <div className="rounded-2xl p-4 mb-5" style={{ background: T.ink }}>
                <div className="text-[10px] uppercase tracking-widest mb-1" style={{ color: "#9BA1A8" }}>Tổng quỹ điểm đang treo</div>
                <div className="text-3xl font-bold" style={{ ...MONO, color: T.brand }}>
                  {totalPot.toLocaleString("vi-VN")} <span className="text-base">điểm</span>
                </div>
                <div className="text-[11px] mt-1" style={{ color: "#9BA1A8" }}>
                  Bỏ cuộc mất điểm cược · về đích chia quỹ · đổi điểm lấy đồ thể thao & vé giải chạy
                </div>
              </div>
              <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Kèo đang mở</div>
              {challenges.length === 0 && <Empty>Chưa có kèo nào trên sàn. Bấm “+ Tạo kèo” để mở kèo đầu tiên!</Empty>}
              {challenges.map((c) => <ChallengeCard key={c.id} c={c} onJoin={setJoining} />)}
            </>)}

            {tab === "mine" && (<>
              <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Kèo của tôi ({mine.length})</div>
              {mine.length === 0 && <Empty>Bạn chưa vào kèo nào. Qua tab Khám phá để chọn thử thách.</Empty>}
              {mine.map((c) => (
                <MyChallengeCard key={c.challenge_id} c={c} busy={busy}
                  onSync={(mc) => act(
                    () => api.syncHealthDemo(mc.source, mc.sport, mc.goal_type, mc.goal_value),
                    "Đã đồng bộ dữ liệu hôm nay qua /v1/health-sync ✓")} />
              ))}
            </>)}

            {tab === "shop" && (<>
              <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Đổi điểm lấy thưởng</div>
              {shop.map((i) => {
                const enough = wallet.available >= i.cost;
                return (
                  <div key={i.sku} className="flex items-center gap-3 rounded-2xl p-3.5 mb-3" style={{ background: T.card }}>
                    <div className="w-12 h-12 rounded-xl flex items-center justify-center text-2xl shrink-0" style={{ background: T.paper }}>
                      {SKU_ICONS[i.sku] || "🎁"}
                    </div>
                    <div className="min-w-0 flex-1 text-[14px] font-bold leading-tight" style={{ color: T.ink }}>{i.name}</div>
                    <button disabled={!enough || busy}
                      onClick={() => act(() => api.redeem(i.sku), `Đã đổi: ${i.name} 🎉`)}
                      className="shrink-0 text-xs font-bold px-3 py-2 rounded-full active:scale-95 transition-transform"
                      style={{ ...MONO, background: enough ? T.brand : T.paper, color: enough ? T.ink : T.gray }}>
                      {Number(i.cost).toLocaleString("vi-VN")} điểm
                    </button>
                  </div>
                );
              })}
            </>)}

            {tab === "wallet" && (<>
              <div className="rounded-2xl p-4 mb-4" style={{ background: T.card }}>
                <div className="flex justify-between mb-1">
                  <div>
                    <div className="text-[10px] uppercase tracking-widest" style={{ color: T.gray }}>Khả dụng</div>
                    <div className="text-xl font-bold" style={{ ...MONO, color: T.ink }}>{fmtP(wallet.available)}</div>
                  </div>
                  <div className="text-right">
                    <div className="text-[10px] uppercase tracking-widest" style={{ color: T.gray }}>Đang khóa cược 🔒</div>
                    <div className="text-xl font-bold" style={{ ...MONO, color: T.brandDark }}>{fmtP(wallet.locked)}</div>
                  </div>
                </div>
                <div className="text-[10px]" style={{ color: T.gray }}>Điểm dùng để cược và đổi thưởng, không rút thành tiền mặt.</div>
              </div>

              <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Mua thêm điểm (mô phỏng callback ZaloPay)</div>
              <div className="grid grid-cols-2 gap-3 mb-5">
                {PACKS.map((p) => (
                  <button key={p.pts} disabled={busy}
                    onClick={() => act(() => api.buyPack(p.pts), `Đã nạp ${p.pts + p.bonus} điểm qua ZaloPay (dev) ✓`)}
                    className="rounded-2xl p-3.5 text-left active:scale-[.97] transition-transform" style={{ background: T.card }}>
                    <div className="text-lg font-bold" style={{ ...MONO, color: T.ink }}>
                      {p.pts.toLocaleString("vi-VN")} <span className="text-xs font-semibold">điểm</span>
                    </div>
                    {p.bonus > 0 && <div className="text-[11px] font-bold" style={{ color: T.green }}>+{p.bonus} điểm tặng</div>}
                    <div className="text-xs mt-1.5 inline-block px-2 py-1 rounded-full font-bold" style={{ background: T.brand, color: T.ink }}>{p.price}</div>
                  </button>
                ))}
              </div>

              <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Lịch sử giao dịch (từ ledger)</div>
              {txs.map((t) => (
                <div key={t.id} className="flex justify-between items-center rounded-xl px-4 py-3 mb-2" style={{ background: T.card }}>
                  <div className="text-[13px] pr-3" style={{ color: T.ink }}>{txnLabel(t)}</div>
                  <div className="text-sm font-bold shrink-0" style={{ ...MONO, color: t.delta_available > 0 ? T.green : T.ink }}>
                    {t.delta_available > 0 ? "+" : ""}{t.delta_available.toLocaleString("vi-VN")}
                  </div>
                </div>
              ))}
            </>)}
          </div>

          {(tab === "discover" || tab === "mine") && (
            <button onClick={() => setCreating(true)}
              className="absolute bottom-24 right-4 z-20 px-4 py-3 rounded-full font-bold text-sm active:scale-95 transition-transform"
              style={{ background: T.ink, color: T.brand, boxShadow: "0 6px 16px rgba(21,23,27,.3)" }}>
              + Tạo kèo
            </button>
          )}

          {/* Tab bar */}
          <div className="absolute bottom-0 inset-x-0 z-20 flex border-t" style={{ background: T.card, borderColor: T.line }}>
            {[
              { k: "discover", icon: "🔥", label: "Khám phá" },
              { k: "mine", icon: "🎯", label: "Kèo của tôi" },
              { k: "shop", icon: "🛍️", label: "Đổi thưởng" },
              { k: "wallet", icon: "⭐", label: "Ví điểm" },
            ].map((t) => (
              <button key={t.k} onClick={() => { setTab(t.k); load(); }} className="flex-1 py-3 pb-5 flex flex-col items-center gap-0.5">
                <span className="text-lg">{t.icon}</span>
                <span className="text-[10px] font-bold" style={{ color: tab === t.k ? T.ink : T.gray }}>{t.label}</span>
                <span className="h-0.5 w-6 rounded-full mt-0.5" style={{ background: tab === t.k ? T.brand : "transparent" }} />
              </button>
            ))}
          </div>

          {toast && (
            <div className="absolute top-24 inset-x-6 z-50 rounded-xl px-4 py-3 text-sm font-semibold text-center"
              style={{ background: T.ink, color: "#fff", boxShadow: "0 8px 20px rgba(21,23,27,.35)" }}>{toast}</div>
          )}

          <JoinModal c={joining} wallet={wallet} busy={busy}
            onConfirm={(c) => act(async () => { await api.joinChallenge(c.id); setJoining(null); }, "Đã chốt kèo! Điểm cược được khóa 🔒")}
            onClose={() => setJoining(null)} />
          <CreateSheet open={creating} busy={busy} onClose={() => setCreating(false)}
            onCreate={(c) => act(async () => { await api.createChallenge(c); setCreating(false); setTab("mine"); }, "Kèo của bạn đã lên sàn 🎉")} />
        </>)}
      </div>
    </div>
  );
}

const Empty = ({ children }) => (
  <div className="rounded-2xl p-8 text-center text-sm" style={{ background: T.card, color: T.gray }}>{children}</div>
);

function txnLabel(t) {
  const names = {
    purchase: "Mua điểm qua ZaloPay", stake_lock: "Đặt cược vào kèo",
    settlement: "Chốt sổ kèo", redeem: "Đổi thưởng", stake_release: "Hoàn cược",
    admin_adjust: "Điều chỉnh",
  };
  return names[t.type] || t.type;
}
