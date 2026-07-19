// Worker chỉ có 1 việc: gọi endpoint drain hàng đợi Strava của Kèo theo lịch cron.
// Endpoint /api/cron/strava idempotent (ProcessOnce claim-then-process) nên gọi
// thừa vô hại. Real-time vẫn do webhook inline xử lý (~2.5s); worker này là LƯỚI
// AN TOÀN vét event lỡ cửa sổ inline (cold-start), đúng lịch — khác GitHub Actions
// bị throttle ~mỗi giờ.

async function drain(env) {
  const url = env.CRON_TARGET || "https://app.xox.vn/api/cron/strava";
  const res = await fetch(url, { method: "GET" });
  const body = await res.text();
  const line = `[strava-cron] HTTP ${res.status}: ${body}`;
  console.log(line);
  // Ném lỗi khi non-2xx để CF ghi nhận lần chạy fail (xem ở tab Observability);
  // event đã tự requeue/failed bên trong endpoint nên không mất dữ liệu.
  if (!res.ok) throw new Error(line);
  return line;
}

export default {
  // Cron Trigger gọi vào đây theo lịch trong wrangler.toml.
  async scheduled(event, env, ctx) {
    ctx.waitUntil(drain(env));
  },
  // Cho phép gọi tay để test: mở URL worker (…workers.dev) trên trình duyệt.
  async fetch(request, env) {
    try {
      return new Response(await drain(env), { status: 200 });
    } catch (e) {
      return new Response(String(e), { status: 502 });
    }
  },
};
