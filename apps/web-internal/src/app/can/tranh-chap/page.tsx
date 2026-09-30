"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { Dispute, DisputePage, DisputeStatus } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, Ref, Section, SelectField, STATUS_LABEL, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { formatKg, formatPct, isKg } from "@/lib/kg";
import { labelOf } from "@/lib/recent";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const ACTION_LABEL: Record<string, string> = { opened: "Mở hồ sơ", add_evidence: "Thêm bằng chứng", start_review: "Bắt đầu xem xét", resolve: "Giải quyết", cancel: "Huỷ" };
const RES_LABEL: Record<string, string> = { customer_weight: "Theo số cân khách", mkg_weight: "Theo số cân MKG (HUB_OUT)", agreed_weight: "Theo khối lượng thoả thuận" };
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function DisputeDetail({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const d = useAsync(() => unwrapAs<Dispute>(api.GET("/v1/measurement/disputes/{id}", { params: { path: { id } } })), [api, id]);
  const [evidence, setEvidence] = useState("");
  const [resolution, setResolution] = useState("customer_weight");
  const [agreed, setAgreed] = useState("");
  const [note, setNote] = useState("");
  const act = useAction();
  const canAct = hasRole(principal, ROLES.disputeOfficer);
  if (d.loading) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const x = d.data!;
  const closed = x.status === "resolved" || x.status === "cancelled";
  const run = async (action: "add_evidence" | "start_review" | "resolve" | "cancel") => {
    const refs = evidence.split(/[\s,]+/).filter(Boolean);
    const body =
      action === "add_evidence"
        ? { action, evidence_refs: refs }
        : action === "resolve"
          ? { action, resolution: resolution as "customer_weight", ...(resolution === "agreed_weight" ? { agreed_kg: agreed.trim() } : {}), note: note.trim() }
          : action === "cancel"
            ? { action, note: note.trim() }
            : { action, ...(note.trim() ? { note: note.trim() } : {}) };
    const r = await act.run(() => unwrapAs<Dispute>(api.POST("/v1/measurement/disputes/{id}/actions", { params: { path: { id }, header: idem() }, body })), (y) => `${ACTION_LABEL[action]} — trạng thái ${STATUS_LABEL[y.status] ?? y.status}`);
    if (r) {
      setEvidence("");
      setNote("");
      d.reload();
      onChanged();
    }
  };
  return (
    <div className="stack">
      <dl className="kv">
        <dt>Lô</dt>
        <dd>
          {labelOf("lot", x.lot_ref) ?? ""} <Ref id={x.lot_ref} />
        </dd>
        <dt>Trạng thái · mức</dt>
        <dd className="row">
          <Chip status={x.status} /> <Chip status={x.severity} />
        </dd>
        <dt>HUB_OUT → CUSTOMER_IN</dt>
        <dd>
          {formatKg(x.hub_out_kg, 3)} → {formatKg(x.customer_in_kg, 3)} (chênh {formatKg(x.diff_kg, 3)}, {formatPct(x.diff_pct, true)})
        </dd>
        <dt>Bằng chứng</dt>
        <dd>{x.evidence_refs.length ? x.evidence_refs.map((e) => <Ref key={e} id={e} />) : "Chưa có"}</dd>
        {x.resolution ? (
          <>
            <dt>Giải quyết</dt>
            <dd>
              {RES_LABEL[x.resolution]} — {formatKg(x.agreed_kg, 3)}
            </dd>
          </>
        ) : null}
      </dl>
      <ol className="list-plain small" aria-label="Lịch sử hồ sơ">
        {x.events?.map((e) => (
          <li key={e.seq}>
            {formatDateTime(e.event_time)} · <b>{ACTION_LABEL[e.action] ?? e.action}</b> → {STATUS_LABEL[e.status_after] ?? e.status_after}
            {e.note ? ` — ${e.note}` : ""} · <Ref id={e.actor_ref} />
          </li>
        ))}
      </ol>
      <ActionStatus a={act} />
      {canAct && !closed ? (
        <div className="panel stack">
          <div className="grid2">
            <TextField label="Mã bằng chứng thêm (UUID, cách nhau dấu phẩy)" value={evidence} onChange={setEvidence} />
            <TextField label="Ghi chú" value={note} onChange={setNote} hint="Bắt buộc ≥ 10 ký tự khi giải quyết hoặc huỷ" />
          </div>
          {x.status === "under_review" ? (
            <div className="grid2">
              <SelectField label="Cách giải quyết" value={resolution} onChange={setResolution} options={Object.entries(RES_LABEL).map(([value, label]) => ({ value, label }))} />
              {resolution === "agreed_weight" ? <TextField label="Khối lượng thoả thuận (kg)" value={agreed} onChange={setAgreed} inputMode="decimal" /> : null}
            </div>
          ) : null}
          <div className="row">
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !evidence.split(/[\s,]+/).filter(Boolean).every((e) => UUID.test(e)) || !evidence.trim()} onClick={() => void run("add_evidence")}>
              Thêm bằng chứng
            </button>
            {x.status === "open" ? (
              <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void run("start_review")}>
                Bắt đầu xem xét
              </button>
            ) : null}
            {x.status === "under_review" ? (
              <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !validReason(note) || (resolution === "agreed_weight" && !isKg(agreed))} onClick={() => void run("resolve")}>
                Giải quyết hồ sơ
              </button>
            ) : null}
            <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(note)} onClick={() => void run("cancel")}>
              Huỷ hồ sơ
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function DisputesPage() {
  const { api } = useAuth();
  const [status, setStatus] = useState<"" | DisputeStatus>("");
  const list = useAsync(() => unwrapAs<DisputePage>(api.GET("/v1/measurement/disputes", { params: { query: status ? { status } : {} } })), [api, status]);
  const [sel, setSel] = useState("");
  return (
    <div className="stack">
      <PageHead title="Tranh chấp" subtitle="Hồ sơ mở tự động khi chênh thương mại vượt dung sai. Vòng đời: đang mở → đang xem xét → đã giải quyết | đã huỷ; bằng chứng thêm khi còn mở. Không bao giờ qua van." />
      <Section title="Hồ sơ">
        <div className="stack">
          <SelectField
            label="Lọc trạng thái"
            value={status}
            onChange={(v) => setStatus(v as typeof status)}
            options={[{ value: "", label: "Tất cả" }, ...(["open", "under_review", "resolved", "cancelled"] as const).map((s) => ({ value: s, label: STATUS_LABEL[s] ?? s }))]}
          />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} /> : null}
          {list.data && !list.data.items.length ? <Empty>Không có hồ sơ.</Empty> : null}
          {list.data?.items.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Danh sách tranh chấp">
                <thead>
                  <tr>
                    <th>Mở lúc</th>
                    <th>Lô</th>
                    <th>Mức</th>
                    <th>Trạng thái</th>
                    <th className="num">Chênh</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.items.map((d) => (
                    <tr key={d.id}>
                      <td>{formatDateTime(d.opened_at)}</td>
                      <td>{labelOf("lot", d.lot_ref) ?? <Ref id={d.lot_ref} />}</td>
                      <td>
                        <Chip status={d.severity} />
                      </td>
                      <td>
                        <Chip status={d.status} />
                      </td>
                      <td className="num">
                        {formatKg(d.diff_kg)} ({formatPct(d.diff_pct, true)})
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(d.id)} aria-label={`Mở hồ sơ ${labelOf("lot", d.lot_ref) ?? d.id}`}>
                          Mở hồ sơ
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
      {sel ? (
        <Section title="Chi tiết hồ sơ">
          <DisputeDetail key={sel} id={sel} onChanged={list.reload} />
        </Section>
      ) : null}
    </div>
  );
}
