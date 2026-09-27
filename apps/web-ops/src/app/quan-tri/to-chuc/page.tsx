"use client";

import { useMemo, useState } from "react";
import { buildOrgTree, type OrgNode } from "@/admin/orgTree";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { OrgKind } from "@/api/pending/common";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { useAsync } from "@/lib/useAsync";

const KIND: Record<OrgKind, { text: string; cls: string }> = {
  ttkn_national: { text: "TTKN Quốc gia", cls: "c-ok" },
  ttkn_province: { text: "TTKN tỉnh", cls: "c-info" },
  extension_team: { text: "Tổ KN / KNV xã", cls: "c-violet" },
  dcrd: { text: "Cục KTHT", cls: "c-warn" },
  cooperative: { text: "HTX / THT", cls: "c-mute" },
  mkg: { text: "Mekong Green", cls: "c-mute" },
  partner: { text: "Đối tác", cls: "c-mute" },
};

function Node({ node, expanded, toggle }: { node: OrgNode; expanded: Set<string>; toggle: (ref: string) => void }) {
  const open = expanded.has(node.org.ref);
  const hasChildren = node.children.length > 0;
  return (
    <li role="treeitem" aria-expanded={hasChildren ? open : undefined} aria-selected={false}>
      <span className="node">
        {hasChildren ? (
          <button type="button" className="toggle" onClick={() => toggle(node.org.ref)} aria-label={`${open ? "Thu gọn" : "Mở rộng"} ${node.org.name}`}>
            {open ? "−" : "+"}
          </button>
        ) : (
          <span style={{ width: 22, display: "inline-block" }} aria-hidden="true" />
        )}
        <b>{node.org.name}</b>
        <span className={`chip ${KIND[node.org.kind].cls}`}>{KIND[node.org.kind].text}</span>
        <code className="mute small">{node.org.code}</code>
        {node.org.admin_unit_code ? <span className="mute small">ĐVHC {node.org.admin_unit_code}</span> : null}
        {!node.org.active ? <span className="chip c-mute">Ngừng hoạt động</span> : null}
      </span>
      {hasChildren && open ? (
        <ul role="group">
          {node.children.map((c) => (
            <Node key={c.org.ref} node={c} expanded={expanded} toggle={toggle} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

export default function OrgTreePage() {
  const { api } = useAuth();
  const orgs = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/orgs", { params: { query: { cursor, limit: 200 } } })), 5000),
    [api],
  );
  const tree = useMemo(() => buildOrgTree(orgs.data ?? []), [orgs.data]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const effective = expanded.size === 0 && tree.length > 0 ? new Set(tree.map((t) => t.org.ref)) : expanded;

  const toggle = (ref: string) =>
    setExpanded(() => {
      const next = new Set(effective);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });

  return (
    <>
      <PageHead
        title="Cơ cấu tổ chức"
        subtitle="Cây phân cấp TW → Tỉnh → Xã quyết định phạm vi dữ liệu của từng tài khoản"
        actions={
          <>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setExpanded(new Set((orgs.data ?? []).map((o) => o.ref)))}>
              Mở rộng tất cả
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setExpanded(new Set(["__none__"]))}>
              Thu gọn
            </button>
          </>
        }
      />
      {orgs.error ? <ProblemAlert problem={orgs.error} onRetry={orgs.reload} /> : null}
      {orgs.loading && !orgs.data ? <Loading /> : null}
      <div className="card sec tree">
        <ul role="tree" aria-label="Cây tổ chức">
          {tree.map((n) => (
            <Node key={n.org.ref} node={n} expanded={effective} toggle={toggle} />
          ))}
        </ul>
      </div>
    </>
  );
}
