import { useCallback, useEffect, useState, useRef } from "react";
import { Flame, Target, ShoppingBag, Wallet, Trophy, Gift, Ticket, Medal, Mountain, Footprints, User, LogOut, ChevronRight, Share2, Sparkles, RefreshCw, SlidersHorizontal, TrendingUp, Users, Heart } from "lucide-react";
import * as api from "./api.js";
import { T, MONO, SPORTS, SOURCES, GOALS, CHARITIES, fmtP, daysLeft } from "./theme.js";
import LeaderboardSheet from "./leaderboard-sheet.jsx";
import { StreakCard, ActivityFeed } from "./activity-feed.jsx";
import { NotificationToast, detectToastType } from "./notification.jsx";
import AdminDashboard from "./admin-dashboard.jsx";

const SKU_ICONS = { "soap-sinh-duoc": Sparkles, "voucher-sport-500k": Ticket, "gear-trail-shoes": Footprints, "ticket-hn-marathon": Medal, "ticket-sg-night-run": Mountain };
// Tỷ giá 1 điểm = 1 VNĐ.
const PACKS = [
  { pts: 100000, price: "100.000đ", bonus: 0 }, { pts: 300000, price: "300.000đ", bonus: 15000 },
  { pts: 500000, price: "500.000đ", bonus: 40000 }, { pts: 1000000, price: "1.000.000đ", bonus: 120000 },
];

// ===== Animated Counter =====
function useAnimatedNumber(target, duration = 800) {
  const [display, setDisplay] = useState(0);
  const prev = useRef(0);
  useEffect(() => {
    const start = prev.current;
    const diff = target - start;
    if (diff === 0) return;
    const startTime = performance.now();
    const step = (now) => {
      const elapsed = now - startTime;
      const t = Math.min(elapsed / duration, 1);
      const eased = 1 - Math.pow(1 - t, 3); // ease out cubic
      setDisplay(Math.round(start + diff * eased));
      if (t < 1) requestAnimationFrame(step);
      else prev.current = target;
    };
    requestAnimationFrame(step);
  }, [target, duration]);
  return display;
}

// ===== Skeleton Card =====
function SkeletonChallengeCard() {
  return (
    <div className="rounded-3xl mb-4 overflow-hidden" style={{ background: T.card, border: `1px solid ${T.line}` }}>
      <div className="p-5 pb-4 flex items-start gap-4">
        <div className="skeleton w-14 h-14 rounded-2xl shrink-0" />
        <div className="min-w-0 flex-1 space-y-2">
          <div className="skeleton h-5 w-3/4 rounded-lg" />
          <div className="skeleton h-3.5 w-1/2 rounded-lg" />
          <div className="skeleton h-3.5 w-2/5 rounded-lg" />
        </div>
      </div>
      <div className="px-5 pb-5 flex items-end justify-between gap-3">
        <div className="space-y-2">
          <div className="skeleton h-3 w-16 rounded" />
          <div className="skeleton h-6 w-20 rounded" />
        </div>
        <div className="skeleton h-10 w-24 rounded-xl" />
      </div>
    </div>
  );
}

function SkeletonMyCard() {
  return (
    <div className="rounded-2xl p-4 mb-4" style={{ background: T.card, border: `1px solid ${T.line}` }}>
      <div className="flex items-center gap-3 mb-3">
        <div className="skeleton w-11 h-11 rounded-xl shrink-0" />
        <div className="flex-1 space-y-2">
          <div className="skeleton h-4 w-3/4 rounded" />
          <div className="skeleton h-3 w-1/2 rounded" />
        </div>
      </div>
      <div className="skeleton h-4 rounded-full mb-2" />
      <div className="skeleton h-3 w-2/3 rounded" />
    </div>
  );
}

function Notch({ side }) {
  return <div className="absolute w-4 h-4 rounded-full"
    style={{ background: T.card, top: "50%", [side]: -8, transform: "translateY(-50%)" }} />;
}

function SourceBadge({ source }) {
  const s = SOURCES[source] || { label: source, icon: Trophy };
  const Icon = s.icon;
  const isStrava = source === "strava";
  return <span className="inline-flex items-center gap-1.5 text-[10px] font-semibold px-2.5 py-1 rounded-full"
    style={{ background: isStrava ? "rgba(252, 76, 2, 0.1)" : T.bg, color: isStrava ? T.strava : T.textDim, border: `1px solid ${isStrava ? "rgba(252, 76, 2, 0.2)" : T.line}` }}>
    <Icon size={12} strokeWidth={2.5} /> {s.label}{isStrava ? " · tự động ⚡" : ""}
  </span>;
}

function ChallengeStatusBadge({ startAt, endAt }) {
  const now = new Date();
  const start = new Date(startAt);
  const end = new Date(endAt);
  
  let label = "Mới";
  let color = T.brand;
  let bg = "rgba(204, 255, 0, 0.08)";
  let border = "rgba(204, 255, 0, 0.2)";
  let pulse = false;
  
  if (now > end) {
    label = "Kết thúc";
    color = T.textDim;
    bg = "rgba(255, 255, 255, 0.04)";
    border = "rgba(255, 255, 255, 0.08)";
  } else if (now >= start) {
    label = "Đang hoạt động";
    color = T.green;
    bg = "rgba(52, 199, 89, 0.08)";
    border = "rgba(52, 199, 89, 0.2)";
    pulse = true;
  }
  
  return (
    <span className="inline-flex items-center gap-1.5 text-[10px] font-bold px-2.5 py-1 rounded-full"
      style={{ background: bg, color: color, border: `1px solid ${border}` }}>
      <span className={pulse ? "pulse-dot" : ""} style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: color }} />
      {label}
    </span>
  );
}

// ===== Màn đăng nhập =====
function Login({ onDone }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const handleGoogleLogin = async () => {
    setBusy(true); setErr("");
    try {
      const { error } = await api.supabase.auth.signInWithOAuth({
        provider: 'google',
        options: {
          redirectTo: window.location.origin,
        }
      });
      if (error) throw error;
    } catch (e) {
      setErr(e.message || "Lỗi đăng nhập Google");
      setBusy(false);
    }
  };

  const handleZaloLogin = async () => {
    setBusy(true); setErr("");
    try {
      const array = new Uint8Array(32);
      window.crypto.getRandomValues(array);
      const verifier = btoa(String.fromCharCode(...array))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");
      
      localStorage.setItem("zalo_code_verifier", verifier);

      const encoder = new TextEncoder();
      const data = encoder.encode(verifier);
      const digest = await window.crypto.subtle.digest("SHA-256", data);
      const challenge = btoa(String.fromCharCode(...new Uint8Array(digest)))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");

      const appID = import.meta.env.VITE_ZALO_APP_ID || "1809071068864700088";
      const redirectURI = encodeURIComponent(window.location.origin + "/oauth/zalo/callback");
      const state = Math.random().toString(36).substring(2);
      window.location.href = `https://oauth.zaloapp.com/v4/permission?app_id=${appID}&redirect_uri=${redirectURI}&code_challenge=${challenge}&state=${state}`;
    } catch (e) {
      setErr(e.message || "Lỗi khởi tạo đăng nhập Zalo");
      setBusy(false);
    }
  };

  return (
      <div className="flex-1 flex flex-col items-center justify-center px-8 bg-grid">
        <div className="text-5xl mb-2 text-glow" style={{ fontFamily: "'Archivo Black', sans-serif", color: T.text }}>
          KÈO<span style={{ color: T.brand }}>.</span>
        </div>
        <div className="text-xs uppercase tracking-widest mb-12 font-semibold" style={{ color: T.textDim }}>Thử thách chính mình và bạn bè</div>
        
        <div className="w-full flex flex-col gap-4">
          <button disabled={busy} onClick={handleGoogleLogin} className="w-full py-3.5 rounded-full font-bold text-[15px] flex items-center justify-center gap-2 transition-transform active:scale-95"
            style={{ background: "#FFF", color: "#000" }}>
            <svg width="18" height="18" viewBox="0 0 24 24"><path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/><path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/><path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"/><path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/><path fill="none" d="M1 1h22v22H1z"/></svg>
            Tiếp tục với Google
          </button>
          
          <button disabled={busy} onClick={handleZaloLogin} className="w-full py-3.5 rounded-full font-bold text-[15px] flex items-center justify-center gap-2 transition-transform active:scale-95"
            style={{ background: "#0068FF", color: "#FFF" }}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none"><path fillRule="evenodd" clipRule="evenodd" d="M12 2C6.48 2 2 5.92 2 10.75c0 2.58 1.28 4.88 3.32 6.37l-.87 2.62a.75.75 0 0 0 1.08.85l3.22-1.93c1.02.22 2.11.34 3.25.34 5.52 0 10-3.92 10-8.75S17.52 2 12 2zm1.31 11.75h-3v-1.5h1.5v-1.25h-1.5v-1.5h3v-1.5h-4.75a.75.75 0 0 0-.75.75v5.5c0 .41.34.75.75.75h4.75v-1.5z" fill="#FFF"/></svg>
            Tiếp tục với Zalo
          </button>
        </div>
        
        {busy && <div className="text-xs mt-6 font-bold" style={{ color: T.brand }}>Đang xác thực...</div>}
        {err && <div className="text-xs mt-6 font-bold bg-red-500/10 px-4 py-2 rounded-lg" style={{ color: T.red }}>{err}</div>}
        
        <div className="text-[10px] mt-12 text-center leading-relaxed" style={{ color: T.textDim }}>
          Bằng việc đăng nhập, bạn đồng ý với Điều khoản sử dụng <br />và Chính sách bảo mật của Kèo.
        </div>
      </div>
  );
}

// ===== Filter Bar cho Discover =====
function FilterBar({ sportFilter, setSportFilter, sortKey, setSortKey }) {
  const [showSort, setShowSort] = useState(false);
  const sortOptions = [
    { k: "default", label: "Mới nhất", icon: TrendingUp },
    { k: "pot", label: "Quỹ cao nhất", icon: Trophy },
    { k: "participants", label: "Nhiều người nhất", icon: Users },
  ];
  const activeSortLabel = sortOptions.find(s => s.k === sortKey)?.label || "Sắp xếp";

  return (
    <div className="mb-5">
      {/* Sport chips */}
      <div className="flex gap-2 overflow-x-auto pb-2 hide-scrollbar mb-3">
        <SportChip active={sportFilter === null} onClick={() => setSportFilter(null)}>Tất cả</SportChip>
        {Object.entries(SPORTS).map(([k, v]) => {
          const Icon = v.icon;
          return (
            <SportChip key={k} active={sportFilter === k} onClick={() => setSportFilter(sportFilter === k ? null : k)}>
              <span className="flex items-center gap-1.5"><Icon size={13} /> {v.label}</span>
            </SportChip>
          );
        })}
      </div>

      {/* Sort button */}
      <div className="relative">
        <button
          onClick={() => setShowSort(v => !v)}
          className="flex items-center gap-1.5 text-[12px] font-bold px-3 py-1.5 rounded-full transition-all"
          style={{ background: sortKey !== "default" ? "rgba(204,255,0,0.1)" : T.card, border: `1px solid ${sortKey !== "default" ? T.brand : T.line}`, color: sortKey !== "default" ? T.brand : T.textDim }}
        >
          <SlidersHorizontal size={12} /> {activeSortLabel}
        </button>
        {showSort && (
          <div className="absolute top-full left-0 mt-1 rounded-2xl overflow-hidden z-20 scale-in" style={{ background: T.card, border: `1px solid ${T.line}`, minWidth: 160, boxShadow: "0 8px 24px rgba(0,0,0,0.4)" }}>
            {sortOptions.map(opt => {
              const OptIcon = opt.icon;
              return (
                <button key={opt.k} onClick={() => { setSortKey(opt.k); setShowSort(false); }}
                  className="w-full flex items-center gap-2.5 px-4 py-3 text-[13px] font-semibold text-left transition-colors hover:bg-white/5"
                  style={{ color: sortKey === opt.k ? T.brand : T.text, borderBottom: `1px solid ${T.line}` }}>
                  <OptIcon size={14} style={{ color: sortKey === opt.k ? T.brand : T.textDim }} />
                  {opt.label}
                  {sortKey === opt.k && <span className="ml-auto text-brand">✓</span>}
                </button>
              );
            })}
          </div>
        )}
        {showSort && <div className="fixed inset-0 z-10" onClick={() => setShowSort(false)} />}
      </div>
    </div>
  );
}

const SportChip = ({ active, onClick, children }) => (
  <button onClick={onClick} className="px-3 py-1.5 rounded-full text-[12px] font-bold transition-all border shrink-0"
    style={{ background: active ? "rgba(204,255,0,0.1)" : T.card, borderColor: active ? T.brand : T.line, color: active ? T.brand : T.textDim }}>
    {children}
  </button>
);

// ===== Thẻ kèo (phiếu cược) =====
function ChallengeCard({ c, onJoin, onBoard, onShare }) {
  const sport = SPORTS[c.sport] || { icon: Trophy };
  const Icon = sport.icon;
  const pot = c.stake_points * c.participants;
  return (
    <div className="relative rounded-3xl mb-4 cursor-pointer overflow-hidden transition-all hover:-translate-y-1 hover:shadow-[0_8px_30px_rgba(204,255,0,0.1)] group fade-in-up" onClick={() => onBoard(c.id)}
      style={{ background: T.card, border: `1px solid ${T.line}` }}>
      <div className="absolute inset-0 bg-gradient-to-br from-white/5 to-transparent opacity-0 group-hover:opacity-100 transition-opacity" />
      <div className="p-5 pb-4 flex items-start gap-4">
        <div className="w-14 h-14 rounded-2xl flex items-center justify-center shrink-0" style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.text }}>
          <Icon size={28} strokeWidth={1.5} />
        </div>
        <div className="min-w-0">
          <div className="text-[17px] font-bold leading-tight mb-1 flex flex-wrap items-center gap-1.5" style={{ color: T.text }}>
            {c.is_charity && <span className="text-[10px] font-black px-1.5 py-0.5 rounded-md text-glow select-none" style={{ background: "rgba(255, 51, 102, 0.15)", color: "#FF3366", border: "1px solid rgba(255, 51, 102, 0.3)" }}>🎗️ TỪ THIỆN</span>}
            {c.title}
          </div>
          <div className="text-xs mb-1.5 font-medium" style={{ color: T.textDim }}>
            Thời gian: <span className="font-semibold text-white">{new Date(c.start_at).toLocaleDateString("vi-VN", {day:"2-digit",month:"2-digit"})}</span> - <span className="font-semibold text-white">{new Date(c.end_at).toLocaleDateString("vi-VN", {day:"2-digit",month:"2-digit"})}</span>
          </div>
          <div className="text-xs mb-2 font-medium" style={{ color: T.textDim }}>
            còn <span style={{ color: T.text }}>{daysLeft(c.end_at)}</span> ngày · <span style={{ color: T.text }}>{c.participants}{c.max_participants > 0 ? `/${c.max_participants}` : ""}</span> người tham gia
          </div>
          <div className="flex flex-wrap gap-1.5 items-center mt-2">
            <SourceBadge source={c.source} />
            <ChallengeStatusBadge startAt={c.start_at} endAt={c.end_at} />
          </div>
        </div>
      </div>
      <div className="relative mx-5" style={{ borderTop: `1px dashed ${T.line}` }}><Notch side="left" /><Notch side="right" /></div>
      <div className="p-5 pt-4 flex items-end justify-between relative z-10">
        <div>
          <div className="text-[10px] uppercase tracking-widest font-bold mb-1" style={{ color: T.textDim }}>Điểm cược</div>
          <div className="text-xl font-bold text-glow" style={{ ...MONO, color: T.brand }}>{c.stake_points}</div>
          {c.is_charity ? (
            <div className="text-[11px] mt-1 font-semibold" style={{ color: CHARITIES[c.charity_id]?.color || T.brand }}>
              Quyên góp: {CHARITIES[c.charity_id]?.name || "Từ thiện"}
            </div>
          ) : (
            <div className="text-[11px] mt-1 font-semibold" style={{ color: T.green }}>Hoàn cược + chia quỹ người rớt</div>
          )}
        </div>
        <div className="text-right flex items-center gap-2">
          <div>
            <div className="text-[10px] uppercase tracking-widest mb-1.5 font-bold" style={{ color: T.textDim }}>{c.is_charity ? "Quỹ từ thiện" : `Quỹ ${fmtP(pot)}`}</div>
            <div className="flex gap-2 justify-end">
              <button onClick={(e) => { e.stopPropagation(); onShare(c.id, c.title); }} 
                className="p-2.5 rounded-xl transition-all hover:bg-white/5 active:scale-95" 
                style={{ background: T.bg, border: `1px solid ${T.line}`, color: T.textDim }}
                title="Chia sẻ & mời bạn bè">
                <Share2 size={16} />
              </button>
              {c.joined
                ? <span className="inline-block text-xs font-bold px-4 py-2.5 rounded-xl border" style={{ background: T.bg, color: T.textDim, borderColor: T.line }}>Đã tham gia</span>
                : <button onClick={(e) => { e.stopPropagation(); onJoin(c); }} className="btn-neon text-sm font-bold px-6 py-2.5 rounded-xl active:scale-95 transition-transform"
                    style={{ background: T.brand, color: T.bg }}>Vào kèo</button>}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ===== Kèo của tôi =====
function MyChallengeCard({ c, onSync, busy, onBoard, onShare }) {
  const sport = SPORTS[c.sport] || { icon: Trophy };
  const Icon = sport.icon;
  const pct = c.periods_total ? Math.round((c.periods_passed / c.periods_total) * 100) : 0;
  const settled = c.status !== "active";
  const won = c.status === "completed";
  return (
    <div className="rounded-2xl p-4 mb-4 cursor-pointer fade-in-up" onClick={() => onBoard(c.challenge_id)} style={{ background: T.card, border: `1px solid ${T.line}` }}>
      <div className="flex items-center gap-3 mb-3">
        <div className="w-11 h-11 rounded-xl flex items-center justify-center shrink-0" style={{ background: T.bg, color: T.brand }}>
          <Icon size={22} strokeWidth={2} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-bold leading-tight flex items-center gap-1.5 flex-wrap" style={{ color: T.text }}>
            {c.is_charity && <span className="text-[9px] font-black px-1.5 py-0.5 rounded bg-pink-950/20 text-[#FF3366] border border-[#FF3366]/30 select-none">🎗️ TỪ THIỆN</span>}
            {c.title}
          </div>
          <div className="text-xs" style={{ color: T.textDim }}>
            {settled ? "đã chốt sổ" : `còn ${daysLeft(c.end_at)} ngày`} · cược {fmtP(c.stake_points)}
            {c.is_charity && <span className="text-[10px] ml-1.5 font-bold" style={{ color: CHARITIES[c.charity_id]?.color || T.brand }}>({CHARITIES[c.charity_id]?.name})</span>}
          </div>
        </div>
        {settled && <span className="text-xs font-bold px-2.5 py-1 rounded-full shrink-0"
          style={{ background: won ? "#E7F5EC" : "#FDEBEC", color: won ? T.green : T.red }}>
          {won ? "✓ Về đích" : "✗ Rớt kèo"}
        </span>}
      </div>
      <div className="flex items-center justify-between mb-2">
        <div className="flex gap-1.5 items-center">
          <SourceBadge source={c.source} />
          <ChallengeStatusBadge startAt={c.start_at} endAt={c.end_at} />
        </div>
        {!settled && (
          <button onClick={(e) => { e.stopPropagation(); onShare(c.challenge_id, c.title); }}
            className="flex items-center gap-1 text-[10px] font-bold px-2.5 py-1 rounded-full active:scale-95 transition-all hover:bg-white/5"
            style={{ background: "rgba(255,255,255,0.04)", border: `1px solid ${T.line}`, color: T.textDim }}>
            <Share2 size={12} /> Chia sẻ & mời
          </button>
        )}
      </div>

      {!settled && (c.source === "strava" ? (
        <div className="rounded-xl px-3 py-2.5 mb-3 text-[11px] font-semibold" style={{ background: "#FEEDE5", color: T.strava }}>
          ⚡ Hoạt động Strava được đồng bộ tự động — không cần thao tác thủ công
        </div>
      ) : (
        <div className="flex items-center justify-between rounded-xl px-3 py-2.5 mb-3" style={{ background: T.card }}>
          <div className="text-[12px] font-semibold" style={{ color: T.text }}>
            Dữ liệu từ {SOURCES[c.source]?.label || c.source}
          </div>
          <button onClick={(e) => { e.stopPropagation(); onSync(c); }} disabled={busy}
            className="text-xs font-bold px-3 py-1.5 rounded-full active:scale-95 transition-transform"
            style={{ background: T.bg, color: T.brand, opacity: busy ? 0.6 : 1 }}>
            ⟳ Đồng bộ (demo)
          </button>
        </div>
      ))}

      <div className="relative h-4 rounded-full overflow-hidden mb-2" style={{ background: T.card }}>
        <div className="absolute inset-y-0 left-0 rounded-full transition-all duration-700"
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
function JoinModal({ c, wallet, busy, onConfirm, onClose, setTab }) {
  if (!c) return null;
  const enough = wallet.available >= c.stake_points;
  const src = SOURCES[c.source] || { label: c.source, icon: "" };
  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-sm rounded-3xl p-6 relative overflow-hidden scale-in" style={{ background: T.card, border: `1px solid ${T.line}` }} onClick={(e) => e.stopPropagation()}>
        <div className="text-xl font-black mb-4 uppercase tracking-wider text-center" style={{ color: T.text }}>Chốt kèo?</div>
        <div className="text-sm font-semibold mb-4 text-center" style={{ color: T.textDim }}>{c.title}</div>
        <div className="rounded-2xl p-4 mb-4 space-y-2" style={{ background: T.paper, border: `1px solid ${T.line}` }}>
          <Row label="Điểm cược (khóa trong ví)" value={fmtP(c.stake_points)} />
          <Row label="Không đạt cam kết" value={`mất ${fmtP(c.stake_points)}`} color={T.red} />
          <Row label="Xác thực tiến độ" value={
            <span className="flex items-center gap-1.5 justify-end">
              {src.icon && <src.icon size={16} className="text-[#CCFF00]" />}
              <span>{src.label}</span>
            </span>
          } />
        </div>
        <div className="text-[11px] mb-6 leading-relaxed text-center" style={{ color: T.textDim }}>
          Điểm của người không hoàn thành chia đều cho người về đích (sau 10% phí).<br />
          Điểm chỉ dùng đổi vật phẩm — <b>không quy đổi thành tiền mặt</b>.
        </div>
        {enough ? (
          <button disabled={busy} onClick={() => onConfirm(c)}
            className="w-full btn-neon font-bold py-3.5 rounded-xl uppercase tracking-widest text-[13px]"
            style={{ background: T.brand, color: T.bg, opacity: busy ? 0.6 : 1 }}>
            {busy ? "Đang chốt..." : `Đặt cược ${fmtP(c.stake_points)}`}
          </button>
        ) : (
          <button onClick={() => { onClose(); setTab("wallet"); }}
            className="w-full font-bold py-3.5 rounded-xl uppercase tracking-widest text-[13px] text-center active:scale-[.98] transition-transform"
            style={{ background: T.red, color: T.text, boxShadow: "0 0 15px rgba(255, 59, 48, 0.4)" }}>
            Không đủ điểm — Nạp thêm ở tab Ví ⭐
          </button>
        )}
      </div>
    </div>
  );
}

function Row({ label, value, color }) {
  return <div className="flex justify-between text-sm gap-3">
    <span style={{ color: T.textDim }}>{label}</span>
    <span className="font-bold text-right" style={{ ...MONO, color: color || T.ink }}>{value}</span>
  </div>;
}

// ===== Tạo kèo =====
function CreateSheet({ open, busy, onClose, onCreate, wallet, setTab }) {
  const [sport, setSport] = useState("run");
  const [goalType, setGoalType] = useState("daily_distance_km");
  const [goal, setGoal] = useState(5);
  const [stake, setStake] = useState(10);
  const [source, setSource] = useState("strava");
  const [maxParticipants, setMaxParticipants] = useState(10);
  const [startAt, setStartAt] = useState(() => new Date().toISOString().split('T')[0]);
  const [endAt, setEndAt] = useState(() => {
    const d = new Date();
    d.setDate(d.getDate() + 30);
    return d.toISOString().split('T')[0];
  });
  const [isCharity, setIsCharity] = useState(false);
  const [charityId, setCharityId] = useState(1001);

  // Đồng bộ stake mặc định khi mở modal hoặc ví khả dụng thay đổi
  useEffect(() => {
    if (open && wallet?.available > 0) {
      setStake(Math.min(200, Math.floor(wallet.available / 10) * 10 || 10));
    } else {
      setStake(10);
    }
  }, [wallet?.available, open]);

  if (!open) return null;

  const enough = wallet?.available >= stake;
  const days = Math.max(1, Math.ceil((new Date(endAt) - new Date(startAt)) / 86400000));

  const pickSport = (k) => {
    setSport(k);
    const gt = Object.entries(GOALS).find(([, v]) => v.sports.includes(k));
    if (gt) {
      setGoalType(gt[0]);
      if (k === "walk") setGoal(10000);
      else if (k === "run" || k === "bike") setGoal(5);
      else if (k === "swim") setGoal(2);
      else if (k === "gym") setGoal(3);
    }
  };

  const handleStartAtChange = (val) => {
    setStartAt(val);
    if (new Date(endAt) <= new Date(val)) {
      const d = new Date(val);
      d.setDate(d.getDate() + 30);
      setEndAt(d.toISOString().split('T')[0]);
    }
  };

  const handleEndAtChange = (val) => {
    if (new Date(val) <= new Date(startAt)) return;
    setEndAt(val);
  };

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-sm rounded-3xl p-6 relative overflow-y-auto max-h-[85vh] scale-in" style={{ background: T.card, border: `1px solid ${T.line}` }} onClick={(e) => e.stopPropagation()}>
        <div className="text-xl font-black mb-5 uppercase tracking-wider text-center" style={{ color: T.text }}>Tạo kèo mới</div>

        <Label>Bộ môn</Label>
        <div className="flex flex-wrap gap-2 mb-4">
          {Object.entries(SPORTS).map(([k, v]) => {
            const Icon = v.icon;
            return (
              <Chip key={k} active={sport === k} onClick={() => pickSport(k)}>
                <span className="flex items-center gap-1.5"><Icon size={16} /> {v.label}</span>
              </Chip>
            );
          })}
        </div>

        <div className="grid grid-cols-2 gap-3 mb-4">
          <div>
            <Label>Mục tiêu ({GOALS[goalType]?.label || "đơn vị"})</Label>
            <input type="number" value={goal} onChange={(e) => setGoal(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
          <div>
            <Label>Tối đa (người)</Label>
            <input type="number" value={maxParticipants} onChange={(e) => setMaxParticipants(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3.5 mb-4">
          <div>
            <Label>Ngày bắt đầu</Label>
            <input type="date" value={startAt} onChange={(e) => handleStartAtChange(e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
          <div>
            <Label>Ngày kết thúc</Label>
            <input type="date" value={endAt} min={startAt} onChange={(e) => handleEndAtChange(e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
        </div>

        <Label>Nguồn xác thực</Label>
        <div className="flex gap-2 mb-4 overflow-x-auto pb-1">
          {Object.entries(SOURCES)
            .filter(([k]) => k === "strava")
            .map(([k, v]) => {
              const Icon = v.icon;
              return (
                <Chip key={k} active={source === k} onClick={() => setSource(k)}>
                  <span className="flex items-center gap-1.5"><Icon size={16} /> {v.label}</span>
                </Chip>
              );
            })}
        </div>

        {/* Toggle Kèo Từ Thiện */}
        <div className="flex items-center justify-between p-3.5 rounded-2xl mb-4" style={{ background: T.paper, border: `1px solid ${T.line}` }}>
          <div className="flex items-center gap-2">
            <Heart size={16} className="animate-pulse" style={{ color: T.red }} />
            <div>
              <div className="text-xs font-bold" style={{ color: T.text }}>🎗️ Kèo Từ Thiện</div>
              <div className="text-[10px]" style={{ color: T.textDim }}>Quyên góp cược thua vào quỹ</div>
            </div>
          </div>
          <button 
            onClick={() => setIsCharity(!isCharity)}
            className="w-11 h-6 rounded-full transition-colors relative flex items-center px-0.5"
            style={{ background: isCharity ? T.brand : T.line }}
          >
            <div 
              className="w-5 h-5 rounded-full bg-white shadow-sm transition-transform"
              style={{ transform: isCharity ? 'translateX(20px)' : 'translateX(0)' }}
            />
          </button>
        </div>

        {/* Danh sách Quỹ từ thiện */}
        {isCharity && (
          <div className="mb-4 p-3 rounded-2xl" style={{ background: T.paper, border: `1px solid ${T.line}` }}>
            <div className="text-xs font-bold mb-2" style={{ color: T.textDim }}>Chọn Quỹ quyên góp</div>
            <div className="flex flex-col gap-2">
              {Object.entries(CHARITIES).map(([k, v]) => (
                <button
                  key={k}
                  onClick={() => setCharityId(Number(k))}
                  className="flex items-start gap-2.5 p-2 rounded-xl text-left transition-all"
                  style={{
                    background: charityId === Number(k) ? T.card : 'transparent',
                    border: charityId === Number(k) ? `1.5px solid ${v.color}` : `1px solid ${T.line}`
                  }}
                >
                  <span className="text-lg mt-0.5">{v.logo}</span>
                  <div>
                    <div className="text-xs font-bold" style={{ color: charityId === Number(k) ? v.color : T.text }}>{v.name}</div>
                    <div className="text-[10px]" style={{ color: T.textDim }}>{v.desc}</div>
                  </div>
                </button>
              ))}
            </div>
            <div className="text-[9px] leading-relaxed font-bold mt-2 px-1 text-center" style={{ color: T.brand }}>
              💡 Thắng hoàn cược, thua quyên góp 100% quỹ. Miễn phí nền tảng!
            </div>
          </div>
        )}

        <Label>Điểm cược mỗi người</Label>
        <div className="rounded-2xl p-4 mb-5" style={{ background: T.paper, border: `1px solid ${T.line}` }}>
          <div className="flex justify-between items-center mb-2">
            <div className="text-xs font-semibold" style={{ color: T.textDim }}>Số điểm cược:</div>
            <div className="text-base font-black text-glow" style={{ ...MONO, color: T.brand }}>
              {fmtP(stake)}
            </div>
          </div>
          <input 
            type="range" 
            min="10" 
            max={Math.max(10, wallet?.available || 0)} 
            step="10"
            value={stake} 
            onChange={(e) => setStake(Number(e.target.value))}
            disabled={!wallet?.available || wallet.available < 10}
            className="w-full h-1.5 bg-zinc-700 rounded-lg appearance-none cursor-pointer accent-[#CCFF00] focus:outline-none" 
          />
          <div className="flex justify-between text-[10px] font-bold mt-1.5" style={{ color: T.textDim }}>
            <span>Min: 10</span>
            <span>Ví khả dụng: {Number(wallet?.available || 0).toLocaleString("vi-VN")} pts</span>
          </div>
        </div>

        {enough ? (
          <button disabled={busy}
            onClick={() => onCreate({
              title: `${SPORTS[sport].label} ${goal.toLocaleString("vi-VN")} ${GOALS[goalType]?.label || ""}`,
              sport, goal_type: goalType, goal_value: goal, source,
              stake_points: stake, duration_days: days, max_participants: maxParticipants,
              start_at: startAt,
              is_charity: isCharity,
              charity_id: isCharity ? charityId : 0,
            })}
            className="w-full py-3.5 rounded-2xl font-bold text-[15px] active:scale-[.98] transition-transform"
            style={{ background: T.brand, color: T.bg, opacity: busy ? 0.6 : 1 }}>
            {busy ? "Đang tạo..." : `Tạo kèo · cược ${fmtP(stake)}`}
          </button>
        ) : (
          <button onClick={() => { onClose(); setTab("wallet"); }}
            className="w-full font-bold py-3.5 rounded-2xl text-[15px] text-center active:scale-[.98] transition-transform"
            style={{ background: T.red, color: T.text, boxShadow: "0 0 15px rgba(255, 59, 48, 0.4)" }}>
            Không đủ điểm — Nạp thêm ở tab Ví ⭐
          </button>
        )}
      </div>
    </div>
  );
}

const Label = ({ children }) => (
  <div className="text-xs font-bold uppercase tracking-widest mb-2" style={{ color: T.textDim }}>{children}</div>
);
const Chip = ({ active, onClick, children }) => (
  <button onClick={onClick} className="px-3.5 py-2 rounded-xl text-[13px] font-bold transition-all border shrink-0"
    style={{ 
      background: active ? "rgba(204,255,0,0.1)" : T.bg, 
      borderColor: active ? T.brand : T.line,
      color: active ? T.brand : T.textDim 
    }}>
    {children}
  </button>
);

function getRedemptionStatusBadge(status) {
  const mapping = {
    created: { label: "Đang xử lý", color: "#E6A23C", bg: "rgba(230, 162, 60, 0.1)" },
    fulfilled: { label: "Đã giao", color: "#67C23A", bg: "rgba(103, 194, 58, 0.1)" },
    cancelled: { label: "Hủy", color: "#F56C6C", bg: "rgba(245, 108, 108, 0.1)" },
  };
  const match = mapping[status] || { label: status, color: "#909399", bg: "rgba(144, 147, 153, 0.1)" };
  return (
    <span className="text-[10px] font-bold px-2.5 py-1 rounded-full shrink-0" style={{ color: match.color, background: match.bg, border: `1px solid ${match.color}33` }}>
      {match.label}
    </span>
  );
}

// ===== App Core =====
function AppCore({ userProfile, onLogout }) {
  const isAdmin = userProfile?.role === "admin";
  const [tab, setTab] = useState("discover");
  const [tabKey, setTabKey] = useState(0); // force re-render for fade animation
  const [wallet, setWallet] = useState({ available: 0, locked: 0 });
  const [challenges, setChallenges] = useState([]);
  const [mine, setMine] = useState([]);
  const [shop, setShop] = useState([]);
  const [txs, setTxs] = useState([]);
  const [stats, setStats] = useState(null);
  const [acts, setActs] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [board, setBoard] = useState(null);
  const [joining, setJoining] = useState(null);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState(null); // { msg, type }
  const [paymentQR, setPaymentQR] = useState(null);
  const [rewards, setRewards] = useState(null); // { checked_in_today, total_points }
  const [redeemConfirm, setRedeemConfirm] = useState(null);
  const [deliveryForm, setDeliveryForm] = useState(null);
  const [redemptions, setRedemptions] = useState([]);
  const [charityStats, setCharityStats] = useState({ "1001": 0, "1002": 0 });

  // Filter/Sort state
  const [sportFilter, setSportFilter] = useState(null);
  const [sortKey, setSortKey] = useState("default");

  // Animated wallet counter
  const animatedWallet = useAnimatedNumber(wallet.available, 900);

  const showToast = (msg, type) => {
    const detectedType = type || detectToastType(msg);
    setToast({ msg, type: detectedType });
  };

  const handleShare = (challengeID, title) => {
    const inviteLink = `${window.location.origin}/?join=${challengeID}`;
    navigator.clipboard.writeText(inviteLink)
      .then(() => showToast(`Đã sao chép link mời tham gia kèo "${title}"! 🚀`, "success"))
      .catch(() => showToast("Không thể sao chép link", "error"));
  };

  const load = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true);
    try {
      const [w, cs, m, sh, tx, st, ac, rw, rd, ch] = await Promise.all([
        api.getWallet(), api.listChallenges(), api.myChallenges(), api.getShop(), api.getTransactions(),
        api.getMyStats(), api.getMyActivities(),
        // Rewards lỗi không được kéo sập cả app — degrade thành ẩn thẻ thưởng.
        api.getRewards().catch(() => null),
        api.getRedemptions().catch(() => []),
        api.getCharitiesStats().catch(() => ({ "1001": 0, "1002": 0 })),
      ]);
      setWallet(w); setChallenges(cs); setMine(m); setShop(sh); setTxs(tx); setStats(st); setActs(ac); setRewards(rw); setRedemptions(rd); setCharityStats(ch);
    } catch (e) {
      if (e.status === 401) { onLogout(); }
      else showToast(`Lỗi tải dữ liệu: ${e.message}`, "error");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // ===== Deep-link: ?join=<challengeID> =====
  useEffect(() => {
    const url = new URL(window.location.href);
    const joinID = url.searchParams.get("join");
    if (joinID && challenges.length > 0) {
      // Clean URL
      window.history.replaceState({}, document.title, "/");
      const target = challenges.find(c => c.id === joinID);
      if (target) {
        if (target.joined) {
          showToast(`Bạn đã tham gia kèo "${target.title}" rồi!`, "info");
        } else {
          setJoining(target);
        }
      } else {
        showToast("Không tìm thấy kèo này. Có thể đã kết thúc hoặc link không hợp lệ.", "error");
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [challenges]);

  const switchTab = (k) => {
    setTab(k);
    setTabKey(prev => prev + 1);
    load();
  };

  const act = async (fn, okMsg, okType) => {
    setBusy(true);
    try { await fn(); await load(); if (okMsg) showToast(okMsg, okType || detectToastType(okMsg)); }
    catch (e) { showToast(e.status === 402 ? "Không đủ điểm — mua thêm ở tab Ví ⭐" : `Lỗi: ${e.message}`, "error"); }
    finally { setBusy(false); }
  };

  const doCheckin = () => act(async () => {
    const res = await api.checkIn();
    if (res.capped && res.points_granted === 0) {
      showToast("Đã check-in — hôm nay bạn chạm trần 100 điểm thưởng/ngày rồi 💪", "info");
    } else {
      showToast(`Check-in thành công! +${res.points_granted} điểm vào ví ✨`, "success");
    }
  }, null);

  // Filter + sort challenges
  const filteredChallenges = challenges
    .filter(c => sportFilter === null || c.sport === sportFilter)
    .sort((a, b) => {
      if (sortKey === "pot") return (b.stake_points * b.participants) - (a.stake_points * a.participants);
      if (sortKey === "participants") return b.participants - a.participants;
      return 0; // default: server order
    });

  const totalPot = challenges.reduce((s, c) => s + c.stake_points * c.participants, 0);

  return (
    <div className="min-h-screen flex justify-center selection:bg-lime-500/30" style={{ background: "#050505", fontFamily: "'Outfit', sans-serif" }}>
      <div className="relative w-full max-w-[400px] min-h-screen flex flex-col hide-scrollbar" style={{ background: T.bg }}>
        <>
          {/* Header */}
          <div className="px-6 pt-8 pb-5 flex items-center justify-between sticky top-0 z-20 glass-panel" style={{ background: "rgba(9,11,14,0.85)", borderBottom: `1px solid ${T.line}` }}>
            <div>
              <div className="text-3xl leading-none text-glow tracking-tight" style={{ fontFamily: "'Archivo Black', sans-serif", color: T.text }}>
                KÈO<span style={{ color: T.brand }}>.</span>
              </div>
              <div className="text-[10px] mt-1.5 uppercase tracking-widest font-bold" style={{ color: T.brand }}>Thử thách chính mình và bạn bè</div>
            </div>
            <div className="flex items-center gap-2">
              {/* Refresh button */}
              <button
                onClick={() => load(true)}
                disabled={refreshing}
                className="p-2.5 rounded-xl transition-all active:scale-95 hover:bg-white/5"
                style={{ background: T.card, border: `1px solid ${T.line}`, color: refreshing ? T.brand : T.textDim }}
                title="Tải lại dữ liệu"
              >
                <RefreshCw size={15} className={refreshing ? "animate-spin" : ""} />
              </button>
              {/* Wallet */}
              <button onClick={() => switchTab("wallet")} className="text-right rounded-2xl px-4 py-2.5 transition-transform active:scale-95" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                <div className="text-[10px] uppercase tracking-widest font-bold mb-0.5" style={{ color: T.textDim }}>Ví điểm</div>
                <div className="text-[15px] font-bold text-glow" style={{ ...MONO, color: T.brand }}>{animatedWallet.toLocaleString("vi-VN")} ⭐</div>
              </button>
            </div>
          </div>

          {/* Nội dung */}
          <div className="flex-1 overflow-y-auto px-5 pt-6 pb-32">
            {tab === "discover" && (
              <div key={tabKey} className="fade-in-up">
                <div className="rounded-3xl p-6 mb-6 relative overflow-hidden group" style={{ background: T.card, border: `1px solid ${T.brand}40` }}>
                  <div className="absolute top-0 left-0 w-full h-1 stripe-animate" />
                  <div className="text-[11px] uppercase tracking-widest mb-1.5 font-bold" style={{ color: T.textDim }}>Tổng quỹ điểm đang treo</div>
                  <div className="text-4xl font-black text-glow tracking-tight" style={{ ...MONO, color: T.brand }}>
                    {totalPot.toLocaleString("vi-VN")} <span className="text-lg">pts</span>
                  </div>
                  <div className="text-[12px] mt-3 font-medium leading-relaxed" style={{ color: T.textDim }}>
                    Bỏ cuộc mất điểm cược · Về đích chia quỹ<br/>Đổi điểm lấy đồ thể thao & vé giải chạy
                  </div>
                </div>

                <button onClick={() => setCreating(true)} className="w-full mb-5 py-4 rounded-2xl font-bold text-sm flex items-center justify-center gap-2 btn-neon active:scale-95 transition-transform"
                  style={{ background: T.brand, color: T.bg, boxShadow: "0 4px 15px rgba(204,255,0,0.15)" }}>
                  + Tạo kèo mới
                </button>

                {/* Filter & Sort */}
                <FilterBar sportFilter={sportFilter} setSportFilter={setSportFilter} sortKey={sortKey} setSortKey={setSortKey} />

                <div className="text-sm font-bold mb-4 uppercase tracking-widest flex items-center justify-between" style={{ color: T.textDim }}>
                  <span>Kèo đang mở</span>
                  {sportFilter && <span className="text-xs normal-case font-semibold" style={{ color: T.brand }}>{filteredChallenges.length} kèo</span>}
                </div>

                {loading ? (
                  <>{[0,1,2].map(i => <SkeletonChallengeCard key={i} />)}</>
                ) : filteredChallenges.length === 0 ? (
                  <div className="rounded-2xl p-8 text-center" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                    <div className="text-4xl mb-3">🏋️</div>
                    <div className="text-sm font-semibold mb-1" style={{ color: T.text }}>
                      {sportFilter ? `Không có kèo ${SPORTS[sportFilter]?.label}` : "Chưa có kèo nào"}
                    </div>
                    <div className="text-xs" style={{ color: T.textDim }}>
                      {sportFilter ? "Thử xem tất cả bộ môn hoặc tạo kèo mới!" : 'Bấm "+ Tạo kèo" để mở kèo đầu tiên!'}
                    </div>
                  </div>
                ) : filteredChallenges.map((c, i) => (
                  <div key={c.id} style={{ animationDelay: `${i * 50}ms` }}>
                    <ChallengeCard c={c} onJoin={setJoining} onBoard={setBoard} onShare={handleShare} />
                  </div>
                ))}
              </div>
            )}

            {tab === "mine" && (
              <div key={tabKey} className="fade-in-up">
                <StreakCard stats={stats} activities={acts} />
                
                <button onClick={() => setCreating(true)} className="w-full mb-6 mt-2 py-4 rounded-2xl font-bold text-sm flex items-center justify-center gap-2 btn-neon active:scale-95 transition-transform"
                  style={{ background: T.brand, color: T.bg, boxShadow: "0 4px 15px rgba(204,255,0,0.15)" }}>
                  + Tạo kèo mới
                </button>

                <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Kèo của tôi ({mine.length})</div>
                {loading ? (
                  <>{[0,1].map(i => <SkeletonMyCard key={i} />)}</>
                ) : mine.length === 0 ? (
                  <div className="rounded-2xl p-8 text-center mb-6" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                    <div className="text-4xl mb-3">🎯</div>
                    <div className="text-sm font-semibold mb-1" style={{ color: T.text }}>Chưa có kèo nào</div>
                    <div className="text-xs" style={{ color: T.textDim }}>Qua tab Khám phá để chọn thử thách!</div>
                  </div>
                ) : mine.map((c) => (
                  <MyChallengeCard key={c.challenge_id} c={c} busy={busy} onBoard={setBoard} onShare={handleShare}
                    onSync={(mc) => act(
                      () => api.syncHealthDemo(mc.source, mc.sport, mc.goal_type, mc.goal_value),
                      "Đã đồng bộ dữ liệu hôm nay qua /v1/health-sync ✓", "success")} />
                ))}
                <div className="mt-8"><ActivityFeed activities={acts} /></div>
              </div>
            )}

            {tab === "shop" && (
              <div key={tabKey} className="fade-in-up">
                {/* Widget Vinh Danh Quyên Góp Từ Thiện */}
                <div className="rounded-3xl p-5 mb-6 text-left relative overflow-hidden" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="flex items-center gap-2 mb-3">
                    <Heart size={16} className="animate-pulse" style={{ color: T.red }} />
                    <div className="text-xs font-bold uppercase tracking-wider" style={{ color: T.text }}>🎗️ Quỹ Cộng Đồng Kèo</div>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    {Object.entries(CHARITIES).map(([k, v]) => (
                      <div key={k} className="p-3 rounded-2xl" style={{ background: T.paper, border: `1px solid ${T.line}` }}>
                        <div className="flex items-center gap-1.5 mb-1.5">
                          <span className="text-base">{v.logo}</span>
                          <div className="text-[10px] font-bold truncate" style={{ color: T.textDim }} title={v.name}>{v.name}</div>
                        </div>
                        <div className="text-xs font-black text-glow" style={{ ...MONO, color: v.color }}>
                          {Number(charityStats[k] || 0).toLocaleString("vi-VN")} pts
                        </div>
                      </div>
                    ))}
                  </div>
                </div>

                <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Đổi điểm lấy thưởng</div>
                {loading ? (
                  <>{[0,1,2].map(i => <div key={i} className="skeleton h-20 rounded-2xl mb-3" />)}</>
                ) : shop.length === 0 ? (
                  <div className="rounded-2xl p-8 text-center" style={{ background: T.card }}>
                    <div className="text-4xl mb-3">🎁</div>
                    <div className="text-sm" style={{ color: T.textDim }}>Shop đang cập nhật...</div>
                  </div>
                ) : shop.map((i) => {
                  const enough = wallet.available >= i.cost;
                  return (
                    <div key={i.sku} className="flex items-center gap-4 rounded-2xl p-4 mb-3 transition-all hover:bg-zinc-800 fade-in-up" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                      <div className="w-12 h-12 rounded-xl flex items-center justify-center shrink-0" style={{ background: T.bg }}>
                        {(() => {
                          const Icon = SKU_ICONS[i.sku] || Gift;
                          return <Icon size={24} color={T.brand} />;
                        })()}
                      </div>
                      <div className="min-w-0 flex-1 text-[14px] font-bold leading-tight" style={{ color: T.text }}>{i.name}</div>
                      <button disabled={busy}
                        onClick={() => {
                          if (enough) {
                            setDeliveryForm(i);
                          } else {
                            setRedeemConfirm(i);
                          }
                        }}
                        className="shrink-0 text-xs font-bold px-3 py-2 rounded-full active:scale-95 transition-transform"
                        style={{ 
                          ...MONO, 
                          background: enough ? T.brand : "rgba(255,59,48,0.1)", 
                          color: enough ? T.ink : T.red,
                          border: enough ? "none" : `1px solid rgba(255,59,48,0.2)`
                        }}>
                        {Number(i.cost).toLocaleString("vi-VN")} điểm
                      </button>
                    </div>
                  );
                })}

                {/* Lịch sử đổi quà */}
                <div className="mt-8 mb-4">
                  <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Lịch sử đổi quà</div>
                  {redemptions.length === 0 ? (
                    <div className="rounded-2xl p-6 text-center text-sm" style={{ background: T.card, color: T.textDim }}>Chưa có quà đổi nào.</div>
                  ) : (
                    redemptions.map((r) => (
                      <div key={r.id} className="flex justify-between items-center rounded-xl p-4 mb-2.5 transition-all hover:bg-zinc-800" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                        <div className="min-w-0 flex-1 pr-3">
                          <div className="text-[13px] font-bold leading-tight" style={{ color: T.text }}>{r.item_name}</div>
                          <div className="text-[10px] mt-1.5" style={{ color: T.textDim }}>
                            {new Date(r.created_at).toLocaleString("vi-VN", { hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit', year: 'numeric' })}
                          </div>
                        </div>
                        <div className="flex flex-col items-end gap-1.5 shrink-0">
                          <div className="text-xs font-bold" style={{ ...MONO, color: T.text }}>-{Number(r.cost_points).toLocaleString("vi-VN")} ⭐</div>
                          {getRedemptionStatusBadge(r.status)}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            )}

            {tab === "wallet" && (
              <div key={tabKey} className="fade-in-up">
                <div className="rounded-3xl p-6 mb-6" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="flex justify-between mb-2">
                    <div>
                      <div className="text-[11px] uppercase tracking-widest font-bold mb-1" style={{ color: T.textDim }}>Khả dụng</div>
                      <div className="text-3xl font-black text-glow" style={{ ...MONO, color: T.brand }}>{animatedWallet.toLocaleString("vi-VN")} điểm</div>
                    </div>
                    <div className="text-right">
                      <div className="text-[11px] uppercase tracking-widest font-bold mb-1" style={{ color: T.textDim }}>Đang khóa cược 🔒</div>
                      <div className="text-xl font-bold mt-2" style={{ ...MONO, color: T.textDim }}>{fmtP(wallet.locked)}</div>
                    </div>
                  </div>
                  <div className="text-[11px] font-medium mt-4" style={{ color: T.textDim }}>Điểm dùng để cược và đổi thưởng, không rút thành tiền mặt.</div>
                </div>

                {/* Điểm thưởng: check-in hàng ngày + km đi bộ/chạy bộ từ Strava */}
                <div className="rounded-3xl p-5 mb-6" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-widest font-bold mb-1" style={{ color: T.textDim }}>
                        <Sparkles size={12} className="inline mr-1" style={{ color: T.brand }} />Tổng điểm thưởng đã nhận
                      </div>
                      <div className="text-2xl font-black" style={{ ...MONO, color: T.brand }}>
                        {(rewards?.total_points ?? 0).toLocaleString("vi-VN")} điểm
                      </div>
                    </div>
                    <button onClick={doCheckin} disabled={busy || !rewards || rewards.checked_in_today}
                      className="rounded-2xl px-4 py-3 font-bold text-sm shrink-0 active:scale-95 transition-all disabled:active:scale-100"
                      style={rewards?.checked_in_today
                        ? { background: T.line, color: T.textDim }
                        : { background: T.brand, color: "#000" }}>
                      {rewards?.checked_in_today ? "Đã check-in hôm nay ✓" : "Check-in +1 ⭐"}
                    </button>
                  </div>
                  <div className="text-[11px] font-medium mt-3" style={{ color: T.textDim }}>
                    Check-in mỗi ngày +1 điểm · mỗi km đi bộ/chạy bộ (Strava) +1 điểm — cộng thẳng vào ví. Trần thưởng 100 điểm/ngày.
                  </div>
                </div>

                <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Nạp Điểm (Chuyển khoản)</div>
                <div className="grid grid-cols-2 gap-3 mb-8">
                  {PACKS.map((p) => (
                    <button key={p.pts} disabled={busy}
                      onClick={() => act(async () => {
                        const order = await api.buyPack(p.pts);
                        setPaymentQR(order.order_url);
                      }, null)}
                      className="rounded-2xl p-4 text-left active:scale-95 transition-all group hover:bg-zinc-800" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                      <div className="text-xl font-bold" style={{ ...MONO, color: T.text }}>
                        {p.pts.toLocaleString("vi-VN")} <span className="text-xs font-semibold" style={{ color: T.textDim }}>pts</span>
                      </div>
                      {p.bonus > 0 && <div className="text-[11px] font-bold mt-1" style={{ color: T.brand }}>+{p.bonus} thưởng</div>}
                      <div className="text-xs mt-3 inline-block px-3 py-1.5 rounded-full font-bold group-hover:bg-[#CCFF00] group-hover:text-black transition-colors" style={{ background: T.line, color: T.text }}>{p.price}</div>
                    </button>
                  ))}
                </div>

                <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Lịch sử giao dịch</div>
                {loading ? (
                  <>{[0,1,2].map(i => <div key={i} className="skeleton h-12 rounded-xl mb-2" />)}</>
                ) : txs.length === 0 ? (
                  <div className="rounded-2xl p-6 text-center text-sm" style={{ background: T.card, color: T.textDim }}>Chưa có giao dịch nào.</div>
                ) : txs.map((t) => (
                  <div key={t.id} className="flex justify-between items-center rounded-xl px-4 py-3 mb-2" style={{ background: T.card }}>
                    <div className="text-[13px] pr-3" style={{ color: T.text }}>{txnLabel(t)}</div>
                    <div className="text-sm font-bold shrink-0" style={{ ...MONO, color: t.delta_available > 0 ? T.green : T.ink }}>
                      {t.delta_available > 0 ? "+" : ""}{t.delta_available.toLocaleString("vi-VN")}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {tab === "account" && (
              <div key={tabKey} className="pb-8 fade-in-up">
                {/* Avatar + Tên */}
                <div className="flex flex-col items-center mb-5">
                  {userProfile?.avatar ? (
                    <img src={userProfile.avatar} alt="avatar" className="w-16 h-16 rounded-full mb-3 border-2" style={{ borderColor: T.brand }} />
                  ) : (
                    <div className="w-16 h-16 rounded-full mb-3 flex items-center justify-center" style={{ background: T.card, border: `2px solid ${T.brand}` }}>
                      <User size={30} style={{ color: T.brand }} />
                    </div>
                  )}
                  <div className="text-lg font-bold" style={{ color: T.text }}>{userProfile?.name || 'Người dùng'}</div>
                  <div className="text-xs mt-0.5" style={{ color: T.textDim }}>{userProfile?.email}</div>
                </div>

                {/* Thống kê nhanh */}
                <div className="grid grid-cols-3 gap-3 mb-5">
                  <div className="rounded-2xl p-3 text-center" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                    <div className="text-lg font-bold" style={{ ...MONO, color: T.brand }}>{animatedWallet.toLocaleString('vi-VN')}</div>
                    <div className="text-[10px] uppercase tracking-widest font-bold mt-1" style={{ color: T.textDim }}>Điểm</div>
                  </div>
                  <div className="rounded-2xl p-3 text-center" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                    <div className="text-lg font-bold" style={{ ...MONO, color: T.text }}>{mine.length}</div>
                    <div className="text-[10px] uppercase tracking-widest font-bold mt-1" style={{ color: T.textDim }}>Kèo</div>
                  </div>
                  <div className="rounded-2xl p-3 text-center" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                    <div className="text-lg font-bold" style={{ ...MONO, color: T.text }}>{stats?.win_rate || '0'}%</div>
                    <div className="text-[10px] uppercase tracking-widest font-bold mt-1" style={{ color: T.textDim }}>Thắng</div>
                  </div>
                </div>

                {/* Menu items */}
                <div className="rounded-2xl overflow-hidden mb-5" style={{ background: T.card, border: `1px solid ${T.line}` }}>
                  {[
                    { label: 'Lịch sử giao dịch', action: () => switchTab('wallet') },
                    { label: 'Kèo của tôi', action: () => switchTab('mine') },
                    {
                      label: 'Kết nối Strava ⚡',
                      action: async () => {
                        const token = await api.currentToken();
                         const stravaClientID = "265299";
                        const redirectURI = `${window.location.origin}/v1/oauth/strava/callback`;
                        window.location.href = `https://www.strava.com/oauth/authorize?client_id=${stravaClientID}&redirect_uri=${encodeURIComponent(redirectURI)}&response_type=code&approval_prompt=auto&scope=read,activity:read_all&state=${token}`;
                      }
                    },
                    ...(isAdmin ? [{ label: 'Trang Quản trị 🛠️', action: () => switchTab('admin') }] : []),
                  ].map((item, i) => (
                    <button key={i} onClick={item.action} className="w-full flex items-center justify-between px-5 py-4 transition-colors hover:bg-white/5"
                      style={{ borderBottom: `1px solid ${T.line}` }}>
                      <span className="text-sm font-semibold" style={{ color: T.text }}>{item.label}</span>
                      <ChevronRight size={16} style={{ color: T.textDim }} />
                    </button>
                  ))}
                </div>

                {/* Đăng xuất */}
                <button onClick={onLogout}
                  className="w-full py-4 rounded-2xl font-bold text-sm flex items-center justify-center gap-2 transition-transform active:scale-95"
                  style={{ background: 'rgba(255,59,48,0.1)', color: '#FF3B30', border: '1px solid rgba(255,59,48,0.2)' }}>
                  <LogOut size={18} />
                  Đăng xuất
                </button>

                <div className="text-center text-[11px] mt-4" style={{ color: T.textDim }}>Phiên bản 1.1.0</div>
              </div>
            )}

            {tab === "admin" && isAdmin && (
              <div key={tabKey} className="pb-8 fade-in-up">
                <AdminDashboard showToast={showToast} />
              </div>
            )}
          </div>


          {/* Tab bar - Floating Pill */}
          <div className="fixed bottom-6 w-full max-w-[400px] px-5 z-30 pointer-events-none">
            <div className="glass-panel rounded-[32px] p-2 flex justify-between items-center pointer-events-auto">
              {[
                { k: "discover", icon: Flame, label: "Khám phá" },
                { k: "mine", icon: Target, label: "Của tôi" },
                { k: "shop", icon: ShoppingBag, label: "Shop" },
                { k: "wallet", icon: Wallet, label: "Ví" },
                { k: "account", icon: User, label: "Tài khoản" },
              ].map((t) => {
                const TabIcon = t.icon;
                return (
                  <button key={t.k} onClick={() => switchTab(t.k)} className="relative flex-1 py-3 flex flex-col items-center gap-1.5 rounded-2xl transition-all"
                    style={{ background: tab === t.k ? "rgba(255,255,255,0.06)" : "transparent" }}>
                    <span className={`transition-transform ${tab === t.k ? "scale-110 text-[#CCFF00]" : "text-[#8B949E]"}`}>
                      <TabIcon size={24} strokeWidth={tab === t.k ? 2.5 : 2} />
                    </span>
                    <span className="text-[10px] font-bold tracking-wide" style={{ color: tab === t.k ? T.text : T.textDim }}>{t.label}</span>
                    {tab === t.k && <span className="absolute bottom-1 w-8 h-1 rounded-full" style={{ background: T.brand, boxShadow: `0 0 8px ${T.brand}` }} />}
                  </button>
                );
              })}
            </div>
          </div>

          {/* PAYMENT QR MODAL */}
          {paymentQR && (
            <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={() => setPaymentQR(null)}>
              <div className="w-full max-w-sm rounded-3xl p-6 relative overflow-hidden text-center scale-in" style={{ background: T.card, border: `1px solid ${T.line}` }} onClick={e => e.stopPropagation()}>
                <h2 className="text-xl font-black mb-4 uppercase tracking-wider" style={{ color: T.text }}>Thanh Toán</h2>
                <div className="bg-white p-4 rounded-2xl mx-auto mb-4 inline-block">
                  <img src={paymentQR} alt="VietQR" className="w-48 h-48 object-contain" />
                </div>
                <p className="text-[13px] font-medium leading-relaxed mb-6" style={{ color: T.textDim }}>
                  Quét mã bằng App Ngân hàng bất kỳ.<br />Điểm sẽ tự động cộng sau 5-10s!
                </p>
                <button className="w-full btn-neon font-bold py-3.5 rounded-xl uppercase tracking-widest text-[13px] active:scale-[0.98] transition-transform" 
                  style={{ background: T.brand, color: T.bg, boxShadow: "0 4px 15px rgba(204,255,0,0.25)" }}
                  onClick={() => { setPaymentQR(null); load(true); }}>
                  Tôi đã chuyển tiền
                </button>
              </div>
            </div>
          )}

          {/* Smart Toast */}
          {toast && (
            <NotificationToast
              msg={toast.msg}
              type={toast.type}
              duration={2800}
              onDone={() => setToast(null)}
            />
          )}

          {redeemConfirm && (
            <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={() => setRedeemConfirm(null)}>
              <div className="w-full max-w-sm rounded-3xl p-6 relative overflow-hidden text-center scale-in" style={{ background: T.card, border: `1px solid ${T.line}` }} onClick={e => e.stopPropagation()}>
                <div className="text-xl font-black mb-4 uppercase tracking-wider text-red-500" style={{ color: T.red }}>Không đủ điểm</div>
                <div className="text-sm font-semibold mb-6 leading-relaxed" style={{ color: T.text }}>
                  Bạn cần có {Number(redeemConfirm.cost).toLocaleString("vi-VN")} điểm để đổi "{redeemConfirm.name}". Hiện tại bạn chỉ có {Number(wallet.available).toLocaleString("vi-VN")} điểm.
                </div>
                <button onClick={() => { setRedeemConfirm(null); switchTab("wallet"); }}
                  className="w-full font-bold py-3.5 rounded-xl uppercase tracking-widest text-[13px]"
                  style={{ background: T.red, color: T.text, boxShadow: "0 0 15px rgba(255, 59, 48, 0.4)" }}>
                  Nạp thêm ở tab Ví ⭐
                </button>
              </div>
            </div>
          )}

          {deliveryForm && (
            <DeliveryModal item={deliveryForm} busy={busy} onClose={() => setDeliveryForm(null)}
              onConfirm={(f) => act(
                async () => {
                  await api.redeem(deliveryForm.sku, f);
                  setDeliveryForm(null);
                },
                `Đã đổi: ${deliveryForm.name} 🎉`, "success"
              )} />
          )}

          <LeaderboardSheet challengeID={board} onClose={() => setBoard(null)} />
          <JoinModal c={joining} wallet={wallet} busy={busy} setTab={switchTab}
            onConfirm={(c) => act(async () => { await api.joinChallenge(c.id); setJoining(null); }, "Đã chốt kèo! Điểm cược được khóa 🔒", "success")}
            onClose={() => setJoining(null)} />
          <CreateSheet open={creating} busy={busy} onClose={() => setCreating(false)} wallet={wallet} setTab={switchTab}
            onCreate={(c) => act(async () => { await api.createChallenge(c); setCreating(false); switchTab("mine"); }, "Kèo của bạn đã lên sàn 🎉", "success")} />
        </>
      </div>
    </div>
  );
}

const Empty = ({ children }) => (
  <div className="rounded-2xl p-8 text-center text-sm" style={{ background: T.card, color: T.textDim }}>{children}</div>
);

function DeliveryModal({ item, busy, onClose, onConfirm }) {
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [address, setAddress] = useState("");
  const [note, setNote] = useState("");

  const handleSubmit = (e) => {
    e.preventDefault();
    if (!name.trim() || !phone.trim() || !address.trim()) {
      alert("Vui lòng điền đầy đủ Họ tên, Số điện thoại và Địa chỉ nhận hàng!");
      return;
    }
    onConfirm({ name, phone, address, note });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm" onClick={onClose}>
      <form className="w-full max-w-sm rounded-3xl p-6 relative overflow-y-auto max-h-[85vh] scale-in" 
        style={{ background: T.card, border: `1px solid ${T.line}` }} 
        onClick={e => e.stopPropagation()} onSubmit={handleSubmit}>
        <h2 className="text-xl font-black mb-4 uppercase tracking-wider text-center" style={{ color: T.text }}>Thông tin giao hàng</h2>
        <div className="text-sm font-semibold mb-4 text-center" style={{ color: T.brand }}>
          Đổi quà: {item.name} ({Number(item.cost).toLocaleString("vi-VN")} ⭐)
        </div>

        <Label>Họ tên người nhận</Label>
        <input type="text" value={name} onChange={e => setName(e.target.value)} required
          placeholder="Nhập họ và tên"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-3"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label>Số điện thoại</Label>
        <input type="tel" value={phone} onChange={e => setPhone(e.target.value)} required
          placeholder="Nhập số điện thoại"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-3"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label>Địa chỉ nhận hàng</Label>
        <textarea value={address} onChange={e => setAddress(e.target.value)} required rows={3}
          placeholder="Số nhà, tên đường, phường/xã, quận/huyện, tỉnh/thành phố"
          className="w-full px-3 py-2 rounded-xl text-sm font-semibold outline-none mb-3 resize-none"
          style={{ background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

        <Label>Ghi chú thêm (Size, mùi hương, v.v.)</Label>
        <input type="text" value={note} onChange={e => setNote(e.target.value)}
          placeholder="Ví dụ: Size M, Mùi hương Bạc Hà"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-5"
          style={{ ...MONO, background: T.bg, color: T.text, border: `1px solid ${T.line}` }} />

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

function txnLabel(t) {
  const names = {
    purchase: "Nạp điểm",
    stake_lock: "Đặt cược kèo",
    settlement: "Nhận thưởng kèo",
    redeem: "Đổi quà",
    stake_release: "Hoàn cược",
    admin_adjust: "Admin điều chỉnh",
    reward_payout: "Thưởng luyện tập",
    challenge_reward: "Thưởng thử thách",
  };
  return names[t.type] || t.type;
}

// Wrapper xử lý Auth của Supabase và Zalo
export default function App() {
  const [session, setSession] = useState(undefined);
  const [zaloUser, setZaloUser] = useState(null);
  const [authChecking, setAuthChecking] = useState(true);
  const [zaloLoading, setZaloLoading] = useState(false);
  const [zaloError, setZaloError] = useState("");
  const zaloCallbackCalled = useRef(false);

  const parseZaloToken = (token) => {
    try {
      const parts = token.split('.');
      const base64Url = parts[1];
      const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
      const pad = base64.length % 4;
      const paddedBase64 = pad ? base64 + '='.repeat(4 - pad) : base64;
      const jsonPayload = decodeURIComponent(atob(paddedBase64).split('').map(function(c) {
          return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
      }).join(''));
      const payload = JSON.parse(jsonPayload);
      return {
        name: payload.user_metadata?.full_name || 'Người dùng Zalo',
        email: payload.email || '',
        avatar: payload.user_metadata?.avatar_url || null,
        role: payload.app_metadata?.role || null,
      };
    } catch (e) {
      localStorage.removeItem("keo_jwt_token");
      return null;
    }
  };

  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.pathname === "/oauth/zalo/callback") {
      if (zaloCallbackCalled.current) return;
      zaloCallbackCalled.current = true;

      const code = url.searchParams.get("code");
      const verifier = localStorage.getItem("zalo_code_verifier");
      
      if (code && verifier) {
        setZaloLoading(true);
        window.history.replaceState({}, document.title, "/");
        
        fetch("/v1/auth/zalo", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ code, code_verifier: verifier })
        })
        .then(async res => {
          if (!res.ok) {
            let detail = "Đăng nhập Zalo thất bại";
            try {
              const errData = await res.json();
              if (errData && errData.error) detail = errData.error;
            } catch (e) {}
            throw new Error(detail);
          }
          return res.json();
        })
        .then(async data => {
          const zaloAccessToken = data.zalo_access_token;
          if (!zaloAccessToken) {
            throw new Error("Không nhận được token xác thực từ Zalo");
          }

          const graphRes = await fetch(`https://graph.zalo.me/v2.0/me?fields=id,name,picture&access_token=${zaloAccessToken}`);
          if (!graphRes.ok) {
            throw new Error("Không thể lấy thông tin cá nhân từ Zalo");
          }
          const profile = await graphRes.json();
          if (profile.error) {
            throw new Error(`Lỗi Zalo Profile: ${profile.message || profile.error}`);
          }

          const verifyRes = await fetch("/v1/auth/zalo/verify", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              zalo_access_token: zaloAccessToken,
              id: profile.id,
              name: profile.name,
              picture: profile.picture?.data?.url || ""
            })
          });
          if (!verifyRes.ok) {
            let detail = "Xác thực tài khoản thất bại";
            try {
              const errData = await verifyRes.json();
              if (errData && errData.error) detail = errData.error;
            } catch (e) {}
            throw new Error(detail);
          }
          const appTokenData = await verifyRes.json();
          if (appTokenData.access_token) {
            localStorage.setItem("keo_jwt_token", appTokenData.access_token);
            const user = parseZaloToken(appTokenData.access_token);
            setZaloUser(user);
          }
        })
        .catch(err => {
          setZaloError(err.message);
        })
        .finally(() => {
          setZaloLoading(false);
          setAuthChecking(false);
        });
      } else {
        setAuthChecking(false);
      }
    } else {
      const token = localStorage.getItem("keo_jwt_token");
      if (token) {
        const user = parseZaloToken(token);
        setZaloUser(user);
      }
      setAuthChecking(false);
    }

    api.supabase.auth.getSession().then(({ data: { session } }) => {
      setSession(session);
    });

    const {
      data: { subscription },
    } = api.supabase.auth.onAuthStateChange((_event, session) => {
      setSession(session);
      if (!session && !localStorage.getItem("keo_jwt_token")) {
        setZaloUser(null);
      }
    });

    return () => subscription.unsubscribe();
  }, []);

  const handleLogout = useCallback(async () => {
    localStorage.removeItem("keo_jwt_token");
    setZaloUser(null);
    setSession(null);
    await api.logout();
  }, []);

  if (zaloLoading || authChecking) {
    return (
      <div className="h-screen bg-grid flex flex-col items-center justify-center font-bold text-lg" style={{ background: T.bg, color: T.text }}>
        <div className="text-5xl mb-4 text-glow" style={{ fontFamily: "'Archivo Black', sans-serif", color: T.text }}>KÈO.</div>
        <div className="text-sm font-semibold animate-pulse" style={{ color: T.brand }}>Đang kiểm tra phiên làm việc...</div>
      </div>
    );
  }

  const loggedIn = !!session || !!zaloUser;

  let userProfile = null;
  if (zaloUser) {
    userProfile = zaloUser;
  } else if (session?.user) {
    const u = session.user;
    userProfile = {
      name: u.user_metadata?.full_name || u.user_metadata?.name || u.email?.split('@')[0] || 'Người dùng',
      email: u.email || '',
      avatar: u.user_metadata?.avatar_url || u.user_metadata?.picture || null,
      role: u.app_metadata?.role || null,
    };
  }

  if (session === undefined && !zaloUser) return <div className="h-screen bg-grid flex items-center justify-center font-bold text-5xl" style={{ background: T.bg, color: T.brand }}>KÈO.</div>;
  if (!loggedIn) return (
    <div className="h-screen flex flex-col bg-grid" style={{ background: T.bg }}>
      <Login onDone={() => {}} />
      {zaloError && (
        <div className="absolute top-6 inset-x-6 z-50 rounded-2xl px-5 py-4 text-xs font-bold text-center border backdrop-blur-sm bg-red-500/10"
          style={{ borderColor: T.red, color: T.red }}>
          {zaloError}
        </div>
      )}
    </div>
  );
  return <AppCore userProfile={userProfile} onLogout={handleLogout} />;
}
