"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useMemo, useState, type FormEvent } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import type { ExtDirective } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

function AckBar({ d }: { d: ExtDirective }) {
  const pct = d.ack_rate === null ? null : Number(d.ack_rate) * 100;
  return (
    <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
      <div className="bar" role="img" aria-label={`Đã xác nhận ${d.ack_count}/${d.target_count}`} style={{ flex: "1 1 120px" }}>
        <span style={{ width: `${pct === null ? 0 : Math.min(100, pct)}%` }} />
      </div>
      <span className="small" style={{ whiteSpace: "nowrap" }}>
        {formatInt(d.ack_count)}/{formatInt(d.target_count)} · {pct === null ? "—" : `${formatDecimal(pct, 0)}%`}
      </span>
    </div>
  );
}

/** Xác nhận đã đọc (chỉ người nhận) — POST /v1/extension/directives/{id}/ack. */
function AckButton({ d, onDone }: { d: ExtDirective; onDone: () => void }) {
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  if (!d.is_target) return null;
  if (d.acked_at) return <span className="chip c-ok">Đã xác nhận {formatDateTime(d.acked_at)}</span>;
  const ack = async () => {
    setBusy(true);
    try {
      await unwrap(api.POST("/v1/extension/directives/{id}/ack", { params: { path: { id: d.directive_id }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      onDone();
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className="btn btn-p btn-sm" onClick={() => void ack()} disabled={busy}>
        <Icon name="check" size={15} />
        {busy ? "Đang gửi…" : "Xác nhận đã đọc"}
      </button>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </div>
  );
}

function DirectiveDetail({ id, names, onClose, onChanged }: { id: string; names: ReadonlyMap<string, string>; onClose: () => void; onChanged: () => void }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/extension/directives/{id}", { params: { path: { id } } })), [api, id]);
  const dir = d.data?.directive;
  const pendingByUnit = useMemo(() => {
    const m = new Map<string, number>();
    for (const t of d.data?.pending_targets ?? []) if (t.unit_code) m.set(t.unit_code, (m.get(t.unit_code) ?? 0) + 1);
    return Array.from(m.entries()).sort((a, b) => b[1] - a[1]);
  }, [d.data]);
  return (
    <section className="card sec hl" style={{ marginBottom: 16 }} aria-labelledby="dir-detail" data-testid="directive-detail">
      <div className="sec-h">
        <h2 id="dir-detail">{dir?.title ?? "Chỉ đạo"}</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      </div>
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {dir ? (
        <div className="grid2" style={{ alignItems: "start" }}>
          <div className="stack" style={{ gap: 10 }}>
            <p style={{ whiteSpace: "pre-wrap" }}>{dir.body}</p>
            <p className="mute small">
              Ban hành {formatDateTime(dir.issued_at)}
              {dir.expires_at ? ` · hết hạn ${formatDateTime(dir.expires_at)}` : ""} · Vùng nhận: {dir.audience_units.map((u) => names.get(u) ?? u).join(", ")}
            </p>
            <AckBar d={dir} />
            <AckButton
              d={dir}
              onDone={() => {
                d.reload();
                onChanged();
              }}
            />
          </div>
          {d.data?.pending_targets ? (
            <div>
              <h3 style={{ fontSize: 14, marginBottom: 6 }}>Chưa xác nhận theo đơn vị</h3>
              {pendingByUnit.length === 0 ? <p className="mute small">Mọi người nhận đã xác nhận.</p> : null}
              <ul className="list-plain">
                {pendingByUnit.map(([u, n]) => (
                  <li key={u} className="row" style={{ justifyContent: "space-between" }}>
                    <span>{names.get(u) ?? u}</span>
                    <span className="chip c-warn">{n} người</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

/** Ban hành chỉ đạo khẩn (TTKN tỉnh/Quốc gia) — POST /v1/extension/directives. */
function IssueForm({ units, onDone }: { units: { code: string; name: string }[]; onDone: (d: ExtDirective) => void }) {
  const { api } = useAuth();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [severity, setSeverity] = useState<"urgent" | "normal">("urgent");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const touch = () => setIdemKey(newIdempotencyKey());
  const errs = problem ? fieldErrorMap(problem) : {};

  const toggle = (code: string) => {
    touch();
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(code)) n.delete(code);
      else n.add(code);
      return n;
    });
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      const d = await unwrap(
        api.POST("/v1/extension/directives", {
          params: { header: { "Idempotency-Key": idemKey } },
          body: {
            title: title.trim(),
            body: body.trim(),
            severity,
            audience_units: Array.from(picked),
            expires_at: expires ? new Date(`${expires}T23:59:59+07:00`).toISOString() : undefined,
          },
        }),
      );
      setTitle("");
      setBody("");
      setPicked(new Set());
      touch();
      onDone(d);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card sec stack" style={{ gap: 12, marginBottom: 16 }} onSubmit={(e) => void submit(e)} noValidate aria-labelledby="issue-title">
      <h2 id="issue-title" style={{ fontSize: 15.5 }}>
        Ban hành chỉ đạo mới
      </h2>
      {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
      <div className="grid2">
        <div className="field">
          <label htmlFor="d-title">Tiêu đề (5–200 ký tự)</label>
          <input id="d-title" className="input" value={title} maxLength={200} onChange={(e) => (touch(), setTitle(e.target.value))} aria-invalid={!!errs.title} />
          {errs.title ? <span className="err">{errs.title}</span> : null}
        </div>
        <div className="row" style={{ alignItems: "flex-end" }}>
          <div className="field">
            <label htmlFor="d-sev">Mức độ</label>
            <select id="d-sev" className="select" value={severity} onChange={(e) => (touch(), setSeverity(e.target.value === "normal" ? "normal" : "urgent"))}>
              <option value="urgent">Khẩn</option>
              <option value="normal">Thường</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="d-exp">Hết hạn (tuỳ chọn)</label>
            <input id="d-exp" type="date" className="input" value={expires} onChange={(e) => (touch(), setExpires(e.target.value))} />
          </div>
        </div>
      </div>
      <div className="field">
        <label htmlFor="d-body">Nội dung (tối thiểu 10 ký tự)</label>
        <textarea id="d-body" className="input textarea" value={body} onChange={(e) => (touch(), setBody(e.target.value))} aria-invalid={!!errs.body} />
        {errs.body ? <span className="err">{errs.body}</span> : null}
      </div>
      <fieldset className="plain">
        <legend>Vùng nhận ({picked.size} đơn vị)</legend>
        <div className="check-grid">
          {units.map((u) => (
            <label key={u.code} className="row small" style={{ gap: 6 }}>
              <input type="checkbox" checked={picked.has(u.code)} onChange={() => toggle(u.code)} />
              {u.name}
            </label>
          ))}
        </div>
        {errs.audience_units ? <span className="err small">{errs.audience_units}</span> : null}
      </fieldset>
      {problem?.code === "validation_failed" ? (
        <p className="err small" role="alert">
          {problem.message} (mã tra cứu {problem.traceId ?? "—"})
        </p>
      ) : null}
      <div>
        <button type="submit" className="btn btn-p" disabled={busy || picked.size === 0}>
          {busy ? "Đang ban hành…" : "Ban hành chỉ đạo"}
        </button>
      </div>
    </form>
  );
}

/** Chỉ đạo khẩn (KN US-TASK-03): danh sách, tỷ lệ xác nhận, ban hành (TTKN tỉnh/Quốc gia). */
function DirectivesInner() {
  const { api, principal } = useAuth();
  const params = useSearchParams();
  const router = useRouter();
  const focus = params.get("id");
  const [mine, setMine] = useState(false);
  const [version, setVersion] = useState(0);
  const supervisor = hasRole(principal, ROLES.knProvinceAdmin, ROLES.knNationalAdmin, ROLES.platformAdmin);
  const [showForm, setShowForm] = useState(false);

  // Đơn vị được chọn làm vùng nhận: TW → các tỉnh; tỉnh → tỉnh + các xã trực thuộc.
  const province = principal?.level === "tinh" ? (principal.scopeUnits.find((u) => u.length === 2) ?? null) : null;
  const units = useAsync(async () => {
    const children = await listChildUnits(api, province);
    const list = children.map((u) => ({ code: u.code, name: u.name }));
    return province ? [{ code: province, name: `Toàn tỉnh ${principal?.unitName ?? province}` }, ...list] : list;
  }, [api, province]);
  const names = useMemo(() => new Map((units.data ?? []).map((u) => [u.code, u.name])), [units.data]);

  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/extension/directives", { params: { query: { mine: mine || undefined, cursor, limit: 20 } } })),
    [api, mine, version],
  );

  return (
    <>
      <PageHead
        title="Chỉ đạo khẩn"
        subtitle="Chỉ đạo tới đúng vùng đã chọn, theo dõi tỷ lệ cán bộ xác nhận đã đọc"
        actions={
          supervisor ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setShowForm((v) => !v)} aria-expanded={showForm}>
              <Icon name="megaphone" size={15} />
              {showForm ? "Đóng biểu mẫu" : "Ban hành chỉ đạo"}
            </button>
          ) : null
        }
      />
      {supervisor && showForm ? (
        <IssueForm
          units={units.data ?? []}
          onDone={(d) => {
            setShowForm(false);
            setVersion((v) => v + 1);
            router.replace(`/chi-dao?id=${d.directive_id}`);
          }}
        />
      ) : null}
      {focus ? (
        <DirectiveDetail key={`${focus}-${version}`} id={focus} names={names} onClose={() => router.replace("/chi-dao")} onChanged={() => setVersion((v) => v + 1)} />
      ) : null}
      <div className="card sec">
        <div className="sec-h">
          <h2>Danh sách chỉ đạo</h2>
          <label className="row small">
            <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} />
            Chỉ chỉ đạo gửi tới tôi
          </label>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Chỉ đạo khẩn</caption>
            <thead>
              <tr>
                <th scope="col">Chỉ đạo</th>
                <th scope="col">Vùng nhận</th>
                <th scope="col">Ban hành</th>
                <th scope="col" style={{ minWidth: 220 }}>
                  Tỷ lệ xác nhận
                </th>
                <th scope="col">Của tôi</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((d) => (
                <tr key={d.directive_id}>
                  <td style={{ maxWidth: 360 }}>
                    <button type="button" onClick={() => router.replace(`/chi-dao?id=${d.directive_id}`)} style={{ fontWeight: 700, textAlign: "left", color: "var(--g800)" }}>
                      {d.title}
                    </button>
                    <div>{d.severity === "urgent" ? <span className="chip c-bad">Khẩn</span> : <span className="chip c-info">Thường</span>}</div>
                  </td>
                  <td className="small">{d.audience_units.map((u) => names.get(u) ?? u).join(", ")}</td>
                  <td className="small">{formatDateTime(d.issued_at)}</td>
                  <td>
                    <AckBar d={d} />
                  </td>
                  <td>{d.is_target ? <AckButton d={d} onDone={() => setVersion((v) => v + 1)} /> : <span className="mute small">Giám sát</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Chưa có chỉ đạo.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}

export default function DirectivesPage() {
  return (
    <Suspense fallback={<Loading />}>
      <DirectivesInner />
    </Suspense>
  );
}
