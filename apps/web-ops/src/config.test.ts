import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { DEFAULT_OIDC_CLIENT_ID, DEV_OIDC_ISSUER, resolveOidcConfig } from "./config";

// #86 KNSPLIT-3-CLIENT-ISSUER: issuer mặc định khi dev phải trùng issuer Keycloak mà Compose công bố
// (cổng 8180), không phải 8081 (API nội bộ MKG). Bản production thật vẫn chỉ lấy từ biến môi trường.
const repoFile = (rel: string) => readFileSync(resolve(process.cwd(), "../..", rel), "utf8");

describe("resolveOidcConfig", () => {
  it("dev (next dev / vitest) không đặt biến → issuer Keycloak Compose 8180, client agrigreen-web-ops", () => {
    expect(resolveOidcConfig({ NODE_ENV: "development" }, {})).toEqual({
      issuer: "http://localhost:8180/realms/agrigreen",
      clientId: "agrigreen-web-ops",
    });
    expect(DEV_OIDC_ISSUER).not.toContain(":8081");
  });

  it("bản build NEXT_PUBLIC_DEPLOY_ENV=dev cũng dùng mặc định 8180", () => {
    expect(resolveOidcConfig({ NODE_ENV: "production", NEXT_PUBLIC_DEPLOY_ENV: "dev" }, {}).issuer).toBe(DEV_OIDC_ISSUER);
  });

  it("bản production/staging không có mặc định localhost", () => {
    expect(resolveOidcConfig({ NODE_ENV: "production" }, {}).issuer).toBe("");
    expect(resolveOidcConfig({ NODE_ENV: "production", NEXT_PUBLIC_DEPLOY_ENV: "staging" }, {}).issuer).toBe("");
    expect(resolveOidcConfig({ NODE_ENV: "production", NEXT_PUBLIC_DEPLOY_ENV: "demo" }, {}).issuer).toBe("");
  });

  it("biến môi trường luôn thắng mặc định (production lấy issuer/client từ env)", () => {
    const got = resolveOidcConfig(
      { NODE_ENV: "production" },
      { issuer: "https://auth.agrigreen.vn/realms/agrigreen", clientId: "kn-web" },
    );
    expect(got).toEqual({ issuer: "https://auth.agrigreen.vn/realms/agrigreen", clientId: "kn-web" });
    expect(resolveOidcConfig({ NODE_ENV: "development" }, { issuer: "", clientId: "" })).toEqual({
      issuer: DEV_OIDC_ISSUER,
      clientId: DEFAULT_OIDC_CLIENT_ID,
    });
  });
});

describe("issuer dev khớp Keycloak Compose", () => {
  const compose = repoFile("deploy/compose/docker-compose.yml");

  it("Compose công bố Keycloak ở 8180 với KC_HOSTNAME và AG_OIDC_ISSUER = DEV_OIDC_ISSUER", () => {
    expect(compose).toMatch(/KC_HOSTNAME:\s*http:\/\/localhost:8180\s*$/m);
    expect(compose).toMatch(/127\.0\.0\.1:8180:8080/);
    expect(compose).toContain(`AG_OIDC_ISSUER: ${DEV_OIDC_ISSUER}`);
    expect(new URL(DEV_OIDC_ISSUER).origin).toBe("http://localhost:8180");
  });

  it("mọi NEXT_PUBLIC_OIDC_ISSUER trong Compose trùng DEV_OIDC_ISSUER", () => {
    const issuers = [...compose.matchAll(/NEXT_PUBLIC_OIDC_ISSUER:\s*(\S+)/g)].map((m) => m[1]);
    expect(issuers.length).toBeGreaterThan(0);
    for (const issuer of issuers) expect(issuer).toBe(DEV_OIDC_ISSUER);
  });

  it(".env.example và README ghi cùng issuer dev", () => {
    const example = readFileSync(resolve(process.cwd(), ".env.example"), "utf8");
    expect(example).toMatch(new RegExp(`^NEXT_PUBLIC_OIDC_ISSUER=${DEV_OIDC_ISSUER.replace(/[.]/g, "\\.")}$`, "m"));
    expect(example).toMatch(/^NEXT_PUBLIC_OIDC_CLIENT_ID=agrigreen-web-ops$/m);
    const readme = readFileSync(resolve(process.cwd(), "README.md"), "utf8");
    expect(readme).toContain(`\`NEXT_PUBLIC_OIDC_ISSUER\` | \`${DEV_OIDC_ISSUER}\``);
    expect(example + readme).not.toMatch(/localhost:8081\/realms/);
  });
});
