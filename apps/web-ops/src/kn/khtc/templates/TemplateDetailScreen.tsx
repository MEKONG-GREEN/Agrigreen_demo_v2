"use client";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField, validateForm } from "@/components/form";
import { DetailPage, LogPanel } from "@/components/patterns/detail";
import { DataTable, StatusPill } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { createKhtcPeriodApi, type KhtcPeriod } from "../periods/api";
import { TEMPLATE_TITLES, useTemplateApi, type TemplateApi, type TemplateDetail, type TemplateKey } from "./api";
import { isTemplateKey, itemHref, outline, REPEAT_OPTIONS, TEMPLATE_BASE, templateHref, templateLog } from "./template";
import { TemplatePreviewPanel } from "./TemplatePreview";

export function TemplateDetailScreen() {
  const { kn } = useAuth(); const params = useParams<{ dept: string; template: string }>();
  const key = params.template ?? "";
  if (!isTemplateKey(key) || params.dept !== "ke_hoach_tai_chinh" || kn.context?.role !== "admin") return <ReadOnlyNotice>Không có mẫu báo cáo hoặc không có quyền cấu hình.</ReadOnlyNotice>;
  return <Record key={`${kn.context.context_id}:${key}`} template={key} />;
}
function Record({ template }: { template: TemplateKey }) {
  const api = useTemplateApi(); const { api: client } = useAuth();
  const periods = useMemo(() => createKhtcPeriodApi(client), [client]);
  const state = useLoad(() => api.get(template), [api, template]);
  const choices = useLoad(() => periods.list(), [periods]);
  const kind = ({ mau_1: "week", mau_2: "month", mau_3: "quarter", mau_4: "year", mau_5: "program" } as const)[template];
  return <LoadState s={state}>{(detail) => <Detail key={`${detail.version_id ?? "seed"}:${detail.publications[0]?.id ?? ""}`} detail={detail} api={api} reload={state.reload} periods={(choices.data ?? []).filter((p) => p.report_type === kind && p.state !== "cancelled").sort((a, b) => b.from_date.localeCompare(a.from_date))} />}</LoadState>;
}
function Detail({ detail, api, reload, periods }: { detail: TemplateDetail; api: TemplateApi; reload: () => void; periods: KhtcPeriod[] }) {
  const [periodID, setPeriodID] = useState(""); const [restoreID, setRestoreID] = useState(""); const [errors, setErrors] = useState<Record<string, string>>({});
  const [ack, setAck] = useState(false);
  const [previewID, setPreviewID] = useState("");
  const act = useAction(); const period = periods.find((p) => p.id === periodID);
  const publish = async () => {
    const checked = validateForm({ period: periodID }, { period: { kind: "text", required: true } }); setErrors(checked.errors);
    if (!checked.valid || !period || !detail.version_id) return;
    const value = await act.run(() => api.publish(detail.key, { base_version: detail.version, version_id: detail.version_id!, applies_from: period.from_date.slice(0, 10) }), "Đã phát hành mẫu cho các kỳ áp dụng.");
    if (value) reload();
  };
  const restore = async () => {
    if (!restoreID || !window.confirm("Khôi phục phiên bản này thành bản nháp mới? Bản soạn và tệp lịch sử được giữ nguyên.")) return;
    const value = await act.run(() => api.restore(detail.key, { base_version: detail.version, version_id: restoreID, acknowledge_text_loss: ack }), "Đã tạo bản nháp mới từ phiên bản cũ."); if (value) reload();
  };
  const saveSeed = async () => { const value = await act.run(() => api.save(detail.key, { base_version: detail.version, definition: detail.definition }), "Đã lưu bản nháp mẫu."); if (value) reload(); };
  const remove = async (id: string) => {
    if (!window.confirm("Xóa mục con này khỏi phiên bản mới? Nếu đã có lời văn trên bản soạn, máy chủ sẽ yêu cầu xác nhận.")) return;
    const removed = new Set([id]); let size = 0;
    while (removed.size !== size) { size = removed.size; for (const node of detail.definition.nodes) if (removed.has(node.parent)) removed.add(node.id); }
    const definition = { ...detail.definition, nodes: detail.definition.nodes.filter((n) => !removed.has(n.id)) };
    const value = await act.run(() => api.save(detail.key, { base_version: detail.version, definition, acknowledge_text_loss: ack }), "Đã xóa mục khỏi bản nháp mới."); if (value) reload();
  };
  return <DetailPage back={{ href: TEMPLATE_BASE, label: "Năm mẫu báo cáo" }} title={TEMPLATE_TITLES[detail.key]} badge={<StatusPill>{detail.version ? `Bản nháp v${detail.version}` : "Khung ban đầu"}</StatusPill>} log={<LogPanel entries={templateLog(detail)} />} actions={detail.editable ? <><Link className="btn" href={`${templateHref(detail.key)}/muc/them-moi`}>Thêm mục con</Link>{!detail.version_id ? <button className="btn btn-primary" disabled={act.busy} onClick={() => void saveSeed()}>Lưu khung ban đầu</button> : null}</> : null}>
    {act.view}
    {!detail.editable ? <ReadOnlyNotice>Phiên này chỉ xem cấu hình mẫu.</ReadOnlyNotice> : null}
    <p>Giữ tiêu đề và thứ tự cấp I/II. Chỉ xóa mục con do Admin thêm; lưu luôn tạo phiên bản nháp mới.</p>
    <DataTable caption="Mục lục mẫu" rows={outline(detail.definition)} rowKey={(n) => n.id} columns={[
      { key: "title", header: "Mục", cell: (n) => <span style={{ paddingInlineStart: `${n.depth * 12}px` }}>{n.number}. {n.title}</span> },
      { key: "repeat", header: "Lặp", cell: (n) => REPEAT_OPTIONS.find((option) => option.value === n.repeat)?.label ?? "Không lặp" },
      { key: "processing", header: "Loại xử lý", cell: (n) => n.bindings.length ? "Tự xác định từ nguồn các biến" : "Văn bản cố định" },
      { key: "protected", header: "Khung chính thức", cell: (n) => detail.protected_ids.includes(n.id) ? "Cấp I/II — giữ tiêu đề, thứ tự" : "Mục con" },
      { key: "actions", header: "Thao tác", cell: (n) => <>{detail.editable ? <Link href={itemHref(detail.key, n.id)}>Sửa</Link> : null}{detail.editable && !detail.seeded_ids.includes(n.id) ? <button className="btn" disabled={act.busy} onClick={() => void remove(n.id)}>Xóa</button> : null}</> },
    ]} />
    {detail.editable ? <label><input type="checkbox" checked={ack} onChange={(event) => setAck(event.target.checked)} /> Tôi xác nhận khi bỏ vùng lời văn đã có nội dung: chỉ đổi phiên bản mẫu mới, giữ bản soạn và tệp lịch sử.</label> : null}
    {detail.history.length ? <SelectField label="Phiên bản xem trước" value={previewID} onChange={setPreviewID} placeholder="Bản nháp đang xem" options={detail.history.filter((v) => v.id !== detail.version_id).map((v) => ({ value: v.id, label: `Phiên bản ${v.version} — lịch sử` }))} /> : null}
    <TemplatePreviewPanel api={api} definition={detail.history.find((v) => v.id === previewID)?.definition ?? detail.definition} periods={periods} />
    {detail.editable && detail.version_id ? <section className="card sec"><h2>Phát hành phiên bản</h2><SelectField label="Kỳ bắt đầu áp dụng" required value={periodID} error={errors.period} onChange={setPeriodID} options={periods.map((p) => ({ value: p.id, label: p.name }))} /><p>{period ? `Áp dụng cho kỳ bắt đầu từ ${period.from_date.slice(0, 10)}. Các bản soạn đã tạo giữ phiên bản của mình.` : "Chọn một kỳ báo cáo để xác định ngày bắt đầu áp dụng."}</p><button className="btn btn-primary" disabled={act.busy || !periods.length} onClick={() => void publish()}>Phát hành bản nháp</button></section> : null}
    {detail.editable && detail.history.length ? <section className="card sec"><h2>Khôi phục phiên bản</h2><SelectField label="Phiên bản nguồn" value={restoreID} onChange={setRestoreID} options={detail.history.map((v) => ({ value: v.id, label: `Phiên bản ${v.version} · ${new Date(v.at).toLocaleString("vi-VN")}` }))} /><button className="btn" disabled={act.busy || !restoreID} onClick={() => void restore()}>Khôi phục thành bản nháp mới</button></section> : null}
  </DetailPage>;
}
