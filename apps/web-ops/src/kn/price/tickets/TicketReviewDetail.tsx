"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { NumberField, TextField } from "@/components/form";
import { DetailPage, LabelValueBlock } from "@/components/patterns/detail";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { TICKET_VIEW_LABEL, money, vnTime, type PriceSheet, type PriceTicketApi } from "./api";
import { SheetTable } from "./SheetLines";
import { fieldKey, PRICE_REVIEW_RULE, reviewInitial, reviewServerErrors, validateReview, type ReviewErrors } from "./ticket-review-form";
import { reviewHref, type ReviewFilters, type ReviewRoute } from "./ticket-review-routes";
import { ReviewDateField, TicketReviewLog } from "./ticket-review-view";
type TicketRoute = Extract<ReviewRoute, { kind: "ticket" }>;
const TITLES = { view: "Chi tiết phiếu báo giá", correct: "Hiệu chỉnh phiếu báo giá", redate: "Sửa ngày phiếu báo giá", delete: "Xóa phiếu báo giá" };
export function TicketReviewDetail({ route, filters, ta, readOnly }: { route: TicketRoute; filters: ReviewFilters; ta: PriceTicketApi; readOnly: boolean }) {
  const load = useLoad(() => ta.ticket(route.id), [ta, route.id]);
  const log = useLoad(() => ta.log(route.id), [ta, route.id]);
  const back = { href: reviewHref(filters), label: "Danh sách kiểm duyệt" };
  return <LoadState s={load}>{sheet => {
    if (!sheet.ticket || sheet.ticket.id !== route.id) return <DetailPage title="Không tìm thấy phiếu" back={back}><p role="alert">Không tìm thấy phiếu theo đúng mã và phạm vi được cấp.</p></DetailPage>;
    const canWrite = !readOnly && route.action !== "view" && sheet.can[route.action];
    return <DetailPage title={TITLES[route.action]} back={back}
      actions={route.action === "view" ? <div className="row">{(["correct", "redate", "delete"] as const).filter(k => !readOnly && sheet.can[k]).map(k => <Link key={k} className="btn" href={reviewHref(filters, route.id, k)}>{({ correct: "Hiệu chỉnh", redate: "Sửa ngày", delete: "Xóa phiếu" })[k]}</Link>)}<Link className="btn" href={`/kn/bao-gia/in?ticket=${route.id}`}>In phiếu</Link></div> : undefined}
      log={<LoadState s={log}>{value => <TicketReviewLog log={value} />}</LoadState>}>
      <LabelValueBlock title="Thông tin phiếu" items={[{ label: "Xã báo giá", value: `${sheet.point.name} (${sheet.point.code})` }, { label: "Ngày phiếu", value: sheet.day }, { label: "Phiên bản", value: `v${sheet.ticket.version}` }, { label: "Trạng thái", value: TICKET_VIEW_LABEL[sheet.view] }, { label: "Người cập nhật", value: sheet.ticket.actor_name || sheet.ticket.actor_ref }, { label: "Thời điểm cập nhật", value: vnTime(sheet.ticket.updated_at) }]} />
      {route.action === "view" ? <><p>Giá Xã gửi được giữ nguyên. Giá hiệu chỉnh được dùng cho bảng giá; nhật ký bên cạnh ghi các phiên bản thực.</p><SheetTable sheet={sheet} /></> : canWrite ? <ReviewForm key={`${sheet.ticket.version}`} sheet={sheet} route={route} filters={filters} ta={ta} onReload={load.reload} /> : <><p role="note">Chỉ xem — thao tác không được cấp trong ngữ cảnh hoặc trạng thái hiện tại. Cần xã thuộc phân công hoặc chưa phân công, đã khóa nhập và bảng giá cho phép sửa.</p><Link className="btn" href={reviewHref(filters, route.id)}>Xem phiếu</Link></>}
    </DetailPage>;
  }}</LoadState>;
}
function ReviewForm({ sheet, route, filters, ta, onReload }: { sheet: PriceSheet; route: TicketRoute; filters: ReviewFilters; ta: PriceTicketApi; onReload: () => void }) {
  const [fields, setFields] = useState(() => reviewInitial(sheet));
  const [errors, setErrors] = useState<ReviewErrors>({});
  const [submitted, setSubmitted] = useState<Record<string, string>>({});
  const [unconfirmed, setUnconfirmed] = useState(false);
  const act = useAction(), router = useRouter(), alive = useRef(true), running = useRef(false);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const change = (key: string, value: string) => { setFields(s => ({ ...s, [key]: value })); setErrors(e => { const n = { ...e }; delete n[key]; delete n.lines; return n; }); };
  const server = reviewServerErrors(act.problem, fields);
  const error = (key: string) => errors[key] ?? (submitted[key] === fields[key] ? server[key] : undefined);
  const save = async () => {
    if (running.current || !sheet.ticket || route.action === "view" || !sheet.can[route.action]) return;
    const v = validateReview(sheet, route.action, fields); setErrors(v.errors); if (!v.valid) return;
    running.current = true; setSubmitted({ ...fields }); setUnconfirmed(false);
    try {
      const result = await act.run(() => route.action === "correct" ? ta.correct(route.id, { version: sheet.ticket!.version, reason: v.reason, lines: v.lines }) : route.action === "redate" ? ta.redate(route.id, { version: sheet.ticket!.version, day: v.day, reason: v.reason }) : ta.remove(route.id, { version: sheet.ticket!.version, reason: v.reason }));
      if (alive.current) { if (result) router.push(reviewHref(filters, route.id)); else setUnconfirmed(true); }
    } finally { running.current = false; }
  };
  const conflict = act.problem?.code === "price.ticket_stale";
  return <form onSubmit={e => { e.preventDefault(); void save(); }}>
    {route.action === "correct" && <><p>Chỉ điền những dòng cần hiệu chỉnh. Giữ nguyên giá Xã gửi; mỗi cặp giá mới phải đầy đủ.</p>{sheet.lines.filter(l => !!l.min && !!l.max && !l.no_goods).map(l => <fieldset key={l.item_code} disabled={act.busy}><legend>{l.name} {l.variety} ({l.unit_name})</legend><p>Giá Xã gửi: {money(l.min)}–{money(l.max)}. Giá đang sử dụng: {money(l.adj_min ?? l.min)}–{money(l.adj_max ?? l.max)}.</p><NumberField label={`Giá thấp nhất ${l.name} ${l.variety} (${l.item_code})`} value={fields[fieldKey(l.item_code, "min")] ?? ""} onChange={v => change(fieldKey(l.item_code, "min"), v)} required={!!fields[fieldKey(l.item_code, "min")]?.trim() || !!fields[fieldKey(l.item_code, "max")]?.trim()} rule={{ ...PRICE_REVIEW_RULE, required: false }} error={error(fieldKey(l.item_code, "min"))} /><NumberField label={`Giá cao nhất ${l.name} ${l.variety} (${l.item_code})`} value={fields[fieldKey(l.item_code, "max")] ?? ""} onChange={v => change(fieldKey(l.item_code, "max"), v)} required={!!fields[fieldKey(l.item_code, "min")]?.trim() || !!fields[fieldKey(l.item_code, "max")]?.trim()} rule={{ ...PRICE_REVIEW_RULE, required: false }} error={error(fieldKey(l.item_code, "max"))} />{l.alert?.reply?.response === "propose" && l.alert.reply.proposed_min !== null && <button type="button" className="btn" onClick={() => { change(fieldKey(l.item_code, "min"), l.alert!.reply!.proposed_min!); change(fieldKey(l.item_code, "max"), l.alert!.reply!.proposed_max!); }}>Dùng giá Xã đề xuất: {money(l.alert.reply.proposed_min)}–{money(l.alert.reply.proposed_max)}</button>}</fieldset>)}{error("lines") && <p role="alert" className="err">{error("lines")}</p>}</>}
    {route.action === "redate" && <ReviewDateField value={fields.day ?? ""} onChange={v => change("day", v)} error={error("day")} disabled={act.busy} />}
    {route.action === "delete" && <p role="alert">Phiếu sẽ chuyển Đã xóa, không tính vào bảng giá và thống kê. Lịch sử vẫn được giữ.</p>}
    <TextField label="Lý do" multiline required hint="1–500 ký tự Unicode, không tính khoảng trắng đầu/cuối." value={fields.reason ?? ""} onChange={v => change("reason", v)} error={error("reason")} disabled={act.busy} />
    {act.view}{unconfirmed && <p role="status">Chưa xác nhận lưu thành công. Kiểm tra thông báo và thử lại cùng dữ liệu nếu kết nối bị gián đoạn.</p>}{conflict && <button type="button" className="btn" onClick={onReload}>Tải lại phiếu và quyền hiện tại</button>}
    <div className="row"><Link className="btn" href={reviewHref(filters, route.id)}>Hủy</Link><button type="submit" className="btn btn-primary" disabled={act.busy || conflict}>{route.action === "delete" ? "Xác nhận xóa phiếu" : "Lưu"}</button></div>
  </form>;
}
