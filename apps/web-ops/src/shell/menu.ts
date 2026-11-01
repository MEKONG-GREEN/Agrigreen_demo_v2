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
const HRM_OWN_APPRAISAL = "/kn/nhan-su/danh-gia-cua-toi";
const appraisalUUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
const appraisalDetail = new RegExp(`^/dot/${appraisalUUID}(?:/phieu/${appraisalUUID})?$`,"i");

// Alias thông báo chỉ đến phiếu UUID và lá tự đánh giá đang dùng trong phòng HCTH.
// Không dùng nhãn và không cho phép toàn bộ tiền tố HRM; backend kiểm quyền bản chụp lần nữa.
function appraisalLink(tree: MenuTree, pathname: string): MenuLink | null | undefined {
 const own = pathname === HRM_OWN_APPRAISAL || pathname.startsWith(HRM_OWN_APPRAISAL + "/");
 const center = pathname === HRM_APPRAISAL || pathname.startsWith(HRM_APPRAISAL + "/");
 if(!own && !center)return undefined;
 const admin = tree.role === "admin";
 if(!admin && (tree.department_code !== "hanh_chinh_tong_hop" || (tree.role !== "kn_tinh" && tree.role !== "kn_xa")))return null;
 if(admin && own)return null;
 const base = own ? HRM_OWN_APPRAISAL : HRM_APPRAISAL;
 const tail = pathname.slice(base.length);
 const detail = appraisalDetail.test(tail);
 const editor = center && (tree.role === "kn_tinh" || admin) && (tail === "/dot/them-moi" || new RegExp(`^/mau/(?:them-moi|${appraisalUUID})$`,"i").test(tail));
 if(tail !== "" && !(center && tail === "/mau" && (tree.role === "kn_tinh" || admin)) && !detail && !editor)return null;
 const links = menuLinks(tree);
 const direct = links.find(l => l.node.href === base);
 if(direct)return direct;
 return !admin && center && detail ? links.find(l => l.node.href === HRM_OWN_APPRAISAL) ?? null : null;
}

/** Mục đang chọn: khớp tiền tố dài nhất (trang chi tiết thuộc mục danh sách). Chỉ một mục active. */
export function findActive(tree: MenuTree, pathname: string): MenuLink | undefined {
  const appraisal = appraisalLink(tree, pathname);
  if(appraisal !== undefined)return appraisal ?? undefined;
  let best: MenuLink | undefined;
  for (const l of menuLinks(tree)) {
    if (matches(l.node.href!, pathname) && (!best || l.node.href!.length > best.node.href!.length)) best = l;
  }
  if (best) return best;
  // The detail screens use a historical URL outside their list's prefix. Bind
  // only these exact UUID routes to an existing usable list, never /kn/khtc/*.
  const detail = /^\/kn\/khtc\/(phieu|ra-soat)\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.exec(pathname);
  if (!detail) return undefined;
  const parents = detail[1]!.toLowerCase() === "phieu"
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
