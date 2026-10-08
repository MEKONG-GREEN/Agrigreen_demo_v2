import { defineConfig, devices } from "@playwright/test";

// E2E trình duyệt (Playwright, Apache-2.0) chạy trên bản build production ở chế độ dữ liệu giả lập
// (NEXT_PUBLIC_API_MOCK=1): `next build` rồi `next start`, không cần backend/Keycloak.
// Đặt E2E_SKIP_BUILD=1 để dùng lại bản build có sẵn khi chạy lặp trên máy dev.
const PORT = 3100;
const CI = !!process.env.CI;

export default defineConfig({
  testDir: "tests/e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  workers: CI ? 2 : undefined,
  reporter: CI ? [["github"], ["html", { open: "never" }]] : [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: `http://localhost:${PORT}`,
    locale: "vi-VN",
    timezoneId: "Asia/Ho_Chi_Minh",
    acceptDownloads: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } }],
  webServer: {
    command: process.env.E2E_SKIP_BUILD ? "npm run start:e2e" : "npm run build:mock && npm run start:e2e",
    url: `http://localhost:${PORT}/login`,
    timeout: 600_000,
    reuseExistingServer: !CI,
    stdout: "ignore",
    stderr: "pipe",
    env: { NEXT_PUBLIC_API_MOCK: "1", NEXT_PUBLIC_DEPLOY_ENV: "demo", NEXT_TELEMETRY_DISABLED: "1" },
  },
});
