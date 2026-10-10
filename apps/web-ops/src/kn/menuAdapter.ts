// KNMenu (wire, GET /v1/menu — contracts/openapi/application.yaml, bảng Go internal/platform/party/domain/menu.go)
// → cây menu của khung (MenuTree, src/kn/contract.ts) mà Sidebar/breadcrumb dùng.
// Chỉ đổi cách TRÌNH BÀY, không thêm/bớt mục: icon lạ → icon dự phòng; nhóm "Phòng ban" của Admin theo Hình 3c
// (phòng ban chưa mở hiện mờ, tên ngắn, hai phòng đầu hiện riêng, phần còn lại gộp "n phòng khác").
import type { NavIcon } from "@/nav/nav";
import type { KnMenu, KnMenuItem, KnRole, MenuNode, MenuTree } from "./contract";

const NAV_ICONS: ReadonlySet<string> = new Set<NavIcon>([
  "dashboard", "map", "coop", "task", "library", "catalog", "param", "org", "tractor", "megaphone", "report", "bell", "sync",
  "user", "conflict", "upload", "phone", "layers", "water", "warehouse", "sim", "season", "journal", "approve", "harvest",
  "chart", "qr", "support", "sop", "plot", "calendar", "sprout", "send", "database", "building", "package", "table", "key",
  "users", "zap", "file", "check", "shield",
]);

/** Icon của backend (tên NavIcon); tên lạ (bảng menu mới hơn web) → "file". */
export function toNavIcon(name: string): NavIcon {
  return (NAV_ICONS.has(name) ? name : "file") as NavIcon;
}

/** Tên ngắn của phòng ban chưa mở trong menu Admin (Hình 3c). Vắng → tên đầy đủ. */
export const SHORT_DEPT_NAME: Readonly<Record<string, string>> = {
  trong_trot: "Trồng trọt",
  chan_nuoi_thuy_san: "Chăn nuôi - TS",
  ke_hoach_tai_chinh: "Kế hoạch - TC",
  thong_tin_truyen_thong: "Thông tin TT",
  hanh_chinh_tong_hop: "Hành chính - TH",
};

/** Khóa nhóm và tiền tố khóa mục phòng ban của menu Admin (domain.AdminDepartmentGroupKey). */
export const ADMIN_DEPARTMENT_GROUP = "phong_ban";

function node(it: KnMenuItem): MenuNode {
  const n: MenuNode = { key: it.key, label: it.label, icon: toNavIcon(it.icon) };
  if (it.route) n.href = it.route;
  if (it.badge_source) n.badge_source = it.badge_source;
  if (it.status === "coming_soon") n.status = "coming_soon";
  else if (!it.enabled) n.status = "not_open";
  if (it.children.length) n.children = it.children.map(node);
  return n;
}

/** Hình 3c: phòng ban mở giữ mục con; chưa mở chỉ hiện tên ngắn (mờ), tối đa 2 rồi gộp "n phòng khác". */
function adminDepartments(items: readonly KnMenuItem[]): MenuNode[] {
  const out: MenuNode[] = [];
  const closed: KnMenuItem[] = [];
  for (const it of items) {
    if (it.enabled) out.push(node(it));
    else closed.push(it);
  }
  for (const it of closed.slice(0, 2)) {
    const code = it.key.slice(ADMIN_DEPARTMENT_GROUP.length + 1);
    out.push({ key: it.key, label: SHORT_DEPT_NAME[code] ?? it.label, icon: toNavIcon(it.icon), status: "not_open" });
  }
  if (closed.length > 2) {
    out.push({ key: `${ADMIN_DEPARTMENT_GROUP}._khac`, label: `${closed.length - 2} phòng khác`, icon: "building", status: "not_open" });
  }
  return out;
}

export function fromKnMenu(m: KnMenu): MenuTree {
  return {
    version: m.menu_version,
    role: m.audience as KnRole,
    department_code: m.department?.code ?? null,
    default_route: m.default_route,
    groups: m.groups.map((g) => ({
      key: g.key,
      label: g.label,
      items: m.audience === "admin" && g.key === ADMIN_DEPARTMENT_GROUP ? adminDepartments(g.items) : g.items.map(node),
    })),
  };
}
