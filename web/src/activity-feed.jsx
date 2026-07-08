// Chuỗi ngày tập (streak) + feed hoạt động gần đây — dữ liệu đã xác thực
// từ bảng activities, cùng nguồn với recompute tiến độ kèo.
import { T, MONO, SPORTS, SOURCES } from "./theme.js";

const km = (m) => (m / 1000).toLocaleString("vi-VN", { maximumFractionDigits: 1 });
const mins = (s) => Math.round(s / 60);

export function StreakCard({ stats }) {
  if (!stats) return null;
  const s = stats.streak_days;
  return (
    <div className="rounded-2xl p-4 mb-4" style={{ background: T.ink }}>
      <div className="flex items-center justify-between">
        <div>
          <div className="text-[10px] uppercase tracking-widest mb-1" style={{ color: "#9BA1A8" }}>Chuỗi ngày tập</div>
          <div className="text-3xl font-bold" style={{ ...MONO, color: s > 0 ? T.brand : "#9BA1A8" }}>
            {s > 0 ? "🔥 " : ""}{s} <span className="text-base">ngày</span>
          </div>
        </div>
        <div className="text-right text-[11px] leading-relaxed" style={{ color: "#9BA1A8" }}>
          Tuần này<br />
          <b style={{ ...MONO, color: "#fff" }}>{km(stats.week_distance_m)} km</b> · <b style={{ ...MONO, color: "#fff" }}>{stats.week_sessions}</b> buổi<br />
          <b style={{ ...MONO, color: "#fff" }}>{stats.week_active_days}</b> ngày hoạt động
        </div>
      </div>
      {s === 0 && <div className="text-[11px] mt-2" style={{ color: "#9BA1A8" }}>
        Tập hôm nay để nhóm lửa chuỗi ngày đầu tiên 🔥
      </div>}
    </div>
  );
}

function fmtWhen(iso) {
  const d = new Date(iso);
  const today = new Date().toLocaleDateString("sv-SE", { timeZone: "Asia/Ho_Chi_Minh" });
  const day = d.toLocaleDateString("sv-SE", { timeZone: "Asia/Ho_Chi_Minh" });
  if (day === today) return "hôm nay";
  return d.toLocaleDateString("vi-VN", { day: "numeric", month: "numeric", timeZone: "Asia/Ho_Chi_Minh" });
}

function actSummary(a) {
  const parts = [];
  if (a.distance_m > 0) parts.push(`${km(a.distance_m)} km`);
  if (a.duration_s > 0) parts.push(`${mins(a.duration_s)} phút`);
  if (a.steps > 0) parts.push(`${a.steps.toLocaleString("vi-VN")} bước`);
  if (!parts.length) parts.push(`${a.sessions} buổi`);
  return parts.join(" · ");
}

export function ActivityFeed({ activities }) {
  if (!activities) return null;
  return (<>
    <div className="text-sm font-bold mb-3" style={{ color: T.ink }}>Hoạt động gần đây</div>
    {activities.length === 0 && (
      <div className="rounded-2xl p-6 text-center text-sm mb-4" style={{ background: T.card, color: T.gray }}>
        Chưa có hoạt động nào được ghi nhận. Kết nối Strava hoặc đồng bộ Health để bắt đầu!
      </div>
    )}
    {activities.map((a, i) => {
      const s = SPORTS[a.sport] || { icon: "🏆", label: a.sport };
      return (
        <div key={`${a.started_at}-${i}`} className="flex items-center gap-3 rounded-xl px-3.5 py-3 mb-2" style={{ background: T.card }}>
          <div className="w-9 h-9 rounded-lg flex items-center justify-center text-lg shrink-0" style={{ background: T.paper }}>{s.icon}</div>
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-bold" style={{ color: T.ink }}>
              {s.label} <span className="font-semibold" style={{ ...MONO, color: T.gray }}>{actSummary(a)}</span>
            </div>
            <div className="text-[11px]" style={{ color: T.gray }}>
              {fmtWhen(a.started_at)} · {SOURCES[a.source]?.icon} {SOURCES[a.source]?.label || a.source}
            </div>
          </div>
        </div>
      );
    })}
  </>);
}
