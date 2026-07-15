// Chuỗi ngày tập (streak) + feed hoạt động gần đây — dữ liệu đã xác thực
// từ bảng activities, cùng nguồn với recompute tiến độ kèo.
import { T, MONO, SPORTS, SOURCES } from "./theme.js";
import { Trophy, Activity, Flame } from "lucide-react";

const km = (m) => (m / 1000).toLocaleString("vi-VN", { maximumFractionDigits: 1 });
const mins = (s) => Math.round(s / 60);

// ===== Mini Calendar 7 ngày gần nhất =====
function StreakMiniCalendar({ activities }) {
  const days = [];
  const today = new Date();
  // Tạo mảng 7 ngày từ 6 ngày trước đến hôm nay
  for (let i = 6; i >= 0; i--) {
    const d = new Date(today);
    d.setDate(today.getDate() - i);
    days.push(d.toLocaleDateString("sv-SE", { timeZone: "Asia/Ho_Chi_Minh" }));
  }

  // Set các ngày có hoạt động
  const activeDays = new Set(
    (activities || []).map((a) =>
      new Date(a.started_at).toLocaleDateString("sv-SE", { timeZone: "Asia/Ho_Chi_Minh" })
    )
  );

  const dayLabels = ["T2", "T3", "T4", "T5", "T6", "T7", "CN"];

  return (
    <div className="flex gap-1.5 mt-3">
      {days.map((dayStr, i) => {
        const isActive = activeDays.has(dayStr);
        const isToday = i === 6;
        return (
          <div key={dayStr} className="flex-1 flex flex-col items-center gap-1">
            <div
              className={`w-full aspect-square rounded-lg flex items-center justify-center transition-all ${isActive ? "pulse-dot" : ""}`}
              style={{
                background: isActive
                  ? "rgba(204,255,0,0.2)"
                  : "rgba(255,255,255,0.04)",
                border: isToday
                  ? `1px solid ${isActive ? "rgba(204,255,0,0.6)" : "rgba(255,255,255,0.15)"}`
                  : "1px solid transparent",
                boxShadow: isActive ? "0 0 8px rgba(204,255,0,0.15)" : "none",
              }}
            >
              {isActive && (
                <Flame size={12} strokeWidth={2.5} style={{ color: T.brand }} />
              )}
            </div>
            <span
              className="text-[9px] font-bold uppercase"
              style={{ color: isActive ? T.brand : T.textDim }}
            >
              {dayLabels[new Date(dayStr + "T00:00:00").getDay() === 0 ? 6 : new Date(dayStr + "T00:00:00").getDay() - 1]}
            </span>
          </div>
        );
      })}
    </div>
  );
}

export function StreakCard({ stats, activities }) {
  if (!stats) return null;
  const s = stats.streak_days;
  return (
    <div className="rounded-3xl p-6 mb-6 relative overflow-hidden fade-in-up" style={{ background: T.card, border: `1px solid ${T.line}` }}>
      <div className="absolute -top-10 -right-10 w-32 h-32 bg-lime-500/10 rounded-full blur-3xl"></div>
      <div className="flex items-center justify-between relative z-10">
        <div>
          <div className="text-[11px] uppercase tracking-widest font-bold mb-1" style={{ color: T.textDim }}>Chuỗi ngày tập</div>
          <div className="text-4xl font-black text-glow" style={{ ...MONO, color: s > 0 ? T.brand : T.textDim }}>
            {s > 0 ? "🔥 " : ""}{s} <span className="text-lg font-bold">ngày</span>
          </div>
        </div>
        <div className="text-right text-[11px] leading-relaxed font-medium" style={{ color: T.textDim }}>
          Tuần này<br />
          <b className="text-sm" style={{ ...MONO, color: T.text }}>{km(stats.week_distance_m)}</b> km · <b className="text-sm" style={{ ...MONO, color: T.text }}>{stats.week_sessions}</b> buổi<br />
          <b className="text-sm" style={{ ...MONO, color: T.text }}>{stats.week_active_days}</b> ngày HĐ
        </div>
      </div>

      {/* Mini Calendar 7 ngày */}
      <StreakMiniCalendar activities={activities} />

      {s === 0 && <div className="text-[11px] mt-3 font-semibold" style={{ color: T.brand }}>
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
    <div className="text-sm font-bold mb-4 uppercase tracking-widest" style={{ color: T.textDim }}>Hoạt động gần đây</div>
    {activities.length === 0 && (
      <div className="rounded-2xl p-6 text-center mb-4" style={{ background: T.card, border: `1px solid ${T.line}` }}>
        <div className="text-3xl mb-2">🏃</div>
        <div className="text-sm font-semibold mb-1" style={{ color: T.text }}>Chưa có hoạt động</div>
        <div className="text-xs leading-relaxed" style={{ color: T.textDim }}>
          Kết nối Strava hoặc đồng bộ Health để<br />bắt đầu ghi nhận tiến trình!
        </div>
      </div>
    )}
    {activities.map((a, i) => {
      const sport = SPORTS[a.sport] || { icon: Trophy, label: a.sport };
      const Icon = sport.icon;
      const src = SOURCES[a.source] || { icon: Activity, label: a.source };
      const SrcIcon = src.icon;
      return (
        <div key={`${a.started_at}-${i}`} className="flex items-center gap-4 rounded-2xl px-4 py-3.5 mb-2.5 fade-in-up transition-all hover:bg-zinc-800" style={{ background: T.card, border: `1px solid ${T.line}`, animationDelay: `${i * 40}ms` }}>
          <div className="w-11 h-11 rounded-xl flex items-center justify-center shrink-0" style={{ background: T.bg, color: T.brand }}>
            <Icon size={22} strokeWidth={2} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-bold" style={{ color: T.text }}>
              {sport.label} <span className="font-semibold" style={{ ...MONO, color: T.textDim }}>{actSummary(a)}</span>
            </div>
            <div className="text-[11px] flex items-center gap-1 mt-0.5" style={{ color: T.textDim }}>
              <span>{fmtWhen(a.started_at)}</span> · <SrcIcon size={12} strokeWidth={2.5} /> <span>{src.label}</span>
            </div>
          </div>
        </div>
      );
    })}
  </>);
}
