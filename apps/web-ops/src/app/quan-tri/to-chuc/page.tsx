"use client";

import { useSearchParams } from "next/navigation";
import {
  Suspense,
  useMemo,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import { OrgCreateDialog } from "@/admin/OrgCreateDialog";
import { canCreateOrg } from "@/admin/orgCreate";
import { buildOrgTree, type OrgNode } from "@/admin/orgTree";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import type { Org, OrgImportResult, OrgKind, PartyOrgEvent } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import {
  filenameFromDisposition,
  readUpload,
  saveBlob,
  type UploadFile,
} from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

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
    <li
      role="treeitem"
      aria-expanded={hasChildren ? open : undefined}
      aria-selected={selected === node.org.ref}
    >
      <span className="node">
        {hasChildren ? (
          <button
            type="button"
            className="toggle"
            onClick={() => toggle(node.org.ref)}
            aria-label={`${open ? "Thu gọn" : "Mở rộng"} ${node.org.name}`}
          >
            {open ? "−" : "+"}
          </button>
        ) : (
          <span
            style={{ width: 22, display: "inline-block" }}
            aria-hidden="true"
          />
        )}
        <button
          type="button"
          onClick={() => onSelect(node.org)}
          style={{ fontWeight: 700, textAlign: "left" }}
          aria-label={`Chi tiết ${node.org.name}`}
        >
          {node.org.name}
        </button>
        <span className={`chip ${KIND[node.org.kind].cls}`}>
          {KIND[node.org.kind].text}
        </span>
        <code className="mute small">{node.org.code}</code>
        {node.org.admin_unit_code ? (
          <span className="mute small">ĐVHC {node.org.admin_unit_code}</span>
        ) : null}
        {!node.org.active ? (
          <span className="chip c-mute">Ngừng hoạt động</span>
        ) : null}
      </span>
      {hasChildren && open ? (
        <ul role="group">
          {node.children.map((c) => (
            <Node
              key={c.org.ref}
              node={c}
              expanded={expanded}
              toggle={toggle}
              selected={selected}
              onSelect={onSelect}
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/** Chi tiết một tổ chức: nhật ký biến động + đổi tên — Admin API (MFA bắt buộc). */
function OrgPanel({ org, onRenamed }: { org: Org; onRenamed: () => void }) {
  const { adminApi } = useAuth();
  const events = useAsync(
    () =>
      unwrap(
        adminApi.GET("/v1/orgs/{ref}/events", {
          params: { path: { ref: org.ref } },
        }),
      ),
    [adminApi, org.ref],
  );
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
          params: {
            path: { ref: org.ref },
            header: { "Idempotency-Key": idemKey },
          },
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
      <form
        className="stack"
        style={{ gap: 8 }}
        onSubmit={(e) => void submit(e)}
      >
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
          <button
            type="submit"
            className="btn btn-p btn-sm"
            disabled={busy || !name.trim() || name.trim() === org.name}
          >
            {busy ? "Đang lưu…" : "Đổi tên"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </form>
      <div>
        <h3 style={{ fontSize: 14, marginBottom: 6 }}>Nhật ký biến động</h3>
        {events.error ? (
          <ProblemAlert problem={events.error} onRetry={events.reload} />
        ) : null}
        {events.loading && !events.data ? <Loading /> : null}
        <ol style={{ margin: "0 0 0 18px" }}>
          {(events.data?.items ?? []).map((ev) => (
            <li key={ev.seq} className="small" style={{ marginBottom: 6 }}>
              <b style={{ display: "inline" }}>{ACTION[ev.action]}</b> ·{" "}
              {formatDateTime(ev.event_time)} — {ev.name}
              {ev.reason ? (
                <div className="mute">Lý do: {ev.reason}</div>
              ) : null}
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}

/** Xuất cây tổ chức Excel (Admin GET /v1/orgs/export.xlsx) — tải tệp nhị phân, nhập lại được ngay. */
function ExportButton() {
  const { adminApi } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const res = await adminApi.GET("/v1/orgs/export.xlsx", {
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      saveBlob(
        res.data,
        filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "co-cau-to-chuc.xlsx",
        ),
      );
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <button
        type="button"
        className="btn btn-o btn-sm"
        onClick={() => void run()}
        disabled={busy}
      >
        <Icon name="download" size={15} />
        {busy ? "Đang xuất…" : "Xuất Excel"}
      </button>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </>
  );
}

/** Nhập cây tổ chức từ .xlsx/CSV (Admin POST /v1/orgs/import) — kết quả theo dòng. */
function ImportPanel({ onDone }: { onDone: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<OrgImportResult | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setIdemKey(newIdempotencyKey());
  };
  const ok = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/orgs/import", {
          params: {
            header: { "Idempotency-Key": idemKey },
            query: { reason: reason.trim() || undefined },
          },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setResult(res);
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };
  const STATUS = {
    created: "Tạo mới",
    updated: "Cập nhật",
    unchanged: "Không đổi",
    rejected: "Bị loại",
  } as const;
  return (
    <form
      className="panel stack"
      style={{ gap: 8, marginBottom: 16 }}
      onSubmit={(e) => void submit(e)}
    >
      <b>Nhập cây tổ chức (.xlsx hoặc CSV, tối đa 500 dòng)</b>
      <span className="small mute">
        Cùng cột với tệp xuất. Mã đã có → di chuyển/đổi tên (ghi sổ kèm lý do);
        tệp không vô hiệu hoá tổ chức vắng mặt.
      </span>
      <div className="row" style={{ alignItems: "flex-end" }}>
        <input
          type="file"
          aria-label="Tệp cây tổ chức"
          accept=".xlsx,.csv"
          onChange={(e) => void onFile(e)}
        />
        <div className="field" style={{ flex: "1 1 240px" }}>
          <label htmlFor="imp-reason">
            Lý do ghi sổ (≥ 10 ký tự, tuỳ chọn)
          </label>
          <input
            id="imp-reason"
            className="input"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </div>
        <button
          type="submit"
          className="btn btn-p btn-sm"
          disabled={!ok || busy}
        >
          {busy ? "Đang nhập…" : "Nhập"}
        </button>
      </div>
      {file && !ok ? (
        <span className="err small">
          Chỉ nhận .xlsx hoặc CSV UTF-8 (không nhận .xls cũ).
        </span>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <div className="small">
          <p>
            {result.created} tạo mới · {result.updated} cập nhật ·{" "}
            {result.unchanged} không đổi · {result.rejected} bị loại
          </p>
          <ul className="list-plain">
            {result.rows
              .filter((r) => r.status !== "unchanged")
              .map((r) => (
                <li key={r.row}>
                  Dòng {r.row}: {STATUS[r.status]}{" "}
                  {r.code ? <code>{r.code}</code> : null}{" "}
                  {r.changes.length ? `(${r.changes.join(", ")})` : ""}{" "}
                  {r.reason ?? ""}
                </li>
              ))}
          </ul>
        </div>
      ) : null}
    </form>
  );
}

/** Cơ cấu tổ chức (Admin API, MFA): cây GET /v1/orgs (G-08), xuất/nhập Excel, nhật ký, đổi tên. */
function OrgTreeInner() {
  const { adminApi, principal } = useAuth();
  const mayCreate = canCreateOrg(principal?.roles);
  const [creating, setCreating] = useState(false);
  const params = useSearchParams();
  const focusRef = params.get("org");
  const [version, setVersion] = useState(0);
  const orgs = useAsync(
    () =>
      collectPages(
        (cursor) =>
          unwrap(
            adminApi.GET("/v1/orgs", {
              params: { query: { include_inactive: true, cursor, limit: 200 } },
            }),
          ),
        5000,
      ),
    [adminApi, version],
  );
  const tree = useMemo(() => buildOrgTree(orgs.data ?? []), [orgs.data]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [picked, setPicked] = useState<Org | null>(null);
  const [showImport, setShowImport] = useState(false);
  // Nút chọn luôn lấy bản mới nhất trong danh sách (sau tạo/đổi tên); chưa tải xong thì dùng bản vừa chọn.
  const selectedRef = picked?.ref ?? focusRef;
  const selected =
    (selectedRef
      ? (orgs.data ?? []).find((o) => o.ref === selectedRef)
      : undefined) ??
    picked ??
    null;
  const setSelected = setPicked;
  const effective =
    expanded.size === 0 && tree.length > 0
      ? new Set(tree.map((t) => t.org.ref))
      : expanded;

  // Sau khi tạo: tải lại cây, chọn nút mới và mở mọi cấp trên của nó.
  const onCreated = (org: Org) => {
    setCreating(false);
    const byRef = new Map((orgs.data ?? []).map((o) => [o.ref, o]));
    const next = new Set(effective);
    for (let p = org.parent_ref; p; p = byRef.get(p)?.parent_ref ?? null)
      next.add(p);
    setExpanded(next);
    setPicked(org);
    setVersion((v) => v + 1);
  };

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
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() =>
                setExpanded(new Set((orgs.data ?? []).map((o) => o.ref)))
              }
            >
              Mở rộng tất cả
            </button>
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setExpanded(new Set(["__none__"]))}
            >
              Thu gọn
            </button>
            {mayCreate ? (
              <button
                type="button"
                className="btn btn-p btn-sm"
                onClick={() => setCreating(true)}
              >
                Tạo tổ chức
              </button>
            ) : null}
            <ExportButton />
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setShowImport((v) => !v)}
              aria-expanded={showImport}
            >
              <Icon name="upload" size={15} />
              Nhập từ tệp
            </button>
          </>
        }
      />
      <MfaNotice />
      {creating ? (
        <OrgCreateDialog
          orgs={orgs.data ?? []}
          onClose={() => setCreating(false)}
          onCreated={onCreated}
        />
      ) : null}
      {showImport ? (
        <ImportPanel onDone={() => setVersion((v) => v + 1)} />
      ) : null}
      {orgs.error ? (
        <ProblemAlert problem={orgs.error} onRetry={orgs.reload} />
      ) : null}
      {orgs.loading && !orgs.data ? <Loading /> : null}
      <div
        className="grid2"
        style={{
          gridTemplateColumns: "minmax(0, 1.6fr) minmax(280px, 1fr)",
          alignItems: "start",
        }}
      >
        <div className="card sec tree">
          <ul role="tree" aria-label="Cây tổ chức">
            {tree.map((n) => (
              <Node
                key={n.org.ref}
                node={n}
                expanded={effective}
                toggle={toggle}
                selected={selected?.ref ?? null}
                onSelect={setSelected}
              />
            ))}
          </ul>
        </div>
        <aside className="card sec" aria-label="Chi tiết tổ chức">
          {selected ? (
            <OrgPanel
              key={selected.ref}
              org={selected}
              onRenamed={orgs.reload}
            />
          ) : (
            <p className="mute">
              Chọn một tổ chức để xem nhật ký biến động hoặc đổi tên.
            </p>
          )}
        </aside>
      </div>
    </>
  );
}

export default function OrgTreePage() {
  return (
    <Suspense fallback={<Loading />}>
      <OrgTreeInner />
    </Suspense>
  );
}
