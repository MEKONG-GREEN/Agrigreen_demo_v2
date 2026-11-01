import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { OIDC_SCOPES, oidcSettings } from "./oidc";

// #86: scope xin khi authorize phải nằm trong client scope của client trong realm JSON
// (Keycloak TokenManager.isValidScope bỏ "openid" rồi đối chiếu default/optional client scope;
// lệch là invalid_scope trước cả PKCE). Kiểm cả agrigreen-web-ops (mặc định) và kn-web.
interface RealmClient {
  clientId: string;
  publicClient?: boolean;
  standardFlowEnabled?: boolean;
  implicitFlowEnabled?: boolean;
  directAccessGrantsEnabled?: boolean;
  redirectUris?: string[];
  webOrigins?: string[];
  defaultClientScopes?: string[];
  optionalClientScopes?: string[];
  attributes?: Record<string, string>;
}
interface Realm {
  clientScopes: { name: string }[];
  clients: RealmClient[];
}

const realm = JSON.parse(
  readFileSync(resolve(process.cwd(), "../../deploy/compose/keycloak/agrigreen-realm.json"), "utf8"),
) as Realm;
const client = (id: string) => {
  const c = realm.clients.find((x) => x.clientId === id);
  if (!c) throw new Error(`realm thiếu client ${id}`);
  return c;
};
// Keycloak so redirect URI với mẫu có "*" ở cuối theo tiền tố.
const matches = (patterns: string[], uri: string) =>
  patterns.some((p) => (p.endsWith("*") ? uri.startsWith(p.slice(0, -1)) : uri === p));

describe("scope OIDC của web-ops", () => {
  it("xin openid + profile, chuỗi scope ghép bằng khoảng trắng", () => {
    expect(OIDC_SCOPES[0]).toBe("openid");
    expect(oidcSettings("http://localhost:3000").scope).toBe("openid profile");
  });

  it.each(["agrigreen-web-ops", "kn-web"])("mọi scope (trừ openid) là client scope của %s trong realm", (id) => {
    const c = client(id);
    const realmScopes = new Set(realm.clientScopes.map((s) => s.name));
    const allowed = new Set([...(c.defaultClientScopes ?? []), ...(c.optionalClientScopes ?? [])]);
    for (const scope of OIDC_SCOPES.filter((s) => s !== "openid")) {
      expect(realmScopes.has(scope), `realm chưa khai báo client scope ${scope}`).toBe(true);
      expect(allowed.has(scope), `client ${id} không được cấp scope ${scope}`).toBe(true);
    }
    // Claim tenant/vai đến từ scope mặc định, không phải từ scope xin thêm.
    expect(c.defaultClientScopes).toEqual(expect.arrayContaining(["basic", "roles", "agrigreen-claims"]));
  });
});

describe("tham số client OIDC khớp realm", () => {
  it.each(["http://localhost:3000", "http://127.0.0.1:3000"])("redirect/logout URI của %s được realm chấp nhận", (origin) => {
    const s = oidcSettings(origin);
    for (const id of ["agrigreen-web-ops", "kn-web"]) {
      const c = client(id);
      expect(matches(c.redirectUris ?? [], s.redirect_uri)).toBe(true);
      expect(matches(c.redirectUris ?? [], s.silent_redirect_uri!)).toBe(true);
      const logout = (c.attributes?.["post.logout.redirect.uris"] ?? "").split("##");
      expect(matches(logout, s.post_logout_redirect_uri!)).toBe(true);
      expect(c.webOrigins).toContain(origin);
    }
  });

  it("client mặc định là public + PKCE S256, chỉ Authorization Code", () => {
    const s = oidcSettings("http://localhost:3000");
    expect(s.client_id).toBe("agrigreen-web-ops");
    expect(s.response_type).toBe("code");
    expect(s.authority).toBe("http://localhost:8180/realms/agrigreen");
    for (const id of ["agrigreen-web-ops", "kn-web"]) {
      const c = client(id);
      expect(c.publicClient).toBe(true);
      expect(c.standardFlowEnabled).toBe(true);
      expect(c.implicitFlowEnabled ?? false).toBe(false);
      expect(c.directAccessGrantsEnabled ?? false).toBe(false);
      expect(c.attributes?.["pkce.code.challenge.method"]).toBe("S256");
    }
  });
});
