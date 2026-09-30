"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import type { CostingSettlement } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { KV, Money, SoD } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Ref, RefPicker, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { BASIS_LABEL, BLOCKED_REASON_LABEL, SETTLEMENT_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg } from "@/lib/kg";
import { addMoney, formatPrice, formatVnd, isMoney, isPrice } from "@/lib/money";
import { shortRef, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const per = (b: string | null | undefined) => (b === "per_bale" ? "cuộn" : "tấn");
const today = () => new Date().toISOString().slice(0, 10);

function SettlementPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, adminApi, principal } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/costing/settlements/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [basis, setBasis] = useState<"per_tonne" | "per_bale">("per_tonne");
  const [price, setPrice] = useState("");
  const [reason, setReason] = useState("");
  const [adjAmount, setAdjAmount] = useState("");
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const s = d.data!;
  const acc = hasRole(principal, ROLES.costAccountant);
  const chief = hasRole(principal, ROLES.chiefAccountant);
  const p = () => ({ path: { id }, header: idem() });
  const done = (x: unknown) => {
    if (x) {
      d.reload();
      onChanged();
      setReason("");
      setPrice("");
      setAdjAmount("");
    }
  };
  const ready = !s.blocked_reason && !!s.amount;
  return (
    <div className="stack">
      <KV
        label="Chi tiết quyết toán HTX"
        items={[
          ["Loại", s.kind === "adjustment" ? <span key="k">Quyết toán điều chỉnh cho <Ref id={s.supersedes_id} /></span> : "Quyết toán gốc (một lệnh gom một quyết toán — PM-01)"],
          ["Trạng thái", <Chip key="s" status={s.status} label={SETTLEMENT_STATUS_LABEL[s.status ?? ""]} />],
          ["Lệnh gom · HTX · ngày gặt", <span key="o"><Ref id={s.collection_order_ref} /> · <Ref id={s.htx_ref} /> · {formatDate(s.harvest_date)}</span>],
          ["Giá", s.basis ? <span key="p">{BASIS_LABEL[s.basis]} · {formatPrice(s.unit_price, per(s.basis))} {s.price_source === "contract" ? <span className="chip c-info">hợp đồng <Ref id={s.contract_ref} /> v{s.contract_version}</span> : <span className="chip c-warn">giá thoả thuận nhập tay (PM-03)</span>}</span> : <span key="p" className="chip c-bad">Chưa có giá — hệ thống không đoán giá</span>],
          ["Số lượng", `${s.rolls ? `${s.rolls} cuộn` : "—"} · ${formatKg(s.mass_kg)}`],
          ["Thành tiền − khấu trừ = phải trả", <span key="a"><Money v={s.gross_amount} /> − <Money v={s.deductions} /> = <Money v={s.amount} strong /></span>],
          ...(s.blocked_reason ? ([["Chưa duyệt được vì", <span key="b" className="chip c-warn">{BLOCKED_REASON_LABEL[s.blocked_reason]}</span>]] as [string, ReactNode][]) : []),
          ["Hạn thanh toán", s.due_on ? <span key="d">{formatDate(s.due_on)} <Provisional what="Q-3" /></span> : "—"],
          ["Người lập → duyệt (R-56)", <SoD key="sd" maker={s.prepared_by} checker={s.approved_by} />],
        ]}
      />
      <ActionStatus a={act} />
      {acc && s.blocked_reason === "no_contract" && s.status === "pending_confirmation" ? (
        <div className="panel stack">
          <b>Nhập giá thoả thuận (không có hợp đồng — PM-03). Người nhập giá trở thành người lập.</b>
          <div className="grid3">
            <SelectField label="Cách tính giá" value={basis} onChange={(b) => setBasis(b as "per_tonne")} options={Object.entries(BASIS_LABEL).map(([value, label]) => ({ value, label }))} />
            <TextField label={`Giá thoả thuận (VND/${per(basis)})`} value={price} onChange={setPrice} inputMode="decimal" />
            <TextField label="Căn cứ giá" value={reason} onChange={setReason} hint="Biên bản / lý do ≥ 10 ký tự" />
          </div>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !isPrice(price.trim()) || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/settlements/{id}/price", { params: p(), body: { basis, unit_price: price.trim(), reason: reason.trim() } })), (r) => `Đã nhập giá — phải trả ${formatVnd(r.amount)}`).then(done)}>
              Ghi giá thoả thuận
            </button>
          </div>
        </div>
      ) : null}
      {s.status === "pending_confirmation" || s.status === "awaiting_weighing" ? (
        <div className="panel stack">
          <b>Duyệt quyết toán — kế toán trưởng KHÁC người lập; ghi đúng MỘT khoản phải trả, đóng băng (PM-04). Bề mặt quản trị (MFA).</b>
          <SelfHint proposer={s.prepared_by} />
          {!ready ? <p className="small mute">Chưa sẵn sàng: {BLOCKED_REASON_LABEL[s.blocked_reason ?? ""] ?? "số tiền ≤ 0"}.</p> : null}
          <div className="row">
            {chief ? (
              <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(adminApi.POST("/v1/costing/settlements/{id}/approve", { params: p() })), (r) => `Đã duyệt quyết toán — phải trả ${formatVnd(r.amount)}, hạn ${formatDate(r.due_on)}`).then(done)}>
                Duyệt quyết toán
              </button>
            ) : null}
            {acc ? (
              <>
                <TextField label="Lý do huỷ quyết toán" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
                <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/settlements/{id}/cancel", { params: p(), body: { reason: reason.trim() } })), "Đã huỷ quyết toán").then(done)}>
                  Huỷ quyết toán
                </button>
              </>
            ) : null}
          </div>
        </div>
      ) : null}
      {s.status === "approved" && (acc || chief) ? (
        <div className="row">
          <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/settlements/{id}/pay", { params: p() })), "Đã ghi đã trả").then(done)}>
            Ghi đã trả
          </button>
        </div>
      ) : null}
      {(s.status === "approved" || s.status === "paid") && acc ? (
        <div className="panel stack">
          <b>Quyết toán điều chỉnh (âm = đảo) — đã duyệt / đã trả không huỷ, đi qua duyệt như gốc</b>
          <div className="grid3">
            <TextField label="Số tiền điều chỉnh (VND)" value={adjAmount} onChange={setAdjAmount} inputMode="decimal" hint="vd -500000 để đảo một phần" />
            <TextField label="Lý do điều chỉnh" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
            <div className="row">
              <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !isMoney(adjAmount.trim()) || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/settlements/{id}/adjust", { params: p(), body: { amount: adjAmount.trim(), reason: reason.trim() } })), "Đã lập quyết toán điều chỉnh — chờ kế toán trưởng khác duyệt").then(done)}>
                Lập quyết toán điều chỉnh
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function CreateSettlement({ onCreated }: { onCreated: (id: string) => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [order, setOrder] = useState("");
  const [htx, setHtx] = useState("");
  const [harvest, setHarvest] = useState(today());
  const [ded, setDed] = useState("");
  const [reason, setReason] = useState("");
  const hasDed = !!ded.trim() && !/^0+(\.0*)?$/.test(ded.trim());
  const err = !UUID.test(order.trim()) ? "Nhập mã lệnh gom (UUID)" : !UUID.test(htx.trim()) ? "Chọn HTX" : ded && !isMoney(ded.trim()) ? "Khấu trừ: tiền VND" : hasDed && !validReason(reason) ? "Có khấu trừ cần lý do ≥ 10 ký tự" : null;
  return (
    <div className="stack">
      <div className="grid2">
        <TextField label="Mã lệnh gom" value={order} onChange={setOrder} hint="UUID lệnh gom (= mã lô gom)" />
        <RefPicker label="HTX" kind="htx" value={htx} onChange={setHtx} />
        <TextField label="Ngày gặt" type="date" value={harvest} onChange={setHarvest} hint="Giá lấy từ hợp đồng hiệu lực tại ngày gặt" />
        <TextField label="Khấu trừ (VND)" value={ded} onChange={setDed} inputMode="decimal" hint="Tuỳ chọn" />
        {hasDed ? <TextField label="Lý do khấu trừ" value={reason} onChange={setReason} /> : null}
      </div>
      {err ? <p className="small mute">{err}</p> : null}
      <ActionStatus a={act} />
      <div className="row">
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={!!err || act.busy}
          onClick={() =>
            void act
              .run(() => unwrap(api.POST("/v1/costing/settlements", { params: { header: idem() }, body: { collection_order_ref: order.trim(), htx_ref: htx.trim(), harvest_date: harvest, ...(hasDed ? { deductions: ded.trim(), reason: reason.trim() } : {}) } })), (r) =>
                r.blocked_reason ? `Đã lập quyết toán — ${BLOCKED_REASON_LABEL[r.blocked_reason]}` : `Đã lập quyết toán — phải trả ${formatVnd(r.amount)}, chờ kế toán trưởng khác duyệt`,
              )
              .then((r) => r?.id && onCreated(r.id))
          }
        >
          Lập quyết toán
        </button>
      </div>
    </div>
  );
}

export default function SettlementsPage() {
  const { api, principal } = useAuth();
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/costing/settlements", { params: { query: { limit: 200, ...(status ? { status: status as "approved" } : {}) } } }))).items ?? [], [api, status]);
  const [sel, setSel] = useState("");
  const [creating, setCreating] = useState(false);
  const items: CostingSettlement[] = list.data ?? [];
  const payable = addMoney(...items.filter((s) => s.status === "approved").map((s) => s.amount));
  return (
    <div className="stack">
      <PageHead
        title="Quyết toán HTX"
        subtitle="Theo cuộn → số cuộn × giá ngay; theo tấn → chờ cân đủ mọi ghe; không hợp đồng → người xác nhận nhập giá (không đoán). Duyệt ghi đúng một khoản phải trả (hạn 15 ngày, tạm) và đóng băng. Khoản phải trả là nợ phải trả, không là chi phí (D03)."
        actions={
          hasRole(principal, ROLES.costAccountant) ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating((v) => !v)}>
              Lập quyết toán
            </button>
          ) : null
        }
      />
      {!status && list.data ? (
        <div className="grid4">
          <KpiTile label="Chờ duyệt" value={String(items.filter((s) => s.status === "pending_confirmation").length)} color="orange" icon="approve" />
          <KpiTile label="Chờ cân đủ ghe" value={String(items.filter((s) => s.status === "awaiting_weighing").length)} color="blue" icon="clock" />
          <KpiTile label="Không hợp đồng — cần nhập giá" value={String(items.filter((s) => s.blocked_reason === "no_contract").length)} color="red" icon="alert" />
          <KpiTile label="Phải trả đã duyệt" value={formatVnd(payable)} color="green" icon="report" />
        </div>
      ) : null}
      {creating ? (
        <Section title="Lập quyết toán (cost_accountant)">
          <CreateSettlement
            onCreated={(id) => {
              setCreating(false);
              setSel(id);
              list.reload();
            }}
          />
        </Section>
      ) : null}
      <Section title="Danh sách quyết toán">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(SETTLEMENT_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !items.length ? <Empty>Không có quyết toán.</Empty> : null}
          {items.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Quyết toán HTX">
                <thead>
                  <tr>
                    <th>Quyết toán</th>
                    <th>HTX</th>
                    <th>Cách tính</th>
                    <th className="num">Số lượng</th>
                    <th className="num">Phải trả</th>
                    <th>Trạng thái</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((s) => (
                    <tr key={s.id} aria-selected={s.id === sel}>
                      <td>
                        <Ref id={s.id} /> {s.kind === "adjustment" ? <span className="chip c-info">điều chỉnh</span> : null}
                      </td>
                      <td>
                        <Ref id={s.htx_ref} />
                      </td>
                      <td>{s.basis ? BASIS_LABEL[s.basis] : <span className="chip c-bad">Không hợp đồng</span>}</td>
                      <td className="num">
                        {s.rolls ? `${s.rolls} cuộn` : ""} {formatKg(s.mass_kg)}
                      </td>
                      <td className="num">
                        <Money v={s.amount} />
                      </td>
                      <td>
                        <Chip status={s.status} label={SETTLEMENT_STATUS_LABEL[s.status ?? ""]} /> {s.blocked_reason ? <span className="chip c-warn">{BLOCKED_REASON_LABEL[s.blocked_reason]?.split(" — ")[0]}</span> : null}
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(s.id!)} aria-label={`Mở quyết toán ${shortRef(s.id)}`}>
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
        <Section title="Chi tiết quyết toán">
          <SettlementPanel key={sel} id={sel} onChanged={list.reload} />
        </Section>
      ) : null}
    </div>
  );
}
