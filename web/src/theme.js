// theme.js — design tokens + từ điển hiển thị dùng chung giữa các màn.

import { Footprints, Zap, Waves, Bike, Dumbbell, Trophy, Activity, HeartPulse, Smartphone } from "lucide-react";

export const T = {
  bg: "#090B0E", paper: "#161920", card: "#1B1F27",
  text: "#FFFFFF", textDim: "#8B949E",
  brand: "#CCFF00", brandDark: "#99BF00",
  green: "#00E676", red: "#FF3B30", strava: "#FC4C02",
  gray: "#8B949E", line: "#2D333B",
};

export const MONO = { fontFamily: "'IBM Plex Mono', monospace" };

export const SPORTS = {
  walk: { label: "Đi bộ", icon: Footprints }, run: { label: "Chạy bộ", icon: Zap },
  swim: { label: "Bơi lội", icon: Waves }, bike: { label: "Đạp xe", icon: Bike },
  gym: { label: "Gym", icon: Dumbbell },
};

export const SOURCES = {
  strava: { label: "Strava", icon: Activity },
  google_fit: { label: "Google Fit", icon: HeartPulse },
  apple_health: { label: "Apple Health", icon: Smartphone },
};

export const GOALS = {
  daily_steps: { label: "bước/ngày", sports: ["walk"] },
  daily_distance_km: { label: "km/ngày", sports: ["run", "bike"] },
  weekly_distance_km: { label: "km/tuần", sports: ["swim"] },
  weekly_sessions: { label: "buổi/tuần", sports: ["gym"] },
};

export const fmtP = (n) => Number(n).toLocaleString("vi-VN") + " điểm";
export const daysLeft = (endAt) => Math.max(0, Math.ceil((new Date(endAt) - Date.now()) / 86400000));

export const CHARITIES = {
  1001: { name: "Quỹ Phẫu Thuật Nụ Cười", desc: "Operation Smile - Phẫu thuật hàm ếch miễn phí cho trẻ em.", color: "#FF3366", logo: "👄" },
  1002: { name: "Quỹ Gieo Mầm Xanh", desc: "Trồng rừng phòng hộ và phủ xanh các khu bảo tồn thiên nhiên.", color: "#33CC66", logo: "🌳" }
};
