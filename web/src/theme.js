// theme.js — design tokens + từ điển hiển thị dùng chung giữa các màn.

export const T = {
  ink: "#15171B", paper: "#F1F3F1", card: "#FFFFFF",
  brand: "#FFD338", brandDark: "#E8B800",
  green: "#149A52", red: "#E5484D", strava: "#FC4C02",
  gray: "#7A7F87", line: "#E4E6E4",
};

export const MONO = { fontFamily: "'IBM Plex Mono', monospace" };

export const SPORTS = {
  walk: { label: "Đi bộ", icon: "🚶" }, run: { label: "Chạy bộ", icon: "🏃" },
  swim: { label: "Bơi lội", icon: "🏊" }, bike: { label: "Đạp xe", icon: "🚴" },
  gym: { label: "Gym", icon: "🏋️" },
};

export const SOURCES = {
  strava: { label: "Strava", icon: "🟠" },
  google_fit: { label: "Google Fit", icon: "🟢" },
  apple_health: { label: "Apple Health", icon: "🍎" },
};

export const GOALS = {
  daily_steps: { label: "bước/ngày", sports: ["walk"] },
  weekly_distance_km: { label: "km/tuần", sports: ["run", "bike", "swim"] },
  weekly_sessions: { label: "buổi/tuần", sports: ["gym", "swim"] },
};

export const fmtP = (n) => Number(n).toLocaleString("vi-VN") + " điểm";
export const daysLeft = (endAt) => Math.max(0, Math.ceil((new Date(endAt) - Date.now()) / 86400000));
