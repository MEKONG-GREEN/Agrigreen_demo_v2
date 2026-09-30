"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import type { MkgFile, OrdersDelivery, OrdersSalesOrder, Product } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { FileRef, FileUploadField } from "@/components/FileUpload";
import { KV, Money, SoD, When } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Ref, RefPicker, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { canSalesAdvance, DELIVERY_STATUS_LABEL, EVIDENCE_KIND_LABEL, SO_STATUS_LABEL } from "@/lib/g4labels";
import { formatKg, isKg } from "@/lib/kg";
import { amountForKg, formatPrice, isPrice, netPrice } from "@/lib/money";
import { remember } from "@/lib/recent";
import { PRODUCT_LABEL, shortRef, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const PCT = /^\d{1,2}(\.\d{1,2})?$/;
const today = () => new Date().toISOString().slice(0, 10);

function DeliveryRow({ d, onChanged }: { d: OrdersDelivery; onChanged: () => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [file, setFile] = useState<MkgFile | null>(null);
  const [kind, setKind] = useState<"epod" | "minutes" | "ebill">("minutes");
  const [dispute, setDispute] = useState("");
  const [trip, setTrip] = useState("");
  const [reason, setReason] = useState("");
  const [ticket, setTicket] = useState("");
  const p = () => ({ path: { id: d.delivery_id! }, header: idem() });
  const done = (x: unknown) => {
    if (x) {
      setReason("");
      onChanged();
    }
  };
  return (
    <div className="panel stack" aria-label={`Giao nhận ${d.code}`}>
      <div className="row">
        <b className="mono">{d.code}</b> <Chip status={d.status} label={DELIVERY_STATUS_LABEL[d.status ?? ""]} /> <span className="small">tuyến {d.route}</span>
      </div>
      <KV
        items={[
          ["Lô · chuyến", <span key="l"><Ref id={d.lot_ref} /> · <Ref id={d.trip_ref} /></span>],
          ["Ngày giao · kg kế hoạch", `${formatDate(d.planned_date)} · ${formatKg(d.planned_kg)}`],
          ["Phiếu CUSTOMER_IN", d.customer_in_ticket_ref ? <span key="t"><Ref id={d.customer_in_ticket_ref} /> · {formatKg(d.ticket_kg)}</span> : <span key="t" className="mute">Chưa có phiếu cân tại khách — chưa chấp nhận được</span>],
          ["Chấp nhận", d.accepted_kg ? <span key="a">{formatKg(d.accepted_kg)} × {formatPrice(d.unit_price, "tấn")} = <Money v={d.amount} strong /> · <When at={d.accepted_at} /></span> : "—"],
          ...(d.evidence_file_ref ? ([["Chứng từ", <span key="e" className="row" style={{ gap: 6, flexWrap: "wrap" }}>{EVIDENCE_KIND_LABEL[d.evidence_kind ?? ""]} <FileRef module="orders" id={d.evidence_file_ref} /> {d.dispute_ref ? <>· tranh chấp <Ref id={d.dispute_ref} /></> : null}</span>]] as [string, ReactNode][]) : []),
          ...(d.reason ? ([["Lý do", d.reason]] as [string, string][]) : []),
        ]}
      />
      <ActionStatus a={act} />
      {d.status === "planned" ? (
        <div className="grid3">
          <TextField label={`Mã chuyến ${d.code}`} value={trip} onChange={setTrip} hint="UUID chuyến vận tải (tuỳ chọn)" />
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || (!!trip && !UUID.test(trip.trim()))} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/deliveries/{id}/dispatch", { params: p(), body: trip.trim() ? { trip_ref: trip.trim() } : {} })), `Đã xuất giao ${d.code}`).then(done)}>
              Xuất giao {d.code}
            </button>
          </div>
        </div>
      ) : null}
      {d.status === "dispatched" ? (
        <div className="stack">
          <p className="small mute">Khối lượng chấp nhận = phiếu cân CUSTOMER_IN đã gắn (measurement), không nhập tay. Tranh chấp liên kết hồ sơ tranh chấp của bộ phận cân.</p>
          <div className="grid3">
            <FileUploadField label={`Tệp chứng từ ${d.code}`} module="orders" purpose="delivery_evidence" value={file} onUploaded={setFile} hint="ePOD / biên bản / e-bill — PDF hoặc ảnh ≤ 15 MB" />
            <SelectField label={`Loại chứng từ ${d.code}`} value={kind} onChange={(k) => setKind(k as "epod")} options={Object.entries(EVIDENCE_KIND_LABEL).map(([value, label]) => ({ value, label }))} />
            <TextField label={`Hồ sơ tranh chấp ${d.code}`} value={dispute} onChange={setDispute} hint="UUID (tuỳ chọn)" />
          </div>
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || !file || (!!dispute && !UUID.test(dispute.trim()))}
              onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/deliveries/{id}/accept", { params: p(), body: { evidence: { file_ref: file!.id, kind }, ...(dispute.trim() ? { dispute_ref: dispute.trim() } : {}) } })), (r) => `Đã chấp nhận ${d.code}: ${formatKg(r.delivery?.accepted_kg)} — phát mkg.orders.delivery.accepted`).then(done)}
            >
              Chấp nhận giao nhận {d.code}
            </button>
          </div>
        </div>
      ) : null}
      {d.status === "accepted" ? (
        <div className="grid3">
          <TextField label={`Phiếu CUSTOMER_IN khác ${d.code}`} value={ticket} onChange={setTicket} hint="UUID (tuỳ chọn) — bản bù supersedes" />
          <TextField label={`Lý do bù ${d.code}`} value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          <div className="row">
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !validReason(reason) || (!!ticket && !UUID.test(ticket.trim()))} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/deliveries/{id}/corrections", { params: p(), body: { reason: reason.trim(), ...(ticket.trim() ? { customer_in_ticket_ref: ticket.trim() } : {}) } })), "Đã ghi chấp nhận bù (sự kiện bản bù)").then(done)}>
              Chấp nhận bù
            </button>
          </div>
        </div>
      ) : null}
      {d.status === "planned" || d.status === "dispatched" ? (
        <div className="grid3">
          <TextField label={`Lý do ${d.status === "planned" ? "huỷ" : "khách từ chối"} ${d.code}`} value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
          <div className="row">
            {d.status === "planned" ? (
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/deliveries/{id}/cancel", { params: p(), body: { reason: reason.trim() } })), "Đã huỷ lượt giao").then(done)}>
                Huỷ lượt giao
              </button>
            ) : (
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/deliveries/{id}/reject", { params: p(), body: { reason: reason.trim() } })), "Đã ghi khách từ chối").then(done)}>
                Khách từ chối
              </button>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function SoPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/orders/sales-orders/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const [lot, setLot] = useState("");
  const [route, setRoute] = useState<"HUB" | "DIRECT">("HUB");
  const [date, setDate] = useState(today());
  const [kg, setKg] = useState("");
  if (d.loading && !d.data) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const o = d.data!.sales_order!;
  const officer = hasRole(principal, ROLES.salesOfficer);
  const p = () => ({ path: { id }, header: idem() });
  const done = (x: unknown) => {
    if (x) {
      d.reload();
      onChanged();
      setReason("");
    }
  };
  const next = (["delivering", "delivered", "completed"] as const).filter((s) => canSalesAdvance(o.status ?? "", s));
  return (
    <div className="stack">
      <KV
        label="Thông tin đơn bán"
        items={[
          ["Mã", <b key="c" className="mono">{o.code}</b>],
          ["Trạng thái", <Chip key="s" status={o.status} label={SO_STATUS_LABEL[o.status ?? ""]} />],
          ["Khách (vault)", <Ref key="k" id={o.customer_ref} />],
          ["Dạng hàng · khối lượng", `${PRODUCT_LABEL[o.product ?? ""] ?? o.product} · ${formatKg(o.ordered_kg)}`],
          ["Giá bảng → đơn giá → sau chiết khấu", `${formatPrice(o.list_price, "tấn")} → ${formatPrice(o.unit_price, "tấn")} − ${o.discount_pct ?? "0"} % → ${formatPrice(o.net_price, "tấn")}`],
          ["Thành tiền", <Money key="m" v={o.amount} strong />],
          ["Duyệt giá / chiết khấu", o.needs_price_approval ? <span key="a" className="row"><span className="chip c-warn">Vượt ngưỡng — cần sales_approver khác người lập</span> <span className="mono small">{o.price_rule}</span> <Provisional what="Q-5" /></span> : <span key="a" className="chip c-ok">Trong ngưỡng</span>],
          ["Lịch giao", `${formatDate(o.delivery_from)} → ${formatDate(o.delivery_to)} · thanh toán ${o.payment_term_days} ngày`],
          ["Người lập → xác nhận giá", <SoD key="sd" maker={o.created_by} checker={o.approved_by} checkerLabel="Người duyệt giá" />],
          ...(o.reason ? ([["Lý do", o.reason]] as [string, string][]) : []),
        ]}
      />
      <ActionStatus a={act} />
      {o.status === "draft" ? (
        <div className="panel stack">
          {o.needs_price_approval ? <SelfHint proposer={o.created_by} /> : null}
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/sales-orders/{id}/confirm", { params: p() })), `Đã xác nhận đơn bán ${o.code}`).then(done)}>
              Xác nhận đơn bán
            </button>
          </div>
        </div>
      ) : null}
      {officer || hasRole(principal, ROLES.salesApprover) ? (
        <div className="row">
          {next.map((s) => (
            <button key={s} type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/sales-orders/{id}/advance", { params: p(), body: { status: s } })), `Đã chuyển đơn sang «${SO_STATUS_LABEL[s]}»`).then(done)}>
              Chuyển sang {SO_STATUS_LABEL[s]}
            </button>
          ))}
          {canSalesAdvance(o.status ?? "", "cancelled") ? (
            <>
              <TextField label="Lý do huỷ đơn" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/sales-orders/{id}/advance", { params: p(), body: { status: "cancelled", reason: reason.trim() } })), "Đã huỷ đơn bán").then(done)}>
                Huỷ đơn bán
              </button>
            </>
          ) : null}
        </div>
      ) : null}
      <h3>Lịch giao</h3>
      {(o.deliveries ?? []).length ? o.deliveries!.map((x) => <DeliveryRow key={x.delivery_id} d={x} onChanged={() => done(true)} />) : <Empty>Chưa có lượt giao.</Empty>}
      {officer && (o.status === "confirmed" || o.status === "delivering") ? (
        <div className="panel stack">
          <b>Thêm lượt giao</b>
          <div className="grid4">
            <RefPicker label="Lô xuất" kind="lot" value={lot} onChange={setLot} />
            <SelectField label="Tuyến" value={route} onChange={(r) => setRoute(r as "HUB")} options={[{ value: "HUB", label: "Qua hub (HUB)" }, { value: "DIRECT", label: "Giao thẳng (DIRECT)" }]} />
            <TextField label="Ngày giao" type="date" value={date} onChange={setDate} />
            <TextField label="Khối lượng kế hoạch (kg)" value={kg} onChange={setKg} inputMode="decimal" />
          </div>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !UUID.test(lot.trim()) || !isKg(kg.trim()) || !date} onClick={() => void act.run(() => unwrap(api.POST("/v1/orders/sales-orders/{id}/deliveries", { params: p(), body: { lot_ref: lot.trim(), route, planned_date: date, planned_kg: kg.trim() } })), (r) => `Đã thêm lượt giao ${r.delivery?.code ?? ""}`).then(done)}>
              Thêm lượt giao
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function CreateSo({ onCreated, customers }: { onCreated: (id: string) => void; customers: { id: string; label: string }[] }) {
  const { api } = useAuth();
  const act = useAction();
  const [customer, setCustomer] = useState("");
  const [product, setProduct] = useState<Product>("compressed_bale");
  const [kg, setKg] = useState("");
  const [list, setList] = useState("");
  const [price, setPrice] = useState("");
  const [disc, setDisc] = useState("0");
  const err = !UUID.test(customer.trim()) ? "Chọn khách (UUID ở vault — không nhập tên)" : !isKg(kg.trim()) || /^0+(\.0*)?$/.test(kg.trim()) ? "Khối lượng > 0" : !isPrice(price.trim()) ? "Đơn giá > 0" : list && !isPrice(list.trim()) ? "Giá bảng > 0" : !PCT.test(disc.trim()) ? "Chiết khấu 0 ≤ x < 100" : null;
  const preview = !err ? { net: netPrice(price.trim(), disc.trim()), amount: amountForKg(kg.trim(), netPrice(price.trim(), disc.trim())) } : null;
  return (
    <div className="stack">
      <RefPicker label="Khách hàng" kind="customer" value={customer} onChange={setCustomer} extra={customers} hint="Định danh khách ở vault MKG (RB-3)" />
      <div className="grid3">
        <SelectField label="Dạng hàng" value={product} onChange={(p) => setProduct(p as Product)} options={Object.entries(PRODUCT_LABEL).map(([value, label]) => ({ value, label }))} />
        <TextField label="Khối lượng đặt (kg)" value={kg} onChange={setKg} inputMode="decimal" />
        <TextField label="Giá bảng (VND/tấn)" value={list} onChange={setList} inputMode="decimal" hint="Tuỳ chọn — lệch giá bảng vượt ngưỡng cần duyệt" />
        <TextField label="Đơn giá (VND/tấn)" value={price} onChange={setPrice} inputMode="decimal" />
        <TextField label="Chiết khấu (%)" value={disc} onChange={setDisc} inputMode="decimal" />
      </div>
      {preview ? (
        <p className="small">
          Giá sau chiết khấu {formatPrice(preview.net, "tấn")} · thành tiền dự kiến <Money v={preview.amount} strong /> (tính bằng số thập phân, backend là nơi quyết định)
        </p>
      ) : (
        <p className="small mute">{err}</p>
      )}
      <ActionStatus a={act} />
      <div className="row">
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={!!err || act.busy}
          onClick={() =>
            void act
              .run(
                () => unwrap(api.POST("/v1/orders/sales-orders", { params: { header: idem() }, body: { customer_ref: customer.trim(), product, ordered_kg: kg.trim(), unit_price: price.trim(), discount_pct: disc.trim(), ...(list.trim() ? { list_price: list.trim() } : {}) } })),
                (r) => `Đã lập đơn bán ${r.sales_order?.code ?? ""} (nháp)${r.sales_order?.needs_price_approval ? " — giá/chiết khấu vượt ngưỡng, cần người duyệt khác" : ""}`,
              )
              .then((r) => {
                if (r?.sales_order?.order_id) {
                  remember("customer", customer.trim());
                  onCreated(r.sales_order.order_id);
                }
              })
          }
        >
          Lập đơn bán
        </button>
      </div>
    </div>
  );
}

export default function SalesOrdersPage() {
  const { api, principal } = useAuth();
  const [status, setStatus] = useState("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/orders/sales-orders", { params: { query: { limit: 200, ...(status ? { status: status as "draft" } : {}) } } }))).items ?? [], [api, status]);
  const [sel, setSel] = useState("");
  const [creating, setCreating] = useState(false);
  const items: OrdersSalesOrder[] = list.data ?? [];
  const customers = Array.from(new Set(items.map((o) => o.customer_ref!))).map((c) => ({ id: c, label: `Khách ${shortRef(c)}` }));
  return (
    <div className="stack">
      <PageHead
        title="Đơn bán & giao nhận"
        subtitle="Đơn bán chỉ tiến (nháp → xác nhận → đang giao → đã giao → hoàn tất). Giá lệch giá bảng / chiết khấu vượt ngưỡng (tham số chờ khách) cần sales_approver KHÁC người lập. Giao nhận chấp nhận theo phiếu cân CUSTOMER_IN."
        actions={
          hasRole(principal, ROLES.salesOfficer) ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating((v) => !v)}>
              Lập đơn bán
            </button>
          ) : null
        }
      />
      {creating ? (
        <Section title="Lập đơn bán">
          <CreateSo
            customers={customers}
            onCreated={(id) => {
              setCreating(false);
              setSel(id);
              list.reload();
            }}
          />
        </Section>
      ) : null}
      <Section title="Danh sách đơn bán">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={setStatus} options={[{ value: "", label: "Tất cả" }, ...Object.entries(SO_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.data && !items.length ? <Empty>Không có đơn bán.</Empty> : null}
          {items.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Đơn bán">
                <thead>
                  <tr>
                    <th>Mã</th>
                    <th>Khách</th>
                    <th>Dạng hàng</th>
                    <th className="num">Khối lượng</th>
                    <th className="num">Giá sau CK</th>
                    <th className="num">Thành tiền</th>
                    <th>Trạng thái</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((o) => (
                    <tr key={o.order_id} aria-selected={o.order_id === sel}>
                      <td className="mono">{o.code}</td>
                      <td>
                        <Ref id={o.customer_ref} />
                      </td>
                      <td>{PRODUCT_LABEL[o.product ?? ""]}</td>
                      <td className="num">{formatKg(o.ordered_kg)}</td>
                      <td className="num">{formatPrice(o.net_price, "tấn")}</td>
                      <td className="num">
                        <Money v={o.amount} />
                      </td>
                      <td>
                        <Chip status={o.status} label={SO_STATUS_LABEL[o.status ?? ""]} /> {o.needs_price_approval && o.status === "draft" ? <span className="chip c-warn">cần duyệt giá</span> : null}
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(o.order_id!)} aria-label={`Mở đơn bán ${o.code}`}>
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
        <Section title="Chi tiết đơn bán">
          <SoPanel key={sel} id={sel} onChanged={list.reload} />
        </Section>
      ) : null}
    </div>
  );
}
