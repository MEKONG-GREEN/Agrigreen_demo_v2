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
import { DEFAULT_ROUTE, knMenu, MENU_VERSION, navigationRoute } from "./kn";
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
      ["Phòng Trồng trọt", "active", 10], // W11.11: + Báo cáo cho KHTC; W11.54: + Trang chủ phòng, Quy trình, văn bản
      ["Chăn nuôi - TS", "not_open", 0],
      ["Kế hoạch - TC", "not_open", 0],
      ["2 phòng khác", "not_open", 0],
    ]);
    // kn.menu/v3: nhóm "Dùng chung" cuối; W11.23: Contact đã mở (liên kết thường, không "Sắp có").
    const tree = fromKnMenu(admin);
    expect(tree.groups.at(-1)).toMatchObject({ key: "dung_chung", label: "Dùng chung", items: [{ key: "chung.contact", href: "/kn/dung-chung/contact" }] });
    expect(tree.groups.at(-1)!.items[0]!.status).toBeUndefined();
    const odd: KnMenu = { ...admin, groups: [{ key: "x", label: "X", items: [{ ...admin.groups[0]!.items[0]!, icon: "khong-co" }] }] };
    expect(fromKnMenu(odd).groups[0]!.items[0]!.icon).toBe("file");
  });
});

describe("kn.menu/v6 (BF-MENU #98, R-A6): menu theo Update prototype và hình khách gửi", () => {
  type Tree = string | [string, Tree[]];
  /** Cây nhãn của nhóm; "*" = Sắp có. */
  const tree = (items: KnMenu["groups"][number]["items"]): Tree[] =>
    items.map((i) => (i.children.length ? [i.label, tree(i.children)] : i.status === "coming_soon" ? `${i.label}*` : i.label));
  const menuOf = (name: string) => cases.find((c) => c.name === name)!.menu;
  const groupsOf = (name: string) => Object.fromEntries(menuOf(name).groups.map((g) => [g.label, tree(g.items)]));
  const KHTC: Tree = ["Báo cáo cho KHTC", ["Phiếu kỳ này", "Phiếu đã gửi", "Tiến độ các CT của phòng*", "Báo cáo KHTC đã xuất*"]];
  const T = (t02: boolean) =>
    ["T01 · Tiến độ XG", ...(t02 ? ["T02 · Lúa mùa"] : []), "T03 · Phân trà", "T04 · Rau màu", "T05 · Sâu bệnh lúa", "T06 · Sâu bệnh khác", "T07 · Thu hoạch"];
  const V = ["V01 · 3G3T", "V02 · 1P5G", "V03 · CGH", "V04 · CCG", "V05 · Lượng giống", "V06 · Mô hình"];

  it("mọi phòng ban: Điều hành / Báo cáo / Dữ liệu / Dùng chung, không còn nhóm Nghiệp vụ", () => {
    for (const c of cases.filter((x) => x.audience !== "admin")) expect(c.menu.groups.map((g) => g.key)).not.toContain("nghiep_vu");
    for (const code of ["trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"]) {
      expect(menuOf(`kn_tinh/${code}`).groups.map((g) => g.key)).toEqual(["dieu_hanh", "bao_cao", "du_lieu", "dung_chung"]);
    }
    // KHTC: Cấp Xã không có menu KHTC.
    expect(menuOf("kn_xa/ke_hoach_tai_chinh").groups.map((g) => g.key)).toEqual(["dieu_hanh", "bao_cao", "dung_chung"]);
  });

  it("UP-01 Trồng trọt Cấp Xã đúng hình file v1", () => {
    expect(groupsOf("kn_xa/trong_trot")).toEqual({
      "Điều hành": [["Nhiệm vụ", ["Công việc cần làm", "Lịch công tác"]]],
      "Báo cáo": [["Báo cáo tuần", T(true)], "Báo cáo tháng", ["Báo cáo hàng vụ", V], "Báo cáo của xã"],
      "Dữ liệu": [["Thu thập dữ liệu", ["Danh sách HTX", "Máy móc HTX", "Vẽ thửa ruộng", "Vật tư"]], "Quy trình, văn bản"], // W11.54
      "Dùng chung": ["Contact"],
    });
  });

  it("UP-02 Trồng trọt Cấp Tỉnh đúng hình file v2 (3 cấp)", () => {
    const g = groupsOf("kn_tinh/trong_trot");
    expect(g["Điều hành"]).toEqual(["Trang chủ phòng", "Dashboard", ["Nhiệm vụ", ["Phát hành biểu mẫu", "Trạng thái nộp", "Rà soát & chốt kỳ", "Lịch công tác"]]]);
    expect(g["Báo cáo"]).toEqual([
      ["Báo cáo tổng hợp các xã", [["Báo cáo tuần", T(false)], "Báo cáo tháng", ["Báo cáo hàng vụ", V], "Báo cáo năm"]],
      "Báo cáo Chi cục",
      KHTC,
    ]);
    expect(g["Dữ liệu"]).toEqual([["Thu thập dữ liệu", ["Danh sách HTX", "Máy móc HTX", "Thửa ruộng", "Vật tư"]], "Quy trình, văn bản"]); // W11.54
  });

  it("UP-03 CNTS Cấp Xã đúng hình file v1", () => {
    expect(groupsOf("kn_xa/chan_nuoi_thuy_san")).toEqual({
      "Điều hành": ["Việc cần làm", "Lịch trực KSGM của tôi", "Lịch công tác"],
      "Báo cáo": ["KSGM hằng ngày", "Nhập liệu tuần chăn nuôi", "Khai thác thủy sản tuần", "Báo cáo công việc tuần", "Báo cáo dịch bệnh"],
      "Dữ liệu": ["Hộ mô hình", ["Nhật ký sản xuất", ["Thủy sản", "Chăn nuôi"]], "Lịch tiêm phòng", "Bản đồ vùng dịch", "Điểm trình diễn", "Quy trình, văn bản"],
      "Dùng chung": ["Contact"],
    });
    expect(menuOf("kn_xa/chan_nuoi_thuy_san").default_route).toBe("/kn/cnts/bao-cao-ky");
  });

  it("UP-10 CNTS Cấp Tỉnh đúng hình khách gửi 23/10", () => {
    const g = groupsOf("kn_tinh/chan_nuoi_thuy_san");
    expect(g["Điều hành"]).toEqual([
      "Trang chủ phòng", // W11.54: trang chủ phòng (FN-17); "Trang chủ" theo hình giữ Dashboard
      "Trang chủ",
      "Cảnh báo",
      ["Biểu mẫu", ["Thiết kế biểu mẫu*", "Phát hành, theo dõi nộp", "Cấu hình nhắc nhở", "Rà soát nhật ký", "Cấp quyền"]],
      ["Lịch", ["Lịch trực KSGM", "Lịch tiêm phòng", "Lịch công tác cán bộ xã", "Lịch công tác phòng"]],
      ["Chiến dịch điều tra", ["Quản lý chiến dịch", "Tạo chiến dịch*"]],
    ]);
    expect(g["Báo cáo"]).toEqual(["Báo cáo định kỳ*", "KSGM tuần, tháng", "Thủy sản (Phụ lục 2)", "Báo cáo dịch bệnh", KHTC]);
    expect(g["Dữ liệu"]).toEqual([
      ["Số liệu cấp xã", ["Nhập liệu tuần*", "Khai thác thủy sản*", "Nhật ký sản xuất*", "Lịch tiêm (thực tế)"]],
      "Bản đồ vùng dịch",
      ["Dự án KNQG", ["Dự án, điểm trình diễn", "Mẫu biểu dự án*"]],
      "Quy trình, văn bản",
    ]);
    // Số đếm lịch chờ duyệt giữ ở "Lịch công tác cán bộ xã".
    const lich = menuOf("kn_tinh/chan_nuoi_thuy_san").groups[0]!.items.find((i) => i.label === "Lịch")!;
    expect(lich.children.find((i) => i.label === "Lịch công tác cán bộ xã")!.badge_source).toBe("extension.work_schedules.pending");
  });

  it("KHTC, Báo giá, HCTH: mục nghiệp vụ trong 3 nhóm; mục chưa có màn là Sắp có, mục khác bấm được", () => {
    const leaves = (name: string) => menuLinks(fromKnMenu(menuOf(name))).map((l) => `${l.group.key}:${l.node.key}`);
    expect(leaves("kn_tinh/ke_hoach_tai_chinh")).toEqual(
      expect.arrayContaining(["dieu_hanh:tinh.khtc.ky_bao_cao", "dieu_hanh:tinh.khtc.ke_hoach_nam", "bao_cao:tinh.khtc.bao_cao", "bao_cao:tinh.khtc.tien_do", "bao_cao:tinh.khtc.da_xuat"]),
    );
    expect(leaves("kn_tinh/thong_tin_truyen_thong")).toEqual(
      expect.arrayContaining(["dieu_hanh:tinh.bao_gia.tien_do", "dieu_hanh:tinh.bao_gia.kiem_duyet", "dieu_hanh:tinh.bao_gia.cau_hinh", "bao_cao:tinh.bao_gia.bang_gia",
        "bao_cao:tinh.bao_gia.bien_dong", "bao_cao:tinh.bao_gia.gia_thanh" /* W11.78 */, "bao_cao:tinh.bao_gia.dashboard", "bao_cao:tinh.bao_gia.thong_ke",
        "du_lieu:tinh.bao_gia.dieu_tra" /* W11.78 */]),
    );
    expect(leaves("kn_xa/thong_tin_truyen_thong")).toEqual(
      expect.arrayContaining(["dieu_hanh:xa.bao_gia.nhap", "dieu_hanh:xa.bao_gia.xac_minh", "bao_cao:xa.bao_gia.bang_gia", "du_lieu:xa.bao_gia.lich_su"]),
    );
    expect(leaves("kn_tw/thong_tin_truyen_thong")).toContain("bao_cao:tw.bao_gia.gia_thanh"); // W11.78: Giá thành sản xuất (TW) có màn
    expect(leaves("kn_tinh/hanh_chinh_tong_hop")).toEqual(
      expect.arrayContaining(["dieu_hanh:tinh.hcth.dashboard", "dieu_hanh:tinh.hcth.nang_bac", "dieu_hanh:tinh.hcth.quyet_dinh_nang_bac", "dieu_hanh:tinh.hcth.ke_hoach" /* W11.27 */, "bao_cao:tinh.hcth.bao_cao_so",
        "du_lieu:tinh.hcth.ho_so", "du_lieu:tinh.hcth.kho_tai_lieu", "du_lieu:tinh.hcth.danh_muc"]),
    );
    expect(groupsOf("kn_tinh/ke_hoach_tai_chinh")["Dữ liệu"]).toEqual(["Số liệu nguồn ngoài*", "Quy trình, văn bản"]);
    expect(groupsOf("kn_tinh/thong_tin_truyen_thong")["Dữ liệu"]).toEqual(["Điều tra giá thành", "Mặt hàng*", "Quy trình, văn bản"]); // W11.78: Điều tra giá thành có màn
    expect(groupsOf("kn_tw/chan_nuoi_thuy_san")["Dữ liệu"]).toEqual(["Dự án KNQG"]);
    // W11.54: Trang chủ phòng đầu Điều hành Cấp Tỉnh mọi phòng; Quy trình, văn bản (/kn/van-ban) cuối Dữ liệu; Cấp Xã/TW không có Trang chủ phòng.
    for (const code of ["trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"]) {
      expect(leaves(`kn_tinh/${code}`)[0]).toBe("dieu_hanh:tinh.trang_chu");
      expect(leaves(`kn_xa/${code}`)).not.toContain("dieu_hanh:tinh.trang_chu");
      expect(leaves(`kn_tw/${code}`)).not.toContain("dieu_hanh:tinh.trang_chu");
    }
    expect(leaves("kn_tinh/hanh_chinh_tong_hop").filter((l) => l.startsWith("du_lieu:")).at(-1)).toBe("du_lieu:tinh.van_ban");
    for (const code of ["trong_trot", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"]) expect(leaves(`kn_xa/${code}`).filter((l) => l.startsWith("du_lieu:")).at(-1)).toBe("du_lieu:xa.van_ban");
    expect(menuOf("kn_xa/chan_nuoi_thuy_san").groups.find((g) => g.key === "du_lieu")!.items.find((i) => i.key === "xa.cnts.quy_trinh")!.route).toBe("/kn/van-ban");
  });

  it("W11.11: Báo cáo cho KHTC cuối nhóm Báo cáo của Cấp Tỉnh 4 phòng chuyên môn; không ở KHTC, Cấp Xã", () => {
    const khtcItem = (name: string) =>
      cases
        .find((c) => c.name === name)!
        .menu.groups.flatMap((g) => g.items.map((i, idx) => ({ g: g.key, last: idx === g.items.length - 1, i })))
        .find((x) => x.i.key === "tinh.bao_cao_khtc");
    for (const code of ["trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"]) {
      const x = khtcItem(`kn_tinh/${code}`)!;
      expect(x).toMatchObject({ g: "bao_cao", last: true });
      const tree = fromKnMenu(cases.find((c) => c.name === `kn_tinh/${code}`)!.menu);
      const links = menuLinks(tree).map((l) => l.node.href);
      expect(links).toEqual(expect.arrayContaining(["/kn/bao-cao-khtc", "/kn/bao-cao-khtc/da-gui"]));
      expect(links).not.toContain("/kn/bao-cao-khtc/tien-do"); // FN-PN-03.03 Sắp có
    }
    expect(khtcItem("kn_tinh/ke_hoach_tai_chinh")).toBeUndefined();
    expect(khtcItem("kn_xa/trong_trot")).toBeUndefined();
  });

  it("mã tạm phong_ban_5 → Phòng Hành chính - Tổng hợp (Admin, /modules)", async () => {
    const res = await mockFetchNoDelay(
      new Request("http://x/v1/departments/phong_ban_5/modules?audience=admin", { headers: { Authorization: `Bearer ${mockToken("kn-admin")}` } }),
    );
    expect(res.status).toBe(200);
    expect(((await res.json()) as KnMenu).department?.code).toBe("hanh_chinh_tong_hop");
  });
});


describe("KHTC default navigation uses registered owner menu policy",()=>{
  it("KHTC and professional profile contexts land on their enabled pages",()=>{
    for(const [department,expected] of [["ke_hoach_tai_chinh","/kn/khtc/ky-bao-cao"],["trong_trot","/kn/bao-cao-khtc"],["chan_nuoi_thuy_san","/kn/bao-cao-khtc"],["thong_tin_truyen_thong","/kn/bao-cao-khtc"],["hanh_chinh_tong_hop","/kn/bao-cao-khtc"]] as const){
      const menu=knMenu("kn_tinh",department);
      expect(menu.default_route).toBe(expected);
      expect(menuLinks(fromKnMenu(menu)).some((item)=>item.node.href===expected)).toBe(true);
    }
  });
  it("disabled and parent routes cannot become landing pages",()=>{
    const groups:KnMenu["groups"]=[{key:"test",label:"test",items:[
      {key:"closed",label:"closed",route:"/kn/khtc/ky-bao-cao",icon:"",badge_source:null,enabled:false,children:[]},
      {key:"parent",label:"parent",route:"/parent",icon:"",badge_source:null,enabled:true,children:[{key:"leaf",label:"leaf",route:"/enabled",icon:"",badge_source:null,enabled:true,children:[]}]},
    ]}];
    expect(navigationRoute("kn_tinh","ke_hoach_tai_chinh",groups)).toBe("/enabled");
    expect(()=>navigationRoute("kn_tinh","ke_hoach_tai_chinh",[])).toThrow("no enabled");
  });
  it("select, current context and menu return the same canonical route",async()=>{
    let id:string|null=null;
    const api=createKnApi({baseUrl:"http://x",getAccessToken:()=>mockToken("kn-an"),getContextId:()=>id,fetch:mockFetchNoDelay});
    const selected=await api.selectContext({assignment_id:"asg-an-tinh",department_code:"trong_trot"});
    id=selected.context_id;
    const response=await mockFetchNoDelay(new Request("http://x/v1/me/context",{headers:{Authorization:`Bearer ${mockToken("kn-an")}`,"X-AG-Context":id!}}));
    expect(response.status).toBe(200);
    const current=await response.json() as {default_route:string};
    const menu=await api.menu();
    expect(current.default_route).toBe(selected.default_route);
    expect(selected.default_route).toBe("/kn/bao-cao-khtc");
    expect(menu.default_route).toBe(selected.default_route);
    for (const [token, context, status] of [[mockToken("kn-an"), null, 428], [mockToken("kn-admin"), id, 403]] as const) {
      const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
      if (context) headers["X-AG-Context"] = context;
      const denied = await mockFetchNoDelay(new Request("http://x/v1/me/context", { headers }));
      expect(denied.status).toBe(status);
    }
  });
});
