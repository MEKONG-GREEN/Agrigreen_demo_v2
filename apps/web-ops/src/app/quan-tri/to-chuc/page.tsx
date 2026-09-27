"use client";

import { useMemo, useState, type FormEvent } from "react";
import { buildOrgTree, type OrgNode } from "@/admin/orgTree";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { Org, OrgKind, PartyOrgEvent } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
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

const ACTION: Record<PartyOrgEvent["action"], string> = {
  created: "Tạo mới",
  moved: "Di chuyển trong cây",
  renamed: "Đổi tên",
  deactivated: "Vô hiệu hoá",
  reactivated: "Kích hoạt lại",
};

function Node({
  node,
  expanded,
  toggle,
  selected,
  onSelect,
}: {
  node: OrgNode;
  expanded: Set<string>;
  toggle: (ref: string) => void;
  selected: string | null;
  onSelect: (o: Org) => void;
}) {
  const open = expanded.has(node.org.ref);
  const hasChildren = node.children.length > 0;
  return (
    <li role="treeitem" aria-expanded={hasChildren ? open : undefined} aria-selected={selected === node.org.ref}>
      <span className="node">
        {hasChildren ? (
          <button type="button" className="toggle" onClick={() => toggle(node.org.ref)} aria-label={`${open ? "Thu gọn" : "Mở rộng"} ${node.org.name}`}>
            {open ? "−" : "+"}
          </button>
        ) : (
          <span style={{ width: 22, display: "inline-block" }} aria-hidden="true" />
        )}
        <button type="button" onClick={() => onSelect(node.org)} style={{ fontWeight: 700, textAlign: "left" }} aria-label={`Chi tiết ${node.org.name}`}>
          {node.org.name}
        </button>
        <span className={`chip ${KIND[node.org.kind].cls}`}>{KIND[node.org.kind].text}</span>
        <code className="mute small">{node.org.code}</code>
        {node.org.admin_unit_code ? <span className="mute small">ĐVHC {node.org.admin_unit_code}</span> : null}
        {!node.org.active ? <span className="chip c-mute">Ngừng hoạt động</span> : null}
      </span>
      {hasChildren && open ? (
        <ul role="group">
          {node.children.map((c) => (
            <Node key={c.org.ref} node={c} expanded={expanded} toggle={toggle} selected={selected} onSelect={onSelect} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/** Chi tiết một tổ chức: nhật ký biến động + đổi tên — Admin API (MFA bắt buộc). */
function OrgPanel({ org, onRenamed }: { org: Org; onRenamed: () => void }) {
  const { adminApi } = useAuth();
  const events = useAsync(() => unwrap(adminApi.GET("/v1/orgs/{ref}/events", { params: { path: { ref: org.ref } } })), [adminApi, org.ref]);
  const [name, setName] = useState(org.name);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/orgs/{ref}/rename", {
          params: { path: { ref: org.ref }, header: { "Idempotency-Key": idemKey } },
          body: { name: name.trim(), reason: reason.trim() || undefined },
        }),
      );
      setIdemKey(newIdempotencyKey());
      events.reload();
      onRenamed();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="stack">
      <div>
        <h2 style={{ fontSize: 17 }}>{org.name}</h2>
        <code className="mute small">{org.ref}</code>
      </div>
      <form className="stack" style={{ gap: 8 }} onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor="org-name">Tên mới</label>
          <input
            id="org-name"
            className="input"
            value={name}
            maxLength={200}
            onChange={(e) => {
              setName(e.target.value);
              setIdemKey(newIdempotencyKey());
            }}
          />
        </div>
        <div className="field">
          <label htmlFor="org-reason">Lý do (ghi vào sổ biến động)</label>
          <input
            id="org-reason"
            className="input"
            value={reason}
            onChange={(e) => {
              setReason(e.target.value);
              setIdemKey(newIdempotencyKey());
            }}
          />
        </div>
        <div>
          <button type="submit" className="btn btn-p btn-sm" disabled={busy || !name.trim() || name.trim() === org.name}>
            {busy ? "Đang lưu…" : "Đổi tên"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </form>
      <div>
        <h3 style={{ fontSize: 14, marginBottom: 6 }}>Nhật ký biến động</h3>
        {events.error ? <ProblemAlert problem={events.error} onRetry={events.reload} /> : null}
        {events.loading && !events.data ? <Loading /> : null}
        <ol style={{ margin: "0 0 0 18px" }}>
          {(events.data?.items ?? []).map((ev) => (
            <li key={ev.seq} className="small" style={{ marginBottom: 6 }}>
              <b style={{ display: "inline" }}>{ACTION[ev.action]}</b> · {formatDateTime(ev.event_time)} — {ev.name}
              {ev.reason ? <div className="mute">Lý do: {ev.reason}</div> : null}
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}

/**
 * Cơ cấu tổ chức. Cây đọc qua Application API GET /v1/orgs (Admin API không có route liệt kê — G-08);
 * nhật ký và đổi tên đi qua Admin API.
 */
export default function OrgTreePage() {
  const { api, principal } = useAuth();
  const orgs = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/orgs", { params: { query: { include_inactive: true, cursor, limit: 200 } } })), 5000),
    [api],
  );
  const tree = useMemo(() => buildOrgTree(orgs.data ?? []), [orgs.data]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Org | null>(null);
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
      {principal && !principal.mfa ? (
        <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status">
          <Icon name="alert" />
          <span>Phiên hiện tại chưa qua xác thực hai yếu tố — nhật ký và đổi tên (Admin API) sẽ bị từ chối (mfa_required).</span>
        </div>
      ) : null}
      {orgs.error ? <ProblemAlert problem={orgs.error} onRetry={orgs.reload} /> : null}
      {orgs.loading && !orgs.data ? <Loading /> : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(280px, 1fr)", alignItems: "start" }}>
        <div className="card sec tree">
          <ul role="tree" aria-label="Cây tổ chức">
            {tree.map((n) => (
              <Node key={n.org.ref} node={n} expanded={effective} toggle={toggle} selected={selected?.ref ?? null} onSelect={setSelected} />
            ))}
          </ul>
        </div>
        <aside className="card sec" aria-label="Chi tiết tổ chức">
          {selected ? (
            <OrgPanel key={selected.ref} org={selected} onRenamed={orgs.reload} />
          ) : (
            <p className="mute">Chọn một tổ chức để xem nhật ký biến động hoặc đổi tên.</p>
          )}
        </aside>
      </div>
    </>
  );
}
