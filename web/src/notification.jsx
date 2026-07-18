// notification.jsx — Smart Toast notification với type, icon và slide-in animation.
// Usage: <NotificationToast msg="..." type="success|error|info|warning" onDone={() => ...} duration={2600} />
import { useEffect, useState } from "react";
import { T } from "./theme.js";
import { CheckCircle, XCircle, AlertCircle, Info, X } from "lucide-react";

const TYPES = {
  success: {
    icon: CheckCircle,
    color: "#00E676",
    bg: "rgba(0,230,118,0.08)",
    border: "rgba(0,230,118,0.3)",
  },
  error: {
    icon: XCircle,
    color: "#FF3B30",
    bg: "rgba(255,59,48,0.08)",
    border: "rgba(255,59,48,0.3)",
  },
  warning: {
    icon: AlertCircle,
    color: "#FF9F0A",
    bg: "rgba(255,159,10,0.08)",
    border: "rgba(255,159,10,0.3)",
  },
  info: {
    icon: Info,
    color: "#CCFF00",
    bg: "rgba(204,255,0,0.07)",
    border: "rgba(204,255,0,0.25)",
  },
};

export function NotificationToast({ msg, type = "info", duration = 2600, onDone }) {
  const [leaving, setLeaving] = useState(false);
  const t = TYPES[type] || TYPES.info;
  const Icon = t.icon;

  useEffect(() => {
    const leaveTimer = setTimeout(() => setLeaving(true), duration - 250);
    const doneTimer = setTimeout(() => onDone?.(), duration);
    return () => { clearTimeout(leaveTimer); clearTimeout(doneTimer); };
  }, [duration, onDone]);

  return (
    <div
      role={type === "error" ? "alert" : "status"}
      aria-live={type === "error" ? "assertive" : "polite"}
      className={leaving ? "slide-up-out" : "slide-down"}
      style={{
        position: "absolute",
        top: "calc(96px + env(safe-area-inset-top))",
        left: 24,
        right: 24,
        zIndex: 60,
        borderRadius: 18,
        padding: "14px 16px",
        background: "rgba(18, 22, 30, 0.92)",
        border: `1px solid ${t.border}`,
        backdropFilter: "blur(16px)",
        WebkitBackdropFilter: "blur(16px)",
        boxShadow: `0 12px 32px rgba(0,0,0,0.5), 0 0 0 1px ${t.border}`,
        overflow: "hidden",
      }}
    >
      {/* Content row */}
      <div className="flex items-start gap-3">
        <Icon size={18} strokeWidth={2.5} style={{ color: t.color, marginTop: 1, flexShrink: 0 }} />
        <div className="flex-1 text-[13px] font-semibold leading-snug" style={{ color: T.text }}>
          {msg}
        </div>
        <button
          onClick={() => { setLeaving(true); setTimeout(() => onDone?.(), 220); }}
          aria-label="Đóng thông báo"
          className="shrink-0 -m-2 p-2 rounded-full transition-opacity hover:opacity-70 flex items-center justify-center"
          style={{ color: T.textDim, minWidth: 40, minHeight: 40 }}
        >
          <X size={16} />
        </button>
      </div>

      {/* Progress bar */}
      <div
        className="absolute bottom-0 left-0 h-[2px] rounded-full toast-progress"
        style={{
          background: t.color,
          animationDuration: `${duration}ms`,
          animationFillMode: "both",
        }}
      />
    </div>
  );
}

// Helper để detect loại toast từ message string
export function detectToastType(msg) {
  const m = msg?.toLowerCase() || "";
  if (m.includes("lỗi") || m.includes("thất bại") || m.includes("không thể") || m.includes("không đủ")) return "error";
  if (m.includes("cảnh báo") || m.includes("chú ý")) return "warning";
  if (m.includes("✓") || m.includes("🎉") || m.includes("✅") || m.includes("thành công") || m.includes("đã chốt") || m.includes("đã đổi") || m.includes("đã sao chép") || m.includes("đồng bộ")) return "success";
  return "info";
}
