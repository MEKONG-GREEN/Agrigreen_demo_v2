// Cấu hình phía trình duyệt. Biến NEXT_PUBLIC_* được Next.js nhúng lúc build,
// nên image Docker phải build lại cho từng môi trường (xem README).
// Không đặt bí mật ở đây: client agrigreen-web-ops là public client + PKCE.
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

/**
 * Issuer Keycloak mặc định khi chạy cục bộ: cổng 8180 của Compose (deploy/compose/docker-compose.yml,
 * KC_HOSTNAME + AG_OIDC_ISSUER). Cổng 8081 là API nội bộ MKG, không phải Keycloak (#86).
 */
export const DEV_OIDC_ISSUER = "http://localhost:8180/realms/agrigreen";
/** Client công khai mặc định (PKCE S256). Đổi sang client khác (vd kn-web) chỉ qua biến môi trường. */
export const DEFAULT_OIDC_CLIENT_ID = "agrigreen-web-ops";

/**
 * Cấu hình OIDC từ biến môi trường đã nhúng. Bản build production thật không có mặc định localhost:
 * issuer rỗng (next.config.ts đã chặn lúc build nếu thiếu/không https).
 */
export function resolveOidcConfig(
  publicEnv: PublicEnv,
  values: { issuer?: string; clientId?: string },
): { issuer: string; clientId: string } {
  return {
    issuer: values.issuer || (allowsDevDefaults(publicEnv) ? DEV_OIDC_ISSUER : ""),
    clientId: values.clientId || DEFAULT_OIDC_CLIENT_ID,
  };
}

export const config = {
  /** Bật chế độ dữ liệu giả lập (không gọi backend, không cần Keycloak) — tắt cứng ở bản production thật. */
  mock: isMockEnabled(env),
  /** Gốc Application API, vd https://api.agrigreen.vn */
  apiBaseUrl: orDevDefault(process.env.NEXT_PUBLIC_API_BASE_URL, "http://localhost:8080"),
  /**
   * Gốc Admin API — host RIÊNG (admin.<domain>), MFA bắt buộc cho mọi route.
   * api-platform mở Admin API ở AG_ADMIN_HTTP_ADDR, mặc định :8082.
   */
  adminApiBaseUrl: orDevDefault(process.env.NEXT_PUBLIC_ADMIN_API_BASE, "http://localhost:8082"),
  oidc: resolveOidcConfig(env, {
    issuer: process.env.NEXT_PUBLIC_OIDC_ISSUER,
    clientId: process.env.NEXT_PUBLIC_OIDC_CLIENT_ID,
  }),
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
