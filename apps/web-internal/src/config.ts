// Cấu hình phía trình duyệt. Biến NEXT_PUBLIC_* được Next.js nhúng lúc build,
// nên image Docker phải build lại cho từng môi trường (xem README).
// Không đặt bí mật ở đây: client agrigreen-web-internal là public client + PKCE.
// Web nội bộ CHỈ gọi api-internal (vùng mkg) — không gọi Application/Admin API của nền tảng.

export const config = {
  /** Bật chế độ dữ liệu giả lập (không gọi backend, không cần Keycloak). */
  mock: process.env.NEXT_PUBLIC_API_MOCK === "1",
  /** Bề mặt ứng dụng của api-internal (AG_HTTP_ADDR, mặc định :8081). */
  apiBaseUrl: process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8081",
  /** Bề mặt quản trị của api-internal (AG_ADMIN_HTTP_ADDR, mặc định :8083) — MFA bắt buộc cho mọi route. */
  adminApiBaseUrl: process.env.NEXT_PUBLIC_ADMIN_API_BASE || "http://localhost:8083",
  oidc: {
    issuer: process.env.NEXT_PUBLIC_OIDC_ISSUER || "http://localhost:8180/realms/agrigreen",
    clientId: process.env.NEXT_PUBLIC_OIDC_CLIENT_ID || "agrigreen-web-internal",
  },
} as const;
