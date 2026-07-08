// api.js — lớp client nói chuyện với backend Go.
// Auth dev: user_id lưu localStorage, gửi qua header X-User-ID.
// Khi backend chuyển sang JWT thật, chỉ file này phải đổi.

const KEY = "keo_user_id";

export function currentUserID() {
  return localStorage.getItem(KEY);
}

export function logout() {
  localStorage.removeItem(KEY);
}

export class APIError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

async function req(method, path, body) {
  const headers = { "Content-Type": "application/json" };
  const uid = currentUserID();
  if (uid) headers["X-User-ID"] = uid;
  const resp = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await resp.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    /* body không phải JSON */
  }
  if (!resp.ok) {
    throw new APIError(resp.status, data?.error || `HTTP ${resp.status}`);
  }
  return data;
}

// ===== Auth (dev) =====
export async function devLogin(displayName) {
  const out = await req("POST", "/v1/auth/dev-login", { display_name: displayName });
  localStorage.setItem(KEY, String(out.user_id));
  return out;
}

// ===== Ví =====
export const getWallet = () => req("GET", "/v1/wallet");
export const getTransactions = () => req("GET", "/v1/wallet/transactions");

export async function buyPack(packPoints) {
  const order = await req("POST", "/v1/wallet/purchase", { pack_points: packPoints });
  // DEV: mô phỏng ZaloPay bắn callback. Bản thật: mở order_url trong
  // app ZaloPay, callback do ZaloPay bắn về server.
  const cb = await req("POST", "/v1/dev/confirm-payment", {
    app_trans_id: order.app_trans_id,
  });
  if (cb.return_code !== 1) throw new APIError(400, cb.return_message || "callback fail");
  return order;
}

// ===== Kèo =====
export const listChallenges = () => req("GET", "/v1/challenges");
export const myChallenges = () => req("GET", "/v1/me/challenges");
export const joinChallenge = (id) => req("POST", `/v1/challenges/${id}/join`);
export const createChallenge = (c) => req("POST", "/v1/challenges", c);
export const getLeaderboard = (id) => req("GET", `/v1/challenges/${id}/leaderboard`);

// ===== Thống kê cá nhân =====
export const getMyActivities = () => req("GET", "/v1/me/activities");
export const getMyStats = () => req("GET", "/v1/me/stats");

// ===== Đổi thưởng =====
export const getShop = () => req("GET", "/v1/shop");
export const redeem = (sku) => req("POST", "/v1/redemptions", { sku });

// ===== Health/Fit sync (demo: gửi bucket hôm nay đạt chỉ tiêu) =====
export function syncHealthDemo(source, sport, goalType, goalValue) {
  const today = new Date().toLocaleDateString("sv-SE", { timeZone: "Asia/Ho_Chi_Minh" });
  const bucket = { date: today, sport };
  if (goalType === "daily_steps") bucket.steps = Math.round(goalValue * 1.1);
  else if (goalType === "weekly_distance_km") bucket.distance_km = Math.round(goalValue * 0.3 * 10) / 10;
  else bucket.sessions = 1;
  return req("POST", "/v1/health-sync", {
    source,
    device_attestation: "dev-token",
    buckets: [bucket],
  });
}
