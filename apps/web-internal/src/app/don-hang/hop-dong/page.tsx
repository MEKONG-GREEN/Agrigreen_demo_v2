"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { OrdersContract, OrdersContractInput } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { KV, SoD, When } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Ref, RefPicker, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { BASIS_LABEL, CONTRACT_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg, isKg } from "@/lib/kg";
import { formatPrice, isPrice } from "@/lib/money";
import { shortRef, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
interface Form {
  htx_ref: string;
  basis: "per_tonne" | "per_bale";
  unit_price: string;
  committed_kg: string;
  max_moisture_pct: string;
  valid_from: string;
  valid_to: string;
  note: string;
  reason: string;
}
const today = () => new Date().toISOString().slice(0, 10);
const EMPTY: Form = { htx_ref: "", basis: "per_tonne", unit_price: "", committed_kg: "", max_moisture_pct: "", valid_from: today(), valid_to: today(), note: "", reason: "" };
const per = (b: string | undefined) => (b === "per_bale" ? "cuộn" : "tấn");

function formError(f: Form, revising: boolean): string | null {
  if (!revising && !UUID.test(f.htx_ref.trim())) return "Chọn HTX (UUID)";
  if (!isPrice(f.unit_price.trim())) return "Giá > 0, tối đa 4 số lẻ (CT-02)";
  if (!isKg(f.committed_kg.trim()) || /^0+(\.0*)?$/.test(f.committed_kg.trim())) return "Khối lượng cam kết > 0 (CT-02)";
  if (!f.valid_from || !f.valid_to || f.valid_to < f.valid_from) return "Kết thúc ≥ bắt đầu (CT-02)";
  if (f.max_moisture_pct && !/^\d{1,3}(\.\d{1,2})?$/.test(f.max_moisture_pct.trim())) return "Trần ẩm: phần trăm, 2 số lẻ";
  if (revising && !validReason(f.reason)) return "Sửa hợp đồng cần lý do ≥ 10 ký tự";
  return null;
}
function toBody(f: Form, revising: boolean): OrdersContractInput {
  return {
    ...(revising ? { reason: f.reason.trim() } : { htx_ref: f.htx_ref.trim() }),
    basis: f.basis,
    unit_price: f.unit_price.trim(),
    committed_kg: f.committed_kg.trim(),
    ...(f.max_moisture_pct.trim() ? { max_moisture_pct: f.max_moisture_pct.trim() } : {}),
    valid_from: f.valid_from,
    valid_to: f.valid_to,
    ...(f.note.trim() ? { note: f.note.trim() } : {}),
  };
}

function ContractFields({ f, set, revising, knownHtx }: { f: Form; set: (x: Partial<Form>) => void; revising: boolean; knownHtx: { id: string; label: string }[] }) {
  return (
    <div className="stack">
      {!revising ? <RefPicker label="HTX" kind="htx" value={f.htx_ref} onChange={(htx_ref) => set({ htx_ref })} extra={knownHtx} /> : null}
      <div className="grid3">
        <SelectField label="Cách tính (CT-03)" value={f.basis} onChange={(b) => set({ basis: b as Form["basis"] })} options={Object.entries(BASIS_LABEL).map(([value, label]) => ({ value, label }))} />
        <TextField label={`Đơn giá (VND/${per(f.basis)})`} value={f.unit_price} onChange={(unit_price) => set({ unit_price })} inputMode="decimal" />
        <TextField label="Khối lượng cam kết (kg)" value={f.committed_kg} onChange={(committed_kg) => set({ committed_kg })} inputMode="decimal" />
        <TextField label="Hiệu lực từ" type="date" value={f.valid_from} onChange={(valid_from) => set({ valid_from })} />
        <TextField label="Hiệu lực đến" type="date" value={f.valid_to} onChange={(valid_to) => set({ valid_to })} />
        <TextField label="Trần ẩm (%)" value={f.max_moisture_pct} onChange={(max_moisture_pct) => set({ max_moisture_pct })} inputMode="decimal" hint="Chỉ lưu, chưa cưỡng chế (C-4)" />
      </div>
      <TextField label="Ghi chú" value={f.note} onChange={(note) => set({ note })} />
      {revising ? <TextField label="Lý do sửa hợp đồng" value={f.reason} onChange={(reason) => set({ reason })} hint="Ít nhất 10 ký tự — sửa là phiên bản mới, chờ duyệt" /> : null}
    </div>
  );
}

function ContractPanel({ id, onChanged, knownHtx }: { id: string; onChanged: () => void; knownHtx: { id: string; label: string }[] }) {
  const { api, principal } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/orders/contracts/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const [revise, setRevise] = useState<Form | null>(null);
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const versions = d.data?.versions ?? [];
  const pending = versions.find((v) => v.status === "pending");
  const active = versions.find((v) => v.status === "active");
  const approver = hasRole(principal, ROLES.salesApprover);
  const officer = hasRole(principal, ROLES.salesOfficer);
  const done = (r: unknown) => {
    if (r) {
      d.reload();
      onChanged();
      setReason("");
      setRevise(null);
    }
  };
  const decide = (v: OrdersContract, decision: "approved" | "rejected") =>
    void act
      .run(
        () => unwrap(api.POST("/v1/orders/contracts/{id}/versions/{version}/decision", { params: { path: { id, version: v.version! }, header: idem() }, body: { decision, ...(decision === "rejected" ? { reason: reason.trim() } : {}) } })),
        decision === "approved" ? `Đã duyệt phiên bản ${v.version} — hợp đồng hiệu lực${versions.length > 1 ? ", phiên bản trước bị thay" : ""}` : `Đã từ chối phiên bản ${v.version}`,
      )
      .then(done);
  const reviseErr = revise ? formError(revise, true) : null;
  return (
    <div className="stack">
      <ActionStatus a={act} />
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Phiên bản hợp đồng">
          <thead>
            <tr>
              <th>Phiên bản</th>
              <th>Trạng thái</th>
              <th>Cách tính</th>
              <th className="num">Đơn giá</th>
              <th className="num">Cam kết</th>
              <th>Hiệu lực</th>
              <th>Trần ẩm</th>
              <th>Người lập → duyệt (R-56)</th>
              <th>Lý do</th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version}>
                <td className="mono">
                  {v.code} v{v.version}
                </td>
                <td>
                  <Chip status={v.status} label={CONTRACT_STATUS_LABEL[v.status ?? ""]} />
                </td>
                <td>{BASIS_LABEL[v.basis ?? ""]}</td>
                <td className="num">{formatPrice(v.unit_price, per(v.basis))}</td>
                <td className="num">{formatKg(v.committed_kg)}</td>
                <td>
                  {formatDate(v.valid_from)} → {formatDate(v.valid_to)}
                </td>
                <td>{v.max_moisture_pct ? `${v.max_moisture_pct} % (chỉ lưu)` : "—"}</td>
                <td>
                  <SoD maker={v.proposed_by} checker={v.approved_by} />
                </td>
                <td className="small">{v.reason ?? v.note ?? ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {pending ? (
        <div className="panel stack">
          <b>Phiên bản {pending.version} chờ duyệt — sales_approver KHÁC người lập (R-56)</b>
          <SelfHint proposer={pending.proposed_by} />
          {approver ? (
            <>
              <TextField label="Lý do (từ chối / chấm dứt)" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
              <div className="row">
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => decide(pending, "approved")}>
                  Duyệt hợp đồng
                </button>
                <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => decide(pending, "rejected")}>
                  Từ chối hợp đồng
                </button>
              </div>
            </>
          ) : (
            <p className="small mute">Bạn không có vai duyệt hợp đồng (sales_approver).</p>
          )}
        </div>
      ) : null}
      {active && !pending ? (
        <div className="panel stack">
          <div className="row">
            {officer ? (
              <button
                type="button"
                className="btn btn-o btn-sm"
                onClick={() => setRevise({ htx_ref: active.htx_ref ?? "", basis: (active.basis ?? "per_tonne") as Form["basis"], unit_price: active.unit_price ?? "", committed_kg: active.committed_kg ?? "", max_moisture_pct: active.max_moisture_pct ?? "", valid_from: active.valid_from ?? today(), valid_to: active.valid_to ?? today(), note: active.note ?? "", reason: "" })}
              >
                Sửa hợp đồng (phiên bản mới)
              </button>
            ) : null}
            {approver ? (
              <>
                <TextField label="Lý do chấm dứt" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
                <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/contracts/{id}/terminate", { params: { path: { id }, header: idem() }, body: { reason: reason.trim() } })), "Đã chấm dứt hợp đồng").then(done)}>
                  Chấm dứt hợp đồng
                </button>
              </>
            ) : null}
          </div>
          {revise ? (
            <>
              <ContractFields f={revise} set={(x) => setRevise({ ...revise, ...x })} revising knownHtx={knownHtx} />
              {reviseErr ? <p className="small mute">{reviseErr}</p> : null}
              <div className="row">
                <button type="button" className="btn btn-p btn-sm" disabled={!!reviseErr || act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/contracts/{id}/revisions", { params: { path: { id }, header: idem() }, body: toBody(revise, true) })), "Đã gửi phiên bản sửa — chờ người khác duyệt").then(done)}>
                  Gửi phiên bản sửa
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setRevise(null)}>
                  Huỷ sửa
                </button>
              </div>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function TermsLookup({ knownHtx }: { knownHtx: { id: string; label: string }[] }) {
  const { api } = useAuth();
  const [htx, setHtx] = useState("");
  const [on, setOn] = useState(today());
  const act = useAction();
  const [terms, setTerms] = useState<Awaited<ReturnType<typeof lookup>> | null>(null);
  const lookup = () => unwrap(api.GET("/v1/orders/contract-terms", { params: { query: { htx_ref: htx.trim(), on } } }));
  return (
    <div className="stack">
      <div className="grid2">
        <RefPicker label="HTX cần tra" kind="htx" value={htx} onChange={setHtx} extra={knownHtx} />
        <TextField label="Tại ngày" type="date" value={on} onChange={setOn} />
      </div>
      <div className="row">
        <button
          type="button"
          className="btn btn-o btn-sm"
          disabled={!UUID.test(htx.trim()) || act.busy}
          onClick={() => {
            setTerms(null);
            void act.run(lookup).then((r) => r && setTerms(r));
          }}
        >
          Tra điều khoản hiệu lực
        </button>
      </div>
      <ActionStatus a={act} />
      {terms ? (
        <KV
          label="Điều khoản hiệu lực"
          items={[
            ["Hợp đồng", `${terms.code} v${terms.version}`],
            ["Cách tính", BASIS_LABEL[terms.basis ?? ""] ?? "—"],
            ["Đơn giá", formatPrice(terms.unit_price, per(terms.basis))],
            ["Hiệu lực", `${formatDate(terms.valid_from)} → ${formatDate(terms.valid_to)}`],
          ]}
        />
      ) : null}
    </div>
  );
}

export default function ContractsPage() {
  const { api, principal } = useAuth();
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/orders/contracts", { params: { query: { limit: 200, ...(status ? { status: status as "active" } : {}) } } }))).items ?? [], [api, status]);
  const [sel, setSel] = useState("");
  const [form, setForm] = useState<Form | null>(null);
  const act = useAction();
  const knownHtx = Array.from(new Set((list.data ?? []).map((c) => c.htx_ref!))).map((h) => ({ id: h, label: `HTX ${shortRef(h)}` }));
  const err = form ? formError(form, false) : null;
  return (
    <div className="stack">
      <PageHead
        title="Hợp đồng mua rơm"
        subtitle="Một hợp đồng hiệu lực mỗi HTX mỗi thời điểm (CT-01, chồng lấn → từ chối). Giá > 0, cam kết > 0, kết thúc ≥ bắt đầu (CT-02); theo tấn nhà máy cân hoặc theo cuộn (CT-03). Người lập ≠ người duyệt (R-56)."
        actions={
          hasRole(principal, ROLES.salesOfficer) ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setForm(EMPTY)}>
              Lập hợp đồng
            </button>
          ) : null
        }
      />
      <ActionStatus a={act} />
      {form ? (
        <Section title="Lập hợp đồng (phiên bản 1, chờ duyệt)">
          <ContractFields f={form} set={(x) => setForm({ ...form, ...x })} revising={false} knownHtx={knownHtx} />
          {err ? <p className="small mute">{err}</p> : null}
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={!!err || act.busy}
              onClick={() =>
                void act.run(() => unwrap(api.POST("/v1/orders/contracts", { params: { header: idem() }, body: toBody(form, false) })), (r) => `Đã lập hợp đồng ${r.versions?.[0]?.code ?? ""} — chờ người khác duyệt`).then((r) => {
                  if (r?.contract_id) {
                    setForm(null);
                    setSel(r.contract_id);
                    list.reload();
                  }
                })
              }
            >
              Gửi hợp đồng
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setForm(null)}>
              Huỷ
            </button>
          </div>
        </Section>
      ) : null}
      <Section title="Danh sách hợp đồng (phiên bản mới nhất)">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(CONTRACT_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !list.data.length ? <Empty>Không có hợp đồng.</Empty> : null}
          {list.data?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Hợp đồng mua rơm">
                <thead>
                  <tr>
                    <th>Mã</th>
                    <th>HTX</th>
                    <th>Cách tính</th>
                    <th className="num">Đơn giá</th>
                    <th>Hiệu lực</th>
                    <th>Trạng thái</th>
                    <th>Lập lúc</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.map((c) => (
                    <tr key={c.contract_id} aria-selected={c.contract_id === sel}>
                      <td className="mono">
                        {c.code} v{c.version}
                      </td>
                      <td>
                        <Ref id={c.htx_ref} />
                      </td>
                      <td>{BASIS_LABEL[c.basis ?? ""]}</td>
                      <td className="num">{formatPrice(c.unit_price, per(c.basis))}</td>
                      <td>
                        {formatDate(c.valid_from)} → {formatDate(c.valid_to)}
                      </td>
                      <td>
                        <Chip status={c.status} label={CONTRACT_STATUS_LABEL[c.status ?? ""]} />
                      </td>
                      <td>
                        <When at={c.event_time} />
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(c.contract_id!)} aria-label={`Mở hợp đồng ${c.code}`}>
                          Mở
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
        <Section title="Chi tiết hợp đồng">
          <ContractPanel key={sel} id={sel} onChanged={list.reload} knownHtx={knownHtx} />
        </Section>
      ) : null}
      <Section title="Tra điều khoản hợp đồng (Contracts.ContractFor)">
        <p className="small mute">
          Không có hợp đồng hiệu lực → <code>orders.no_contract</code>: quyết toán không đoán giá, người xác nhận nhập giá thoả thuận (PM-03). Trần ẩm chỉ lưu <Provisional what="C-4" />
        </p>
        <TermsLookup knownHtx={knownHtx} />
      </Section>
    </div>
  );
}
