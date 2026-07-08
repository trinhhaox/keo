// Bảng xếp hạng một kèo — bottom sheet mở khi bấm vào thẻ kèo.
import { useEffect, useState } from "react";
import * as api from "./api.js";
import { T, MONO, SPORTS, fmtP, daysLeft } from "./theme.js";

const MEDALS = ["🥇", "🥈", "🥉"];

function EntryRow({ e, rank }) {
  const pct = e.periods_total ? Math.round((e.periods_passed / e.periods_total) * 100) : 0;
  const out = e.status === "failed" || e.status === "withdrawn";
  return (
    <div className="rounded-xl px-3 py-2.5 mb-2"
      style={{ background: e.is_me ? "#FFF7DB" : T.paper, opacity: out ? 0.55 : 1 }}>
      <div className="flex items-center gap-2 mb-1.5">
        <span className="w-6 text-center text-sm shrink-0">{MEDALS[rank] || `${rank + 1}.`}</span>
        <span className="text-[13px] font-bold flex-1 truncate" style={{ color: T.ink }}>
          {e.display_name}{e.is_me ? " (bạn)" : ""}
        </span>
        <span className="text-xs font-bold shrink-0" style={{ ...MONO, color: pct >= 80 ? T.green : out ? T.red : T.gray }}>
          {out ? "✗ rớt" : `${e.periods_passed}/${e.periods_total} kỳ`}
        </span>
      </div>
      <div className="h-2 rounded-full overflow-hidden ml-8" style={{ background: "#E7E9E7" }}>
        <div className="h-full rounded-full transition-all duration-500"
          style={{ width: `${Math.min(pct, 100)}%`, background: pct >= 80 ? T.green : T.brand }} />
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
    <div className="absolute inset-0 z-30 flex items-end" style={{ background: "rgba(21,23,27,.55)" }} onClick={onClose}>
      <div className="w-full rounded-t-3xl p-5 pb-8 max-h-[85%] overflow-y-auto"
        style={{ background: T.card }} onClick={(e) => e.stopPropagation()}>
        <div className="w-10 h-1 rounded-full mx-auto mb-4" style={{ background: T.line }} />
        {err && <div className="text-sm text-center py-6" style={{ color: T.red }}>{err}</div>}
        {!data && !err && <div className="text-sm text-center py-6" style={{ color: T.gray }}>Đang tải bảng xếp hạng...</div>}
        {c && (<>
          <div className="flex items-center gap-3 mb-1">
            <span className="text-xl">{SPORTS[c.sport]?.icon || "🏆"}</span>
            <div className="text-lg font-bold leading-tight" style={{ color: T.ink }}>{c.title}</div>
          </div>
          <div className="text-xs mb-4" style={{ color: T.gray }}>
            {c.status === "settled" ? "đã chốt sổ" : `còn ${daysLeft(c.end_at)} ngày`} · {data.entries.length} người
            · quỹ <b style={MONO}>{fmtP(data.pot)}</b>
          </div>
          <div className="text-[10px] uppercase tracking-widest font-bold mb-2" style={{ color: T.gray }}>
            Bảng xếp hạng · đạt ≥80% kỳ để chia quỹ
          </div>
          {data.entries.map((e, i) => <EntryRow key={e.user_id} e={e} rank={i} />)}
        </>)}
      </div>
    </div>
  );
}
