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
  parent: MenuNode | null;
  group: MenuGroup;
}

/** Mọi liên kết (lá có href) theo thứ tự hiển thị. */
export function menuLinks(tree: MenuTree): MenuLink[] {
  const out: MenuLink[] = [];
  for (const group of tree.groups) {
    for (const item of group.items) {
      if (item.children?.length) {
        for (const c of item.children) if (c.href && c.status !== "not_open") out.push({ node: c, parent: item, group });
      } else if (item.href && item.status !== "not_open") out.push({ node: item, parent: null, group });
    }
  }
  return out;
}

function matches(href: string, pathname: string): boolean {
  return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
}

/** Mục đang chọn: khớp tiền tố dài nhất (trang chi tiết thuộc mục danh sách). Chỉ một mục active. */
export function findActive(tree: MenuTree, pathname: string): MenuLink | undefined {
  let best: MenuLink | undefined;
  for (const l of menuLinks(tree)) {
    if (matches(l.node.href!, pathname) && (!best || l.node.href!.length > best.node.href!.length)) best = l;
  }
  return best;
}

/** Đường dẫn có thuộc menu hiện tại không (kiểm quyền hiển thị phía client — backend vẫn kiểm R-55). */
export function menuAllows(tree: MenuTree, pathname: string): boolean {
  return !!findActive(tree, pathname);
}

/** Breadcrumb: [mục cha hoặc nhóm, mục] — "Điều hành / Dashboard", "Nhiệm vụ / Công việc cần làm". */
export function crumbsFor(tree: MenuTree, pathname: string): string[] {
  const a = findActive(tree, pathname);
  if (!a) return [];
  return [a.parent?.label ?? a.group.label, a.node.label];
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
