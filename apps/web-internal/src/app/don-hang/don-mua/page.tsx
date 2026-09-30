"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import type { MkgFile, OrdersPurchaseOrder, OrdersReceipt } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { FileRef, FileUploadField } from "@/components/FileUpload";
import { KV, Money, SoD, When } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Ref, RefPicker, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { PO_CATEGORY_LABEL, PO_STATUS_LABEL, QTY_SOURCE_LABEL, RECEIPT_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg, formatPct, isKg } from "@/lib/kg";
import { formatPrice, isPrice } from "@/lib/money";
import { shortRef, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const qtyText = (unit: string | undefined, q: string | null | undefined) => (q === null || q === undefined ? "—" : unit === "bale" ? `${q} cuộn` : formatKg(q));
const per = (unit: string | undefined) => (unit === "bale" ? "cuộn" : "tấn");

function ReceiptCard({ r, po, onChanged }: { r: OrdersReceipt; po: OrdersPurchaseOrder; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const act = useAction();
  const [note, setNote] = useState("");
  const [file, setFile] = useState<MkgFile | null>(null);
  const [qty, setQty] = useState("");
  const [reason, setReason] = useState("");
  const officer = hasRole(principal, ROLES.salesOfficer);
  const approver = hasRole(principal, ROLES.salesApprover);
  const p = () => ({ path: { id: r.receipt_id! }, header: idem() });
  const done = (x: unknown) => {
    if (x) {
      setNote("");
      setReason("");
      setQty("");
      setFile(null);
      onChanged();
    }
  };
  const open = r.status === "awaiting_weigh" || r.status === "pending_approval";
  return (
    <div className="panel stack" aria-label={`Phiếu nhận ${r.code}`}>
      <div className="row">
        <b className="mono">{r.code}</b> <Chip status={r.status} label={RECEIPT_STATUS_LABEL[r.status ?? ""]} />
        {r.needs_variance_report ? <span className="chip c-warn">Cần biên bản chênh lệch</span> : null}
        {r.report_note ? <span className="chip c-ok">Đã có biên bản</span> : null}
      </div>
      <KV
        items={[
          ["Lô / phiếu cân HUB_IN", <span key="l"><Ref id={r.lot_ref} /> · <Ref id={r.ticket_ref} /></span>],
          ["Đặt → nhận", `${qtyText(r.unit, r.ordered_qty)} → ${qtyText(r.unit, r.received_qty)}${r.qty_source ? ` (${QTY_SOURCE_LABEL[r.qty_source]})` : ""}`],
          ["Chênh lệch ↔ đơn mua", r.variance_pct ? <span key="v">{formatPct(r.variance_pct, true)} · ngưỡng {r.threshold_pct} % <span className="mono small">{r.param_version}</span> <Provisional what="Q-5" /></span> : "—"],
          ...(r.audit_note ? ([["Ghi chú kiểm toán", r.audit_note]] as [string, string][]) : []),
          ...(r.report_note ? ([["Biên bản chênh lệch", <span key="rn" className="stack" style={{ gap: 4 }}><span>{r.report_note}</span>{r.report_file_ref ? <FileRef module="orders" id={r.report_file_ref} /> : null}</span>]] as [string, ReactNode][]) : []),
          ["Người lập → duyệt", <SoD key="s" maker={r.created_by} checker={r.approved_by} />],
          ...(r.payable ? ([["Khoản phải trả", <span key="p"><Money v={r.payable.amount} strong /> · hạn {formatDate(r.payable.due_date)} ({r.payable.payment_term_days} ngày)</span>]] as [string, ReactNode][]) : []),
          ...(po.category === "straw" && r.status === "approved" ? ([["Phải trả", "Rơm: không ghi ở đây — chỉ từ quyết toán HTX đã duyệt (một nguồn khoản phải trả)"]] as [string, string][]) : []),
        ]}
      />
      {r.needs_variance_report ? (
        <div className="alert alert-warn" role="status">
          <span>Lệch đơn mua vượt ngưỡng — cần biên bản chênh lệch. Chỉ cảnh báo, KHÔNG chặn duyệt.</span>
        </div>
      ) : null}
      <ActionStatus a={act} />
      {officer && r.status !== "rejected" && r.needs_variance_report ? (
        <div className="grid3">
          <TextField label={`Biên bản chênh lệch ${r.code}`} value={note} onChange={setNote} multiline hint="Ít nhất 10 ký tự" />
          <FileUploadField label={`Tệp biên bản ${r.code}`} module="orders" purpose="receipt_variance_report" value={file} onUploaded={setFile} hint="Tuỳ chọn — PDF / ảnh ≤ 15 MB, tải lên kho chứng từ trước khi ghi biên bản" />
          <div className="row">
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy || !validReason(note)}
              onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/receipts/{id}/variance-report", { params: p(), body: { note: note.trim(), ...(file ? { file_ref: file.id } : {}) } })), file ? `Đã ghi biên bản chênh lệch kèm tệp ${file.file_name}` : "Đã ghi biên bản chênh lệch").then(done)}
            >
              Ghi biên bản
            </button>
          </div>
        </div>
      ) : null}
      {officer && open && r.qty_source && r.qty_source !== "ticket" ? (
        <div className="grid3">
          <TextField label={`Số nhận bù ${r.code}`} value={qty} onChange={setQty} inputMode="decimal" />
          <TextField label={`Lý do bù ${r.code}`} value={reason} onChange={setReason} hint="Bản bù supersedes, có lý do (RB-1)" />
          <div className="row">
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !qty.trim() || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/receipts/{id}/corrections", { params: p(), body: { received_qty: qty.trim(), reason: reason.trim() } })), "Đã ghi bản bù số nhận").then(done)}>
              Ghi bản bù
            </button>
          </div>
        </div>
      ) : null}
      {approver && open ? (
        <div className="stack">
          <SelfHint proposer={r.created_by} />
          <TextField label={`Lý do từ chối ${r.code}`} value={reason} onChange={setReason} hint="Bắt buộc khi từ chối" />
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/receipts/{id}/decision", { params: p(), body: { decision: "approved" } })), po.category === "other" ? "Đã duyệt phiếu nhận — ghi một khoản phải trả" : "Đã duyệt phiếu nhận").then(done)}>
              Duyệt phiếu nhận {r.code}
            </button>
            <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/receipts/{id}/decision", { params: p(), body: { decision: "rejected", reason: reason.trim() } })), "Đã từ chối phiếu nhận").then(done)}>
              Từ chối
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function PoPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/orders/purchase-orders/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const [lot, setLot] = useState("");
  const [qty, setQty] = useState("");
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const po = d.data!.purchase_order!;
  const officer = hasRole(principal, ROLES.salesOfficer);
  const approver = hasRole(principal, ROLES.salesApprover);
  const p = () => ({ path: { id }, header: idem() });
  const done = (x: unknown) => {
    if (x) {
      d.reload();
      onChanged();
      setReason("");
      setLot("");
      setQty("");
    }
  };
  const byTicket = po.category === "straw" && po.unit === "kg";
  return (
    <div className="stack">
      <KV
        label="Thông tin đơn mua"
        items={[
          ["Mã", <b key="c" className="mono">{po.code}</b>],
          ["Trạng thái", <Chip key="s" status={po.status} label={PO_STATUS_LABEL[po.status ?? ""]} />],
          ["Loại / bên bán", <span key="b">{PO_CATEGORY_LABEL[po.category ?? ""]} · <Ref id={po.supplier_ref} /></span>],
          ["Số lượng", qtyText(po.unit, po.ordered_qty)],
          ["Đơn giá", <span key="p">{formatPrice(po.unit_price, per(po.unit))} {po.price_source === "contract" ? <span className="chip c-info">theo hợp đồng <Ref id={po.contract_ref} /> v{po.contract_version}</span> : <span className="chip c-mute">nhập tay</span>}</span>],
          ["Ngày nhận dự kiến", formatDate(po.expected_date)],
          ["Diễn giải", po.description ?? "—"],
          ["Người lập → duyệt", <SoD key="sd" maker={po.created_by} checker={po.approved_by} />],
          ...(po.reason ? ([["Lý do", po.reason]] as [string, string][]) : []),
        ]}
      />
      <ActionStatus a={act} />
      {po.status === "draft" ? (
        <div className="panel stack">
          <SelfHint proposer={po.created_by} />
          {approver ? (
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/purchase-orders/{id}/decision", { params: p(), body: { decision: "approved" } })), `Đã duyệt đơn mua ${po.code}`).then(done)}>
                Duyệt đơn mua
              </button>
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/purchase-orders/{id}/decision", { params: p(), body: { decision: "rejected", reason: reason.trim() } })), "Đã từ chối đơn mua").then(done)}>
                Từ chối đơn mua
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      {(po.status === "draft" || po.status === "approved") && (officer || approver) ? (
        <div className="row">
          <TextField label="Lý do (từ chối / huỷ đơn)" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/purchase-orders/{id}/cancel", { params: p(), body: { reason: reason.trim() } })), "Đã huỷ đơn mua").then(done)}>
            Huỷ đơn mua
          </button>
        </div>
      ) : null}
      {po.status === "approved" && officer ? (
        <div className="panel stack">
          <b>Lập phiếu nhận hàng</b>
          {byTicket ? (
            <>
              <p className="small mute">Rơm theo kg: số nhận = tịnh phiếu cân HUB_IN của lô (measurement), không nhập tay. Phiếu chưa về → chờ, tự gắn khi về.</p>
              <RefPicker label="Lô nhận" kind="lot" value={lot} onChange={setLot} />
            </>
          ) : (
            <TextField label={po.unit === "bale" ? "Số cuộn đếm được" : "Số nhận (kg)"} value={qty} onChange={setQty} inputMode="decimal" />
          )}
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || (byTicket ? !UUID.test(lot.trim()) : po.unit === "bale" ? !/^\d+$/.test(qty.trim()) : !isKg(qty.trim()))}
              onClick={() =>
                void act
                  .run(() => unwrap(api.POST("/v1/orders/purchase-orders/{id}/receipts", { params: p(), body: byTicket ? { lot_ref: lot.trim() } : { received_qty: qty.trim() } })), (r) =>
                    r.receipt?.needs_variance_report ? `Đã lập phiếu nhận ${r.receipt.code} — lệch ${r.receipt.variance_pct} % vượt ngưỡng, cần biên bản chênh lệch (không chặn)` : `Đã lập phiếu nhận ${r.receipt?.code ?? ""}`,
                  )
                  .then(done)
              }
            >
              Lập phiếu nhận
            </button>
          </div>
        </div>
      ) : null}
      <h3>Phiếu nhận hàng</h3>
      {po.receipts?.length ? po.receipts.map((r) => <ReceiptCard key={r.receipt_id} r={r} po={po} onChanged={() => done(true)} />) : <Empty>Chưa có phiếu nhận.</Empty>}
    </div>
  );
}

function CreatePo({ onCreated }: { onCreated: (id: string) => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [category, setCategory] = useState<"straw" | "other">("straw");
  const [supplier, setSupplier] = useState("");
  const [unit, setUnit] = useState<"" | "kg" | "bale">("");
  const [qty, setQty] = useState("");
  const [price, setPrice] = useState("");
  const [expected, setExpected] = useState("");
  const [desc, setDesc] = useState("");
  const err = !UUID.test(supplier.trim()) ? "Chọn HTX / nhà cung cấp (UUID)" : !qty.trim() ? "Nhập số lượng" : price && !isPrice(price.trim()) ? "Đơn giá > 0" : category === "other" && !price ? "Hàng khác: bắt buộc đơn giá" : null;
  return (
    <div className="stack">
      <div className="grid3">
        <SelectField label="Loại đơn mua" value={category} onChange={(c) => setCategory(c as "straw")} options={Object.entries(PO_CATEGORY_LABEL).map(([value, label]) => ({ value, label }))} />
        <SelectField label="Đơn vị" value={unit} onChange={(u) => setUnit(u as "kg")} options={[{ value: "", label: "Theo hợp đồng (rơm)" }, { value: "kg", label: "kg" }, { value: "bale", label: "cuộn" }]} />
        <TextField label="Số lượng đặt" value={qty} onChange={setQty} inputMode="decimal" hint="kg (3 số lẻ) hoặc số cuộn nguyên" />
        <TextField label="Đơn giá (VND/tấn hoặc /cuộn)" value={price} onChange={setPrice} inputMode="decimal" hint="Rơm có hợp đồng: bỏ trống = giá hợp đồng; không hợp đồng → bắt buộc" />
        <TextField label="Ngày nhận dự kiến" type="date" value={expected} onChange={setExpected} />
        <TextField label="Diễn giải" value={desc} onChange={setDesc} />
      </div>
      <RefPicker label="Bên bán (HTX / nhà cung cấp)" kind="htx" value={supplier} onChange={setSupplier} />
      {err ? <p className="small mute">{err}</p> : null}
      <ActionStatus a={act} />
      <div className="row">
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={!!err || act.busy}
          onClick={() =>
            void act
              .run(
                () =>
                  unwrap(
                    api.POST("/v1/orders/purchase-orders", {
                      params: { header: idem() },
                      body: { category, supplier_ref: supplier.trim(), ordered_qty: qty.trim(), ...(unit ? { unit } : {}), ...(price.trim() ? { unit_price: price.trim() } : {}), ...(expected ? { expected_date: expected } : {}), ...(desc.trim() ? { description: desc.trim() } : {}) },
                    }),
                  ),
                (r) => `Đã lập đơn mua ${r.purchase_order?.code ?? ""} (nháp) — chờ người khác duyệt`,
              )
              .then((r) => r?.purchase_order?.po_id && onCreated(r.purchase_order.po_id))
          }
        >
          Lập đơn mua
        </button>
      </div>
    </div>
  );
}

export default function PurchaseOrdersPage() {
  const { api, principal } = useAuth();
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/orders/purchase-orders", { params: { query: { limit: 200, ...(status ? { status: status as "draft" } : {}) } } }))).items ?? [], [api, status]);
  const payables = useAsync(async () => (await unwrap(api.GET("/v1/orders/payables", { params: { query: { limit: 200 } } }))).items ?? [], [api]);
  const [sel, setSel] = useState("");
  const [creating, setCreating] = useState(false);
  const reload = () => {
    list.reload();
    payables.reload();
  };
  return (
    <div className="stack">
      <PageHead
        title="Đơn mua & phiếu nhận"
        subtitle="Đơn mua nháp → duyệt (người khác, R-56). Phiếu nhận ↔ đơn mua lệch > ngưỡng (3 %, tạm chờ khách) → cần biên bản chênh lệch + ghi chú kiểm toán, không chặn. Phải trả rơm chỉ từ quyết toán HTX; hàng khác từ phiếu nhận đã duyệt."
        actions={
          hasRole(principal, ROLES.salesOfficer) ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating((v) => !v)}>
              Lập đơn mua
            </button>
          ) : null
        }
      />
      {creating ? (
        <Section title="Lập đơn mua">
          <CreatePo
            onCreated={(id) => {
              setCreating(false);
              setSel(id);
              reload();
            }}
          />
        </Section>
      ) : null}
      <Section title="Danh sách đơn mua">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(PO_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !list.data.length ? <Empty>Không có đơn mua.</Empty> : null}
          {list.data?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Đơn mua">
                <thead>
                  <tr>
                    <th>Mã</th>
                    <th>Loại</th>
                    <th>Bên bán</th>
                    <th className="num">Số lượng</th>
                    <th className="num">Đơn giá</th>
                    <th>Trạng thái</th>
                    <th>Phiếu nhận</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.map((po) => (
                    <tr key={po.po_id} aria-selected={po.po_id === sel}>
                      <td className="mono">{po.code}</td>
                      <td>{PO_CATEGORY_LABEL[po.category ?? ""]}</td>
                      <td>
                        <Ref id={po.supplier_ref} />
                      </td>
                      <td className="num">{qtyText(po.unit, po.ordered_qty)}</td>
                      <td className="num">{formatPrice(po.unit_price, per(po.unit))}</td>
                      <td>
                        <Chip status={po.status} label={PO_STATUS_LABEL[po.status ?? ""]} />
                      </td>
                      <td>
                        {po.receipts?.length ?? 0}
                        {po.receipts?.some((r) => r.needs_variance_report) ? <span className="chip c-warn"> lệch &gt; ngưỡng</span> : null}
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(po.po_id!)} aria-label={`Mở đơn mua ${po.code}`}>
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
        <Section title="Chi tiết đơn mua">
          <PoPanel key={sel} id={sel} onChanged={reload} />
        </Section>
      ) : null}
      <Section title="Khoản phải trả đơn mua khác (một khoản mỗi phiếu nhận đã duyệt)">
        {payables.data && !payables.data.length ? <Empty>Chưa có khoản phải trả.</Empty> : null}
        {payables.data?.length ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Khoản phải trả">
              <thead>
                <tr>
                  <th>Phiếu nhận</th>
                  <th>Bên bán</th>
                  <th className="num">Số lượng</th>
                  <th className="num">Thành tiền</th>
                  <th>Hạn trả</th>
                  <th>Tham số</th>
                  <th>Ghi lúc</th>
                </tr>
              </thead>
              <tbody>
                {payables.data.map((x) => (
                  <tr key={x.payable_id}>
                    <td>
                      <Ref id={x.receipt_id} />
                    </td>
                    <td>
                      <Ref id={x.supplier_ref} label={shortRef(x.supplier_ref)} />
                    </td>
                    <td className="num">{qtyText(x.unit, x.qty)}</td>
                    <td className="num">
                      <Money v={x.amount} />
                    </td>
                    <td>{formatDate(x.due_date)}</td>
                    <td className="mono small">{x.param_version}</td>
                    <td>
                      <When at={x.recorded_at} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Section>
    </div>
  );
}
