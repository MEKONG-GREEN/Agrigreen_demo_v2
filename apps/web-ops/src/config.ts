// Cấu hình phía trình duyệt. Biến NEXT_PUBLIC_* được Next.js nhúng lúc build,
// nên image Docker phải build lại cho từng môi trường (xem README).
// Không đặt bí mật ở đây: client agrigreen-web-ops là public client + PKCE.

export const config = {
  /** Bật chế độ dữ liệu giả lập (không gọi backend, không cần Keycloak). */
  mock: process.env.NEXT_PUBLIC_API_MOCK === "1",
  /** Gốc Application API, vd https://api.agrigreen.vn */
  apiBaseUrl: process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080",
  /**
   * Gốc Admin API — host RIÊNG (admin.<domain>), MFA bắt buộc cho mọi route.
   * api-platform mở Admin API ở AG_ADMIN_HTTP_ADDR, mặc định :8082.
   */
  adminApiBaseUrl: process.env.NEXT_PUBLIC_ADMIN_API_BASE || "http://localhost:8082",
  oidc: {
    issuer: process.env.NEXT_PUBLIC_OIDC_ISSUER || "http://localhost:8081/realms/agrigreen",
    clientId: process.env.NEXT_PUBLIC_OIDC_CLIENT_ID || "agrigreen-web-ops",
  },
  /** URL style MapLibre (tile trong lãnh thổ, ADR-0007). Rỗng → nền trơn, không gọi tile ngoài. */
  mapStyleUrl: process.env.NEXT_PUBLIC_MAP_STYLE_URL ?? "",
  /**
   * Bề mặt menu (KN v7.7, kế hoạch P6 mục 3): "auto" = tài khoản có vai trò KN (GET /v1/me/contexts)
   * dùng menu theo cấu hình, còn lại menu cũ; "legacy" = luôn menu cũ (nav.ts).
   */
  surface: process.env.NEXT_PUBLIC_SURFACE === "legacy" ? ("legacy" as const) : ("auto" as const),
  /** 1 = thêm nhóm "Chức năng khác" (menu cũ theo vai realm) dưới menu KN — tính năng bị rút khỏi Trồng trọt. */
  knLegacyMenu: process.env.NEXT_PUBLIC_KN_LEGACY_MENU === "1",
} as const;
