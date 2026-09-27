import type { NextConfig } from "next";

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

export default nextConfig;
