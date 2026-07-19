// Bảng xếp hạng một kèo — bottom sheet trượt lên khi bấm vào thẻ kèo.
import { useEffect, useState } from "react";
import * as api from "./api.js";
import { T, MONO, SPORTS, fmtP, daysLeft } from "./theme.js";
import { Trophy, Medal, X } from "lucide-react";

// Màu huy chương top 3 (vàng/bạc/đồng); ngoài top 3 dùng số thứ hạng.
const MEDAL_COLORS = ["#FFD700", "#C0C0C0", "#CD7F32"];

function EntryRow({ e, rank, goalType }) {
  const pct = e.periods_total ? Math.round((e.periods_passed / e.periods_total) * 100) : 0;
  const out = e.status === "failed" || e.status === "withdrawn";

  const getUnit = (gt) => {
    if (gt === "weekly_distance_km") return "km";
    if (gt === "daily_steps") return "bước";
    if (gt === "weekly_sessions") return "buổi";
    return "";
  };
  const unit = getUnit(goalType);

  return (
    <div className={`rounded-2xl px-4 py-3.5 mb-2.5 transition-all ${e.is_me ? "border" : ""}`}
      style={{
        background: e.is_me ? "rgba(204,255,0,0.08)" : T.bg,
        borderColor: e.is_me ? T.brand : "transparent",
        opacity: out ? 0.55 : 1
      }}>
      <div className="flex items-center gap-3 mb-2.5">
        <div className="flex items-center justify-center shrink-0 w-8 h-8 rounded-full"
             style={{
               background: e.is_me ? "rgba(204,255,0,0.15)" : "rgba(255,255,255,0.04)",
               border: `1px solid ${e.is_me ? T.brand : "rgba(255,255,255,0.08)"}`
             }}>
          {rank < 3
            ? <Medal size={16} strokeWidth={2.5} style={{ color: MEDAL_COLORS[rank] }} />
            : <span className="text-[12px] font-black" style={{ color: e.is_me ? T.brand : T.text }}>{rank + 1}</span>}
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-[14px] font-bold truncate" style={{ color: T.text }}>
            {e.display_name}{e.is_me ? " (bạn)" : ""}
          </div>
          <div className="text-[11px] mt-0.5" style={{ color: T.textDim }}>
            Tích lũy: <span className="font-bold" style={{ ...MONO, color: T.brand }}>{Number(e.total_achieved).toLocaleString("vi-VN", { maximumFractionDigits: 1 })}</span> {unit}
          </div>
        </div>
        <div className="text-right shrink-0">
          <span className="text-xs font-bold block" style={{ ...MONO, color: pct >= 80 ? T.green : out ? T.red : T.text }}>
            {out ? "Rớt" : `${e.periods_passed}/${e.periods_total} kỳ`}
          </span>
          <span className="text-[10px]" style={{ color: T.textDim }}>{pct}% kỳ đạt</span>
        </div>
      </div>
      <div className="h-2 rounded-full overflow-hidden ml-11" style={{ background: "rgba(255,255,255,0.05)" }}>
        <div className="h-full rounded-full transition-all duration-1000 ease-out"
          style={{ width: `${Math.min(pct, 100)}%`, background: pct >= 80 ? T.green : T.brand, boxShadow: `0 0 8px ${pct >= 80 ? T.green : T.brand}` }} />
      </div>
    </div>
  );
}

function SkeletonEntry() {
  return (
    <div className="rounded-2xl px-4 py-3.5 mb-2.5" style={{ background: T.bg }}>
      <div className="flex items-center gap-3 mb-2.5">
        <div className="skeleton w-8 h-8 rounded-full shrink-0" />
        <div className="flex-1 space-y-2">
          <div className="skeleton h-3.5 w-1/2 rounded" />
          <div className="skeleton h-3 w-1/3 rounded" />
        </div>
      </div>
      <div className="skeleton h-2 rounded-full ml-11" />
    </div>
  );
}

export default function LeaderboardSheet({ challengeID, onClose }) {
  const [data, setData] = useState(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!challengeID) return;
    setData(null); setErr("");
    api.getLeaderboard(challengeID).then(setData).catch((e) => setErr(e.message));
  }, [challengeID]);

  if (!challengeID) return null;
  const c = data?.challenge;
  return (
    <div className="fixed inset-0 z-40 flex items-end justify-center bg-black/85 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-sm rounded-t-3xl p-6 pt-3 relative overflow-y-auto max-h-[85vh] sheet-in glass-panel"
        style={{ background: "rgba(27,31,39,0.97)", borderTop: `1px solid ${T.line}`, paddingBottom: "calc(1.5rem + env(safe-area-inset-bottom))" }}
        onClick={(e) => e.stopPropagation()}>
        {/* Drag handle + nút đóng */}
        <div className="flex items-center justify-center relative mb-3">
          <div className="w-10 h-1 rounded-full" style={{ background: T.line }} />
          <button onClick={onClose} aria-label="Đóng bảng xếp hạng"
            className="absolute right-0 -top-1 flex items-center justify-center rounded-full active:scale-90 transition-transform"
            style={{ minWidth: 40, minHeight: 40, color: T.textDim }}>
            <X size={18} />
          </button>
        </div>

        {err && (
          <div className="text-sm text-center py-8" style={{ color: T.red }}>
            <div className="font-semibold mb-3">Không tải được bảng xếp hạng</div>
            <button onClick={() => { setErr(""); setData(null); api.getLeaderboard(challengeID).then(setData).catch((e) => setErr(e.message)); }}
              className="px-4 py-2.5 rounded-xl font-bold text-xs active:scale-95 transition-transform"
              style={{ background: T.brand, color: T.bg }}>Thử lại</button>
          </div>
        )}

        {!data && !err && (
          <div className="pt-2">
            <div className="skeleton h-5 w-1/2 mx-auto mb-2 rounded" />
            <div className="skeleton h-3 w-2/3 mx-auto mb-5 rounded" />
            {[0, 1, 2, 3].map((i) => <SkeletonEntry key={i} />)}
          </div>
        )}

        {c && (() => {
          const sport = SPORTS[c.sport] || { icon: Trophy };
          const Icon = sport.icon;
          return (<>
            <div className="flex items-center gap-3 mb-1 text-center justify-center">
              <Icon size={22} strokeWidth={2} color={T.brand} />
              <div className="text-lg font-black uppercase tracking-wider" style={{ color: T.text }}>{c.title}</div>
            </div>
            <div className="text-xs mb-5 text-center" style={{ color: T.textDim }}>
              {c.status === "settled" ? "đã chốt sổ" : `còn ${daysLeft(c.end_at)} ngày`} · {data.entries.length} người
              · quỹ <b style={MONO}>{fmtP(data.pot)}</b>
            </div>
            <div className="text-[10px] uppercase tracking-widest font-bold mb-3 text-center" style={{ color: T.textDim }}>
              Bảng xếp hạng · đạt ≥80% kỳ để chia quỹ
            </div>
            {data.entries.map((e, i) => <EntryRow key={e.user_id} e={e} rank={i} goalType={c.goal_type} />)}
          </>);
        })()}
      </div>
    </div>
  );
}
