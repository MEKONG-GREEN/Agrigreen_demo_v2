import type { NextConfig } from "next";
import { PHASE_PRODUCTION_BUILD } from "next/constants";
import { assertBuildEnv, type EnvRules } from "./src/deployEnv";

// Header bảo mật cơ bản. CSP chặt hơn (nonce) để cổng G2 khi đã chốt domain API/Keycloak.
const securityHeaders = [
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "X-Frame-Options", value: "SAMEORIGIN" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(self)" },
];

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  reactStrictMode: true,
  // Không dùng tối ưu ảnh của Next → không cần sharp. Loại sharp/libvips (LGPL-3.0, ngoài
  // deploy/policies/allowed-licenses.txt) khỏi bản standalone chạy trong image.
  images: { unoptimized: true },
  outputFileTracingExcludes: {
    "*": ["node_modules/sharp/**", "node_modules/@img/**"],
  },
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

// M16/M17: `next build` dừng nếu bản thật thiếu NEXT_PUBLIC_* / dùng http / bật giả lập
// (giả lập chỉ khi NEXT_PUBLIC_DEPLOY_ENV=demo|dev — build:mock tự đặt demo). Xem src/deployEnv.ts.
const buildEnvRules: EnvRules = {
  requiredUrls: ["NEXT_PUBLIC_API_BASE_URL", "NEXT_PUBLIC_ADMIN_API_BASE", "NEXT_PUBLIC_OIDC_ISSUER"],
};

export default function config(phase: string): NextConfig {
  if (phase === PHASE_PRODUCTION_BUILD) assertBuildEnv(process.env, buildEnvRules);
  return nextConfig;
}
