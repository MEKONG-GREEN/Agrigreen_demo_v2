// Hợp đồng menu Go ↔ web-ops (KN-fix, UR-KT-02): bản giả lập menu (kn.ts) phải trả ĐÚNG KNMenu mà bảng Go
// (internal/platform/party/domain/menu.go) dựng — ca vàng fixtures/kn-menu.json do Go sinh:
//   AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture
import { describe, expect, it } from "vitest";
import type { KnMenu, KnRole } from "@/kn/contract";
import { createKnApi } from "@/kn/client";
import { fromKnMenu } from "@/kn/menuAdapter";
import { menuLinks } from "@/shell/menu";
import fixture from "./fixtures/kn-menu.json";
import { mockFetchNoDelay } from "./handler";
import { DEFAULT_ROUTE, knMenu, MENU_VERSION } from "./kn";
import { KN_DEPARTMENTS, KN_MENU_PROFILE } from "./kn-accounts";
import { mockToken } from "./personas";

interface FixtureCase {
  name: string;
  audience: KnRole;
  department?: string;
  ad_hoc: boolean;
  menu: KnMenu;
}
const cases = fixture.cases as unknown as FixtureCase[];
/** org_ref là dữ liệu của từng đơn vị (Go: uuid.Nil), không thuộc bảng menu. */
const shape = (m: KnMenu) => ({ ...m, org_ref: null });

describe("menu giả lập = bảng menu Go (kn-menu.json)", () => {
  it("phiên bản, trang mặc định (P1 default_route = P2), 5 phòng ban nền (W11.06)", () => {
    expect(MENU_VERSION).toBe(fixture.menu_version);
    expect(DEFAULT_ROUTE).toEqual(fixture.default_routes);
    expect(KN_DEPARTMENTS.map((d) => ({ code: d.code, name: d.name, status: d.status, menu_profile: KN_MENU_PROFILE[d.code] ?? "generic" }))).toEqual(
      fixture.departments,
    );
  });

  it.each(cases.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    expect(shape(knMenu(c.audience, c.department ?? null, { adHoc: c.ad_hoc }))).toEqual(c.menu);
  });

  it("GET /v1/menu theo ngữ cảnh phiên trả đúng ca vàng; ?role= (tên cũ) khác ngữ cảnh → 403", async () => {
    let ctxId: string | null = null;
    const kn = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("kn-an"), getContextId: () => ctxId, fetch: mockFetchNoDelay });
    const ctx = await kn.selectContext({ assignment_id: "asg-an-xa", department_code: "trong_trot" });
    ctxId = ctx.context_id;
    const res = await mockFetchNoDelay(new Request("http://x/v1/menu", { headers: { Authorization: `Bearer ${mockToken("kn-an")}`, "X-AG-Context": ctxId } }));
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    expect(shape((await res.json()) as KnMenu)).toEqual(cases.find((c) => c.name === "kn_xa/trong_trot")!.menu);
    expect(ctx.default_route).toBe(fixture.default_routes.kn_xa);
    const forbidden = await mockFetchNoDelay(
      new Request("http://x/v1/menu?role=kn_tinh", { headers: { Authorization: `Bearer ${mockToken("kn-an")}`, "X-AG-Context": ctxId } }),
    );
    expect(forbidden.status).toBe(403);
    // Client của khung: cùng menu, dựng sang cây vẽ.
    expect(await kn.menu()).toEqual(fromKnMenu(cases.find((c) => c.name === "kn_xa/trong_trot")!.menu));
  });

  it("mọi đường dẫn KN theo hợp đồng: /kn/<màn> (không mã phòng ban), Admin theo phòng ban /kn/phong-ban/{mã}/…", () => {
    const routes: string[] = [];
    const walk = (items: KnMenu["groups"][number]["items"]) =>
      items.forEach((i) => {
        if (i.route) routes.push(i.route);
        walk(i.children);
      });
    cases.forEach((c) => c.menu.groups.forEach((g) => walk(g.items)));
    for (const r of routes.filter((x) => x.startsWith("/kn/"))) {
      expect(r).not.toMatch(/\{|\/kn\/(trong_trot|chan_nuoi)/);
      if (r.startsWith("/kn/phong-ban/")) expect(r).toMatch(/^\/kn\/phong-ban\/[a-z_0-9]+\/[a-z-]+$/);
    }
    expect(routes).toContain("/kn/nhiem-vu/lich-cong-tac");
    expect(routes).toContain("/kn/phong-ban/trong_trot/mau-bao-cao");
  });
});

describe("fromKnMenu — trình bày Hình 3c", () => {
  it("Admin: phòng ban chưa mở mờ, tên ngắn, 2 phòng đầu rồi gộp 'n phòng khác'; icon lạ → dự phòng", () => {
    const admin = cases.find((c) => c.name === "admin")!.menu;
    const pb = fromKnMenu(admin).groups.find((g) => g.key === "phong_ban")!;
    expect(pb.items.map((i) => [i.label, i.status ?? "active", i.children?.length ?? 0])).toEqual([
      ["Phòng Trồng trọt", "active", 7],
      ["Chăn nuôi - TS", "not_open", 0],
      ["Kế hoạch - TC", "not_open", 0],
      ["2 phòng khác", "not_open", 0],
    ]);
    // kn.menu/v3: nhóm "Dùng chung" cuối; Contact "Sắp có" không thành liên kết.
    const tree = fromKnMenu(admin);
    expect(tree.groups.at(-1)).toMatchObject({ key: "dung_chung", label: "Dùng chung", items: [{ key: "chung.contact", status: "coming_soon" }] });
    const odd: KnMenu = { ...admin, groups: [{ key: "x", label: "X", items: [{ ...admin.groups[0]!.items[0]!, icon: "khong-co" }] }] };
    expect(fromKnMenu(odd).groups[0]!.items[0]!.icon).toBe("file");
  });
});

describe("kn.menu/v3 (W11.06): khuôn theo phòng ban + Dùng chung", () => {
  it("phòng ban khác Trồng trọt: Điều hành / Nghiệp vụ (Sắp có) / Báo cáo / Dùng chung; mục Sắp có không bấm được", () => {
    for (const code of ["chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"]) {
      const m = cases.find((c) => c.name === `kn_tinh/${code}`)!.menu;
      expect(m.groups.map((g) => g.key)).toEqual(["dieu_hanh", "nghiep_vu", "bao_cao", "dung_chung"]);
      const tree = fromKnMenu(m);
      const biz = tree.groups.find((g) => g.key === "nghiep_vu")!;
      expect(biz.items.every((i) => i.status === "coming_soon")).toBe(true);
      const links = menuLinks(tree).map((l) => l.node.key);
      expect(links).toContain("tinh.dashboard"); // khung chung vẫn chạy
      expect(links.some((k) => biz.items.some((i) => i.key === k))).toBe(false);
    }
    // KHTC: Cấp Xã không có menu KHTC.
    expect(cases.find((c) => c.name === "kn_xa/ke_hoach_tai_chinh")!.menu.groups.map((g) => g.key)).toEqual(["dieu_hanh", "bao_cao", "dung_chung"]);
  });

  it("mã tạm phong_ban_5 → Phòng Hành chính - Tổng hợp (Admin, /modules)", async () => {
    const res = await mockFetchNoDelay(
      new Request("http://x/v1/departments/phong_ban_5/modules?audience=admin", { headers: { Authorization: `Bearer ${mockToken("kn-admin")}` } }),
    );
    expect(res.status).toBe(200);
    expect(((await res.json()) as KnMenu).department?.code).toBe("hanh_chinh_tong_hop");
  });
});
