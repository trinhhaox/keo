// Bảng xếp hạng một kèo — bottom sheet mở khi bấm vào thẻ kèo.
import { useEffect, useState } from "react";
import * as api from "./api.js";
import { T, MONO, SPORTS, fmtP, daysLeft } from "./theme.js";
import { Trophy } from "lucide-react";

const MEDALS = ["🥇", "🥈", "🥉"];

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
        <div className="flex flex-col items-center justify-center shrink-0 w-8 h-8 rounded-full" 
             style={{ 
               background: e.is_me ? "rgba(204,255,0,0.15)" : "rgba(255,255,255,0.04)", 
               border: `1px solid ${e.is_me ? T.brand : "rgba(255,255,255,0.08)"}` 
             }}>
          <span className="text-[12px] font-black" style={{ color: e.is_me ? T.brand : T.text }}>
            {rank + 1}
          </span>
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-[14px] font-bold truncate flex items-center gap-1" style={{ color: T.text }}>
            {MEDALS[rank]} {e.display_name}{e.is_me ? " (bạn)" : ""}
          </div>
          <div className="text-[11px] mt-0.5" style={{ color: T.textDim }}>
            Tích lũy: <span className="font-bold" style={{ ...MONO, color: T.brand }}>{Number(e.total_achieved).toLocaleString("vi-VN", { maximumFractionDigits: 1 })}</span> {unit}
          </div>
        </div>
        <div className="text-right shrink-0">
          <span className="text-xs font-bold block" style={{ ...MONO, color: pct >= 80 ? T.green : out ? T.red : T.text }}>
            {out ? "✗ rớt" : `${e.periods_passed}/${e.periods_total} kỳ`}
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
    <div className="fixed inset-0 z-40 flex items-center justify-center p-4 bg-black/85 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-sm rounded-3xl p-6 relative overflow-y-auto max-h-[85vh] glass-panel"
        style={{ background: "rgba(27,31,39,0.95)", border: `1px solid ${T.line}` }} onClick={(e) => e.stopPropagation()}>
        {err && <div className="text-sm text-center py-6" style={{ color: T.red }}>{err}</div>}
        {!data && !err && <div className="text-sm text-center py-6" style={{ color: T.textDim }}>Đang tải bảng xếp hạng...</div>}
        {c && (() => {
          const sport = SPORTS[c.sport] || { icon: Trophy };
          const Icon = sport.icon;
          return (<>
            <div className="flex items-center gap-3 mb-1 text-center justify-center">
              <span className="text-brand flex items-center justify-center"><Icon size={22} strokeWidth={2} color={T.brand} /></span>
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
