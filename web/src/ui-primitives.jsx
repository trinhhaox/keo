// Primitive UI dùng chung giữa các màn/form.
import { T } from "./theme.js";

// Label: render <label htmlFor> khi có htmlFor (gắn ngữ nghĩa + click-to-focus,
// a11y), ngược lại là <div> tiêu đề trường thường.
export const Label = ({ children, htmlFor }) =>
  htmlFor ? (
    <label htmlFor={htmlFor} className="block text-xs font-bold uppercase tracking-widest mb-2" style={{ color: T.textDim }}>{children}</label>
  ) : (
    <div className="text-xs font-bold uppercase tracking-widest mb-2" style={{ color: T.textDim }}>{children}</div>
  );

export const Chip = ({ active, onClick, children }) => (
  <button onClick={onClick} className="px-3.5 py-2 rounded-xl text-[13px] font-bold transition-all border shrink-0"
    style={{
      background: active ? "rgba(204,255,0,0.1)" : T.bg,
      borderColor: active ? T.brand : T.line,
      color: active ? T.brand : T.textDim
    }}>
    {children}
  </button>
);
