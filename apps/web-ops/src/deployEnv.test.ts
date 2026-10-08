import { afterEach, describe, expect, it, vi } from "vitest";
import { assertBuildEnv, buildEnvErrors, isMockEnabled, type EnvRules } from "./deployEnv";

// M16/M17: bản build production không có cờ demo phải TỪ CHỐI giả lập (bỏ qua đăng nhập) và
// mặc định localhost/http. Chạy trong CI qua `npm test`.
const rules: EnvRules = {
  requiredUrls: ["NEXT_PUBLIC_API_BASE_URL", "NEXT_PUBLIC_ADMIN_API_BASE", "NEXT_PUBLIC_OIDC_ISSUER"],
  optionalUrls: ["NEXT_PUBLIC_MAP_STYLE_URL"],
};
const prodUrls = {
  NEXT_PUBLIC_API_BASE_URL: "https://api.agrigreen.vn",
  NEXT_PUBLIC_ADMIN_API_BASE: "https://admin.agrigreen.vn",
  NEXT_PUBLIC_OIDC_ISSUER: "https://auth.agrigreen.vn/realms/agrigreen",
};

describe("buildEnvErrors (chặn lúc next build)", () => {
  it("bản production thiếu biến → lỗi, không rơi về localhost", () => {
    const errs = buildEnvErrors({}, rules);
    expect(errs).toHaveLength(3);
    expect(errs.join("\n")).toContain("NEXT_PUBLIC_OIDC_ISSUER bắt buộc");
  });

  it("bản production dùng http → lỗi", () => {
    const errs = buildEnvErrors({ ...prodUrls, NEXT_PUBLIC_OIDC_ISSUER: "http://auth.agrigreen.vn/realms/agrigreen" }, rules);
    expect(errs).toEqual([expect.stringContaining("NEXT_PUBLIC_OIDC_ISSUER phải là URL https://")]);
    expect(buildEnvErrors({ ...prodUrls, NEXT_PUBLIC_MAP_STYLE_URL: "http://tiles.local/style.json" }, rules)).toHaveLength(1);
  });

  it("bản production đủ URL https → hợp lệ", () => {
    expect(buildEnvErrors(prodUrls, rules)).toEqual([]);
    expect(buildEnvErrors({ ...prodUrls, NEXT_PUBLIC_DEPLOY_ENV: "staging" }, rules)).toEqual([]);
  });

  it("giả lập không có cờ demo → từ chối build (kể cả khi đủ URL)", () => {
    for (const deploy of [undefined, "", "production", "staging"]) {
      const errs = buildEnvErrors({ ...prodUrls, NEXT_PUBLIC_API_MOCK: "1", NEXT_PUBLIC_DEPLOY_ENV: deploy }, rules);
      expect(errs.join("\n")).toContain("NEXT_PUBLIC_API_MOCK=1");
    }
    expect(() => assertBuildEnv({ NEXT_PUBLIC_API_MOCK: "1" }, rules)).toThrow(/Cấu hình build không hợp lệ/);
  });

  it("demo giả lập (build:mock, Render) và dev cục bộ → hợp lệ", () => {
    expect(buildEnvErrors({ NEXT_PUBLIC_API_MOCK: "1", NEXT_PUBLIC_DEPLOY_ENV: "demo" }, rules)).toEqual([]);
    expect(buildEnvErrors({ NEXT_PUBLIC_DEPLOY_ENV: "dev", NEXT_PUBLIC_API_BASE_URL: "http://localhost:8080" }, rules)).toEqual([]);
  });

  it("demo KHÔNG giả lập vẫn phải đủ URL https; giá trị DEPLOY_ENV lạ → lỗi", () => {
    expect(buildEnvErrors({ NEXT_PUBLIC_DEPLOY_ENV: "demo" }, rules)).toHaveLength(3);
    expect(buildEnvErrors({ ...prodUrls, NEXT_PUBLIC_DEPLOY_ENV: "prod" }, rules)).toEqual([expect.stringContaining("không hợp lệ")]);
  });
});

describe("isMockEnabled (chốt lại lúc chạy)", () => {
  it("bản production không có cờ demo → giả lập tắt dù NEXT_PUBLIC_API_MOCK=1", () => {
    expect(isMockEnabled({ NODE_ENV: "production", NEXT_PUBLIC_API_MOCK: "1" })).toBe(false);
    expect(isMockEnabled({ NODE_ENV: "production", NEXT_PUBLIC_API_MOCK: "1", NEXT_PUBLIC_DEPLOY_ENV: "staging" })).toBe(false);
    expect(isMockEnabled({ NODE_ENV: "production", NEXT_PUBLIC_API_MOCK: "1", NEXT_PUBLIC_DEPLOY_ENV: "demo" })).toBe(true);
    expect(isMockEnabled({ NODE_ENV: "development", NEXT_PUBLIC_API_MOCK: "1" })).toBe(true);
    expect(isMockEnabled({ NODE_ENV: "production", NEXT_PUBLIC_DEPLOY_ENV: "demo" })).toBe(false);
  });
});

describe("config (bản production)", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("không có cờ demo → mock=false và không có URL localhost", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("NEXT_PUBLIC_API_MOCK", "1");
    vi.stubEnv("NEXT_PUBLIC_DEPLOY_ENV", "");
    vi.stubEnv("NEXT_PUBLIC_API_BASE_URL", "");
    vi.stubEnv("NEXT_PUBLIC_ADMIN_API_BASE", "");
    vi.stubEnv("NEXT_PUBLIC_OIDC_ISSUER", "");
    vi.resetModules();
    const { config } = await import("./config");
    expect(config.mock).toBe(false);
    expect(JSON.stringify(config)).not.toContain("localhost");
  });
});
