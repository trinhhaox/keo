// Modal tạo kèo mới — form chọn bộ môn, mục tiêu, cược, quỹ từ thiện.
import { useState, useEffect } from "react";
import { T, MONO, SPORTS, GOALS, SOURCES, CHARITIES, fmtP } from "./theme.js";
import { Heart, Ribbon } from "lucide-react";
import { Label, Chip } from "./ui-primitives.jsx";

export default function CreateSheet({ open, busy, onClose, onCreate, wallet, setTab }) {
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
  const [title, setTitle] = useState(""); // tên kèo tuỳ chọn; rỗng → tự sinh từ bộ môn+mục tiêu

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

  // Validation client: mục tiêu > 0, số người là số nguyên ≥ 0 (0 = không giới hạn).
  const goalNum = Number(goal);
  const maxNum = Number(maxParticipants);
  const invalidMsg =
    !(goalNum > 0) ? "Mục tiêu phải lớn hơn 0."
    : !(Number.isInteger(maxNum) && maxNum >= 0) ? "Số người tối đa phải là số nguyên ≥ 0 (0 = không giới hạn)."
    : "";

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

        <Label htmlFor="ck-title">Tên kèo (tuỳ chọn)</Label>
        <input id="ck-title" type="text" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={60}
          placeholder="Ví dụ: Chạy bộ mỗi sáng cùng team"
          className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none mb-4"
          style={{ background: T.card, color: T.text, border: `1px solid ${T.line}` }} />

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
            <Label htmlFor="ck-goal">Mục tiêu ({GOALS[goalType]?.label || "đơn vị"})</Label>
            <input id="ck-goal" type="number" inputMode="numeric" min="1" value={goal} onChange={(e) => setGoal(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
          <div>
            <Label htmlFor="ck-max">Tối đa (người)</Label>
            <input id="ck-max" type="number" inputMode="numeric" min="0" value={maxParticipants} onChange={(e) => setMaxParticipants(+e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3.5 mb-4">
          <div>
            <Label htmlFor="ck-start">Ngày bắt đầu</Label>
            <input id="ck-start" type="date" value={startAt} onChange={(e) => handleStartAtChange(e.target.value)}
              className="w-full px-3 py-3 rounded-xl text-sm font-bold outline-none"
              style={{ ...MONO, background: T.card, color: T.text, border: `1px solid ${T.line}` }} />
          </div>
          <div>
            <Label htmlFor="ck-end">Ngày kết thúc</Label>
            <input id="ck-end" type="date" value={endAt} min={startAt} onChange={(e) => handleEndAtChange(e.target.value)}
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
              <div className="text-xs font-bold inline-flex items-center gap-1.5" style={{ color: T.text }}><Ribbon size={13} strokeWidth={2.5} style={{ color: T.red }} /> Kèo Từ Thiện</div>
              <div className="text-[10px]" style={{ color: T.textDim }}>Quyên góp cược thua vào quỹ</div>
            </div>
          </div>
          <button
            onClick={() => setIsCharity(!isCharity)}
            aria-label="Bật/tắt kèo từ thiện"
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
                  <span className="mt-0.5 shrink-0"><v.Icon size={20} strokeWidth={2} style={{ color: v.color }} /></span>
                  <div>
                    <div className="text-xs font-bold" style={{ color: charityId === Number(k) ? v.color : T.text }}>{v.name}</div>
                    <div className="text-[10px]" style={{ color: T.textDim }}>{v.desc}</div>
                  </div>
                </button>
              ))}
            </div>
            <div className="text-[9px] leading-relaxed font-bold mt-2 px-1 text-center" style={{ color: T.brand }}>
              Thắng hoàn cược, thua quyên góp 100% quỹ. Miễn phí nền tảng!
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
            aria-label="Số điểm cược"
            className="w-full h-1.5 bg-zinc-700 rounded-lg appearance-none cursor-pointer accent-[#CCFF00] focus:outline-none"
          />
          <div className="flex justify-between text-[10px] font-bold mt-1.5" style={{ color: T.textDim }}>
            <span>Min: 10</span>
            <span>Ví khả dụng: {Number(wallet?.available || 0).toLocaleString("vi-VN")} pts</span>
          </div>
        </div>

        {invalidMsg && (
          <div className="text-xs font-semibold mb-3 px-3 py-2 rounded-lg" role="alert" aria-live="polite"
            style={{ background: "rgba(255,59,48,0.1)", color: T.red, border: `1px solid ${T.red}33` }}>
            {invalidMsg}
          </div>
        )}
        {enough ? (
          <button disabled={busy || !!invalidMsg}
            onClick={() => onCreate({
              title: title.trim() || `${SPORTS[sport].label} ${goalNum.toLocaleString("vi-VN")} ${GOALS[goalType]?.label || ""}`,
              sport, goal_type: goalType, goal_value: goalNum, source,
              stake_points: stake, duration_days: days, max_participants: maxNum,
              start_at: startAt,
              is_charity: isCharity,
              charity_id: isCharity ? charityId : 0,
            })}
            className="w-full py-3.5 rounded-2xl font-bold text-[15px] active:scale-[.98] transition-transform"
            style={{ background: T.brand, color: T.bg, opacity: busy || invalidMsg ? 0.5 : 1 }}>
            {busy ? "Đang tạo..." : `Tạo kèo · cược ${fmtP(stake)}`}
          </button>
        ) : (
          <button onClick={() => { onClose(); setTab("wallet"); }}
            className="w-full font-bold py-3.5 rounded-2xl text-[15px] text-center active:scale-[.98] transition-transform"
            style={{ background: T.red, color: T.text, boxShadow: "0 0 15px rgba(255, 59, 48, 0.4)" }}>
            Không đủ điểm — Nạp thêm ở tab Ví
          </button>
        )}
      </div>
    </div>
  );
}
