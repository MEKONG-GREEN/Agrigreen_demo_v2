// Cấu hình phía trình duyệt. Biến NEXT_PUBLIC_* được Next.js nhúng lúc build,
// nên image Docker phải build lại cho từng môi trường (xem README).
// Không đặt bí mật ở đây: client agrigreen-web-internal là public client + PKCE.
// Web nội bộ CHỈ gọi api-internal (vùng mkg) — không gọi Application/Admin API của nền tảng.
// Bản build production: next.config.ts chặn thiếu biến / URL không https / giả lập ngoài
// NEXT_PUBLIC_DEPLOY_ENV=demo|dev (src/deployEnv.ts). Mặc định localhost chỉ dùng khi dev.
import { allowsDevDefaults, isMockEnabled, type PublicEnv } from "./deployEnv";

/** Next.js chỉ nhúng tham chiếu process.env.NEXT_PUBLIC_* viết tường minh — giữ nguyên dạng này. */
const env: PublicEnv = {
  NODE_ENV: process.env.NODE_ENV,
  NEXT_PUBLIC_DEPLOY_ENV: process.env.NEXT_PUBLIC_DEPLOY_ENV,
  NEXT_PUBLIC_API_MOCK: process.env.NEXT_PUBLIC_API_MOCK,
};

function orDevDefault(value: string | undefined, devDefault: string): string {
  return value || (allowsDevDefaults(env) ? devDefault : "");
}

export const config = {
  /** Bật chế độ dữ liệu giả lập (không gọi backend, không cần Keycloak) — tắt cứng ở bản production thật. */
  mock: isMockEnabled(env),
  /** Bề mặt ứng dụng của api-internal (AG_HTTP_ADDR, mặc định :8081). */
  apiBaseUrl: orDevDefault(process.env.NEXT_PUBLIC_API_BASE_URL, "http://localhost:8081"),
  /** Bề mặt quản trị của api-internal (AG_ADMIN_HTTP_ADDR, mặc định :8083) — MFA bắt buộc cho mọi route. */
  adminApiBaseUrl: orDevDefault(process.env.NEXT_PUBLIC_ADMIN_API_BASE, "http://localhost:8083"),
  oidc: {
    issuer: orDevDefault(process.env.NEXT_PUBLIC_OIDC_ISSUER, "http://localhost:8180/realms/agrigreen"),
    clientId: process.env.NEXT_PUBLIC_OIDC_CLIENT_ID || "agrigreen-web-internal",
  },
} as const;
