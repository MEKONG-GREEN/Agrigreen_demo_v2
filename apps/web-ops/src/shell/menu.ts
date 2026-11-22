// Mô hình menu dùng chung của khung: cây cấu hình KN v7.7 (P2) hoặc menu cũ nav.ts đổi sang cùng dạng.
import type { MenuBadges, MenuGroup, MenuNode, MenuTree } from "@/kn/contract";
import type { NavGroup } from "@/nav/nav";

/** Menu cũ (src/nav/nav.ts) → cây menu: mỗi nhóm giữ nguyên nhãn, mục phẳng. */
export function legacyMenu(groups: readonly NavGroup[]): MenuTree {
  return {
    version: "legacy",
    role: null,
    department_code: null,
    groups: groups.map((g, gi) => ({
      key: `legacy-${gi}`,
      label: g.label,
      items: g.items.map((i) => ({ key: i.href, label: i.label, icon: i.icon, href: i.href })),
    })),
  };
}

export interface MenuLink {
  node: MenuNode;
  /** Mục cha gần nhất (null nếu là mục cấp 1). */
  parent: MenuNode | null;
  /** Chuỗi mục cha từ cấp 1 xuống (menu TT tỉnh có 3 cấp: Báo cáo tổng hợp › Báo cáo tuần › T01). */
  ancestors: MenuNode[];
  group: MenuGroup;
}

/** Lá bấm được: có href, không "Chưa mở" / "Sắp có" (kn.menu/v3). */
const usable = (n: MenuNode): boolean => !!n.href && n.status !== "not_open" && n.status !== "coming_soon";

/** Mọi liên kết (lá có href) theo thứ tự hiển thị, mọi cấp. */
export function menuLinks(tree: MenuTree): MenuLink[] {
  const out: MenuLink[] = [];
  const walk = (items: readonly MenuNode[], ancestors: MenuNode[], group: MenuGroup) => {
    for (const item of items) {
      if (item.children?.length) walk(item.children, [...ancestors, item], group);
      else if (usable(item)) out.push({ node: item, parent: ancestors[ancestors.length - 1] ?? null, ancestors, group });
    }
  };
  for (const group of tree.groups) walk(group.items, [], group);
  return out;
}

function matches(href: string, pathname: string): boolean {
  return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
}

const HRM_APPRAISAL = "/kn/nhan-su/danh-gia";
const appraisalUUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
const appraisalDetail = new RegExp(`^/dot/${appraisalUUID}(?:/phieu/${appraisalUUID})?$`,"i");

// Chỉ các trang hiện có được ánh xạ vào lá đánh giá của lãnh đạo/HCTH.
// Không nhận URL cũ hoặc đường dẫn gần giống; backend kiểm phân công lần nữa.
function appraisalLink(tree: MenuTree, pathname: string): MenuLink | null | undefined {
 if(!pathname.startsWith(HRM_APPRAISAL))return undefined;
 if(pathname !== HRM_APPRAISAL && !pathname.startsWith(HRM_APPRAISAL + "/"))return null;
 const admin = tree.role === "admin";
 if(!admin && (tree.department_code !== "hanh_chinh_tong_hop" || tree.role !== "kn_tinh"))return null;
 const tail = pathname.slice(HRM_APPRAISAL.length);
 const detail = appraisalDetail.test(tail);
 const editor = tail === "/dot/them-moi" || new RegExp(`^/mau/(?:them-moi|${appraisalUUID})$`,"i").test(tail);
 if(tail !== "" && tail !== "/mau" && !detail && !editor)return null;
 return menuLinks(tree).find(l => l.node.href === HRM_APPRAISAL) ?? null;
}

// Historical FN19 pages have department aliases, while module menus use the
// canonical HRM leaf. Only actual page shapes map, preserving the entire suffix
// so the normal longest-prefix matcher and usable menu permissions still apply.
function hrmCatalogAlias(tree: MenuTree, pathname: string): string | undefined {
  const prefix = "/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su";
  if (pathname !== prefix && !pathname.startsWith(prefix + "/")) return undefined;
  if (tree.role !== "admin" && (tree.role !== "kn_tinh" || tree.department_code !== "hanh_chinh_tong_hop")) return undefined;
  const tail = pathname.slice(prefix.length);
  const kind = "(?:position|grade|education_level|major|major_group|ethnicity|religion|political_theory|state_mgmt|foreign_language|language_level|it_level|award_form|discipline_form|change_type|leave_reason)";
  const code = "[a-z][a-z0-9_]{0,39}";
  const grade = "[A-Za-z0-9][A-Za-z0-9._-]{0,39}";
  const catalog = new RegExp(`^/${kind}(?:/them-moi)?$`).test(tail)
    || new RegExp(`^/grade/muc/${grade}(?:/sua)?$`).test(tail)
    || new RegExp(`^/${kind}/muc/${code}(?:/sua)?$`).test(tail);
  const scale = tail === "/he-so" || new RegExp(`^/he-so/${grade}(?:/them-moi)?$`).test(tail);
  const rule = /^\/quy-tac\/(?:promotion|achievement_conversion)(?:\/them-moi)?$/.test(tail);
  return tail === "" || catalog || scale || rule ? "/kn/nhan-su/danh-muc" + tail : undefined;
}

/** Mục đang chọn: khớp tiền tố dài nhất (trang chi tiết thuộc mục danh sách). Chỉ một mục active. */
export function findActive(tree: MenuTree, pathname: string): MenuLink | undefined {
  const canonical = hrmCatalogAlias(tree, pathname);
  if (canonical) {
    const active = findActive(tree, canonical);
    if (active) return active;
  }
  const appraisal = appraisalLink(tree, pathname);
  if(appraisal !== undefined)return appraisal ?? undefined;
  let best: MenuLink | undefined;
  for (const l of menuLinks(tree)) {
    if (matches(l.node.href!, pathname) && (!best || l.node.href!.length > best.node.href!.length)) best = l;
  }
  if (best) return best;
  // The detail screens use a historical URL outside their list's prefix. Bind
  // only these exact UUID routes to an existing usable list, never /kn/khtc/*.
  const uuid = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
  const detail = new RegExp(`^/kn/khtc/(phieu|ra-soat)/${uuid}$`, "i").exec(pathname);
  const history = new RegExp(`^/kn/khtc/ra-soat/${uuid}/lan-gui(?:/${uuid})?$`, "i").test(pathname);
  if (!detail && !history) return undefined;
  const parents = detail?.[1]?.toLowerCase() === "phieu"
    ? ["/kn/bao-cao-khtc", "/kn/khtc/ky-bao-cao", "/kn/khtc/bao-cao-phat-sinh"]
    : ["/kn/khtc/ky-bao-cao", "/kn/khtc/bao-cao-phat-sinh"];
  return menuLinks(tree).find((l) => parents.includes(l.node.href!));
}

/** Đường dẫn có thuộc menu hiện tại không (kiểm quyền hiển thị phía client — backend vẫn kiểm R-55). */
export function menuAllows(tree: MenuTree, pathname: string): boolean {
  return !!findActive(tree, pathname);
}

/** Breadcrumb: [các mục cha hoặc nhóm, mục] — "Điều hành / Dashboard", "Nhiệm vụ / Công việc cần làm",
 *  "Báo cáo tổng hợp các xã / Báo cáo tuần / T01 · Tiến độ XG". */
export function crumbsFor(tree: MenuTree, pathname: string): string[] {
  const a = findActive(tree, pathname);
  if (!a) return [];
  return a.ancestors.length ? [...a.ancestors.map((n) => n.label), a.node.label] : [a.group.label, a.node.label];
}

/** Số đếm của mục (ẩn khi 0 hoặc không có nguồn). Mục cha không tự cộng — chỉ việc cần xử lý mới có số. */
export function badgeOf(node: MenuNode, badges: MenuBadges | null): number {
  if (!node.badge_source || !badges) return 0;
  const n = badges.counts[node.badge_source] ?? 0;
  return n > 0 ? n : 0;
}

/** Trang đầu tiên của menu (dự phòng khi không có default_route). */
export function firstHref(tree: MenuTree): string | undefined {
  return menuLinks(tree)[0]?.node.href;
}

/** Ghép thêm nhóm (vd menu cũ sau cờ NEXT_PUBLIC_KN_LEGACY_MENU=1). */
export function withExtraGroups(tree: MenuTree, extra: readonly MenuGroup[]): MenuTree {
  return extra.length ? { ...tree, groups: [...tree.groups, ...extra] } : tree;
}
