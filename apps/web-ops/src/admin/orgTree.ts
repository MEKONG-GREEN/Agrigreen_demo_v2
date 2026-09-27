import type { Org } from "@/api/types";

export interface OrgNode {
  org: Org;
  children: OrgNode[];
}

/** Dựng cây từ danh sách phẳng theo parent_ref. Nút có cha không tồn tại (ngoài phạm vi) thành gốc. */
export function buildOrgTree(orgs: Org[]): OrgNode[] {
  const nodes = new Map<string, OrgNode>();
  for (const o of orgs) nodes.set(o.ref, { org: o, children: [] });
  const roots: OrgNode[] = [];
  for (const n of nodes.values()) {
    const parent = n.org.parent_ref ? nodes.get(n.org.parent_ref) : undefined;
    if (parent && parent !== n) parent.children.push(n);
    else roots.push(n);
  }
  const sort = (list: OrgNode[]) => {
    list.sort((a, b) => a.org.name.localeCompare(b.org.name, "vi"));
    list.forEach((c) => sort(c.children));
  };
  sort(roots);
  return roots;
}
