// api.js — lớp client nói chuyện với backend Go.
// Đăng nhập qua Zalo hoặc Google; JWT app lưu ở localStorage["keo_jwt_token"].

// Client ID công khai của Google OAuth (nhúng ở FE là bình thường, không phải bí mật).
export const GOOGLE_CLIENT_ID =
  import.meta.env.VITE_GOOGLE_CLIENT_ID ||
  "550781100457-t0pg0qf5bgrpejse9r5sne03dcd83k3e.apps.googleusercontent.com";

export function currentToken() {
  return localStorage.getItem("keo_jwt_token");
}

export function logout() {
  localStorage.removeItem("keo_jwt_token");
}

export class APIError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

async function req(method, path, body) {
  const headers = { "Content-Type": "application/json" };
  const token = currentToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
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

// googleLogin đổi Google ID token (credential) lấy JWT app của hệ thống.
export async function googleLogin(credential) {
  const resp = await fetch("/v1/auth/google", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ credential }),
  });
  const text = await resp.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* not json */ }
  if (!resp.ok) throw new APIError(resp.status, data?.error || `HTTP ${resp.status}`);
  return data.access_token;
}

// ===== Ví =====
export const getWallet = () => req("GET", "/v1/wallet");
export const getTransactions = () => req("GET", "/v1/wallet/transactions");

export async function buyPack(packPoints) {
  const order = await req("POST", "/v1/wallet/purchase", { pack_points: packPoints });
  return order; // Trả về order (có order_url)
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

// ===== Điểm thưởng (check-in + km Strava) =====
export const getRewards = () => req("GET", "/v1/rewards");
export const checkIn = () => req("POST", "/v1/checkins");

// ===== Đổi thưởng =====
export const getShop = () => req("GET", "/v1/shop");
export const redeem = (sku, fulfillment) => req("POST", "/v1/redemptions", { sku, fulfillment });
export const getRedemptions = () => req("GET", "/v1/redemptions");
export const getCharitiesStats = () => req("GET", "/v1/charities/stats");

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

// ===== Admin APIs =====
export const adminListUsers = () => req("GET", "/v1/admin/users");
export const adminAdjustUserPoints = (id, delta, reason) => req("POST", `/v1/admin/users/${id}/adjust`, { delta, reason });
export const adminListRedemptions = () => req("GET", "/v1/admin/redemptions");
export const adminUpdateRedemptionStatus = (id, status, fulfillment) => req("POST", `/v1/admin/redemptions/${id}/status`, { status, fulfillment });
export const adminListShopItems = () => req("GET", "/v1/admin/shop-items");
export const adminCreateShopItem = (item) => req("POST", "/v1/admin/shop-items", item);
export const adminUpdateShopItem = (id, item) => req("PUT", `/v1/admin/shop-items/${id}`, item);
export const adminDeleteShopItem = (id) => req("DELETE", `/v1/admin/shop-items/${id}`);
