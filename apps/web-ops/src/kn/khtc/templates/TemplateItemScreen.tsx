"use client";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { uuidv7 } from "@/api/idempotency";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DetailPage, LogPanel } from "@/components/patterns/detail";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { createKhtcPeriodApi, type KhtcPeriod } from "../periods/api";
import { TEMPLATE_TITLES, useTemplateApi, type TemplateApi, type TemplateBinding, type TemplateDetail, type TemplateKey, type TemplateNode } from "./api";
import { FIELDS, FIELD_LABELS, isTemplateKey, outline, REPEAT_OPTIONS, SOURCE_LABELS, templateHref, templateLog, validateNode } from "./template";
import { TemplatePreviewPanel } from "./TemplatePreview";

export function TemplateItemScreen({ create = false }: { create?: boolean }) {
  const { kn } = useAuth(); const params = useParams<{ dept: string; template: string; item: string }>();
  const key = params.template ?? "";
  if (!isTemplateKey(key) || params.dept !== "ke_hoach_tai_chinh" || kn.context?.role !== "admin" || kn.context.read_only || kn.support) return <ReadOnlyNotice>Chỉ Admin trong phiên được phép ghi cấu hình mục lục mẫu.</ReadOnlyNotice>;
  return <Record key={`${kn.context.context_id}:${key}:${params.item ?? "new"}:${create}`} template={key} item={params.item} create={create} />;
}
function Record({ template, item, create }: { template: TemplateKey; item: string; create: boolean }) {
  const api = useTemplateApi(); const { api: client } = useAuth(); const periodApi = useMemo(() => createKhtcPeriodApi(client), [client]);
  const state = useLoad(() => api.get(template), [api, template]); const periods = useLoad(() => periodApi.list(), [periodApi]);
  const kind = ({ mau_1: "week", mau_2: "month", mau_3: "quarter", mau_4: "year", mau_5: "program" } as const)[template];
  return <LoadState s={state}>{(detail) => {
    if (!detail.editable) return <ReadOnlyNotice>Phiên này chỉ xem cấu hình mẫu.</ReadOnlyNotice>;
    const node = create ? undefined : detail.definition.nodes.find((n) => n.id === item);
    if (!create && !node) return <DetailPage title="Không tìm thấy mục" back={{ href: templateHref(template), label: TEMPLATE_TITLES[template] }}><p>Mục không còn trong phiên bản đang xem.</p></DetailPage>;
    return <Editor key={`${detail.version_id ?? "seed"}:${item ?? "new"}`} detail={detail} original={node} api={api} periods={(periods.data ?? []).filter((p) => p.report_type === kind && p.state !== "cancelled").sort((a, b) => b.from_date.localeCompare(a.from_date))} />;
  }}</LoadState>;
}

function Editor({ detail, original, api, periods }: { detail: TemplateDetail; original?: TemplateNode; api: TemplateApi; periods: KhtcPeriod[] }) {
  const router = useRouter(); const act = useAction();
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [node, setNode] = useState<TemplateNode>(() => original ? structuredClone(original) : { id: uuidv7(), parent: "", order: 1, title: "", repeat: "none", body: "", bindings: [], text_slot: false, locked_admin: [] });
  const [order, setOrder] = useState(String(original?.order ?? 1)); const [errors, setErrors] = useState<Record<string, string>>({}); const [ack, setAck] = useState(false);
  const protectedItem = detail.protected_ids.includes(node.id);
  const changed = original ? JSON.stringify(node) !== JSON.stringify(original) || order !== String(original.order) : node.title !== "" || node.body !== "" || node.parent !== "";
  useEffect(() => { const guard = (event: BeforeUnloadEvent) => { if (changed) { event.preventDefault(); event.returnValue = ""; } }; window.addEventListener("beforeunload", guard); return () => window.removeEventListener("beforeunload", guard); }, [changed]);
  const cancel = () => { if (!changed || window.confirm("Bạn có thay đổi chưa lưu. Hủy các thay đổi này?")) router.push(templateHref(detail.key)); };
  const patch = (update: Partial<TemplateNode>) => setNode((current) => ({ ...current, ...update }));
  const save = async () => {
    const checked = validateNode(node, order, detail.definition); setErrors(checked.errors); if (!checked.node) return;
    const definition = { ...detail.definition, nodes: original ? detail.definition.nodes.map((n) => n.id === node.id ? checked.node! : n) : [...detail.definition.nodes, checked.node] };
    const value = await act.run(() => api.save(detail.key, { base_version: detail.version, definition, acknowledge_text_loss: ack }), "Đã lưu phiên bản nháp mới.");
    if (value && mounted.current) router.push(templateHref(detail.key));
  };
  const serverErrors = act.problem ? fieldErrorMap(act.problem) : {};
  const error = (key: string) => errors[key] ?? serverErrors[`nodes.${node.id}.${key}`] ?? serverErrors[key];
  const current = { ...node, order: order.length <= 12 && /^\d+$/.test(order) ? Number(order) : node.order };
  const draftDefinition = { ...detail.definition, nodes: original ? detail.definition.nodes.map((n) => n.id === node.id ? current : n) : [...detail.definition.nodes, current] };
  const setBinding = (index: number, update: Partial<TemplateBinding>) => patch({ bindings: node.bindings.map((b, i) => i === index ? { ...b, ...update } : b) });
  return <DetailPage back={{ href: templateHref(detail.key), label: TEMPLATE_TITLES[detail.key] }} title={original ? `Sửa mục: ${original.title}` : "Thêm mục con"} log={<LogPanel entries={templateLog(detail)} />} actions={<><button className="btn" type="button" onClick={cancel}>Hủy</button><button className="btn btn-primary" type="button" disabled={act.busy} onClick={() => void save()}>Lưu bản nháp</button></>}>
    {act.view}
    <form onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <SelectField label="Mục cha" required={!original} value={node.parent} disabled={!!original} error={error("parent")} onChange={(parent) => { patch({ parent }); setOrder(String(Math.max(0, ...detail.definition.nodes.filter((n) => n.parent === parent).map((n) => n.order)) + 1)); }} options={outline(detail.definition).filter((n) => n.depth < 5).map((n) => ({ value: n.id, label: `${"— ".repeat(n.depth)}${n.number}. ${n.title}` }))} placeholder={original && !node.parent ? "Mục cấp cao nhất" : "Chọn mục cha"} />
      <TextField label="Tên mục" required value={node.title} disabled={protectedItem} error={error("title")} maxLength={500} onChange={(title) => patch({ title })} />
      <NumberField label="Thứ tự trong mục cha" required value={order} disabled={protectedItem} error={error("order")} rule={{ integer: true, positive: true, max: 10000, maxInputLength: 12 }} onChange={setOrder} />
      {protectedItem ? <p>Tiêu đề và thứ tự cấp I/II được giữ theo mẫu chính thức.</p> : null}
      <SelectField label="Phạm vi lặp" required value={node.repeat} error={error("repeat")} onChange={(repeat) => patch({ repeat: repeat as TemplateNode["repeat"], ...(repeat === "none" ? { preamble: "" } : {}) })} options={REPEAT_OPTIONS} />
      {node.repeat === "plan_item" || node.repeat === "program" ? <SelectField label="Nhóm kế hoạch" value={node.tab ?? "all"} onChange={(tab) => patch({ tab: tab as TemplateNode["tab"] })} options={[{ value: "all", label: "Tất cả" }, { value: "previous", label: "Còn theo dõi năm trước" }, { value: "current", label: "Triển khai trong năm" }]} /> : null}
      <SelectField label="Giới hạn phòng cung cấp" value={node.department ?? ""} onChange={(department) => patch({ department })} placeholder="Mọi phòng phù hợp" options={[{ value: "trong_trot", label: "Trồng trọt" }, { value: "chan_nuoi_thuy_san", label: "Chăn nuôi – Thủy sản" }, { value: "hanh_chinh_tong_hop", label: "Hành chính – Tổng hợp" }, { value: "thong_tin_truyen_thong", label: "Thông tin – Tuyên truyền" }]} />
      {node.repeat !== "none" ? <TextField label="Câu mở đầu nhóm chương trình" value={node.preamble ?? ""} maxLength={1000} onChange={(preamble) => patch({ preamble })} error={error("preamble")} /> : null}
      <TextField label="Khuôn câu" required={!detail.definition.nodes.some((n) => n.parent === node.id)} multiline value={node.body} maxLength={12000} onChange={(body) => patch({ body })} error={error("body")} hint="Chèn biến đã gắn nguồn bằng nút bên dưới. Dùng {{ và }} để viết dấu ngoặc nhọn." />
      <label><input type="checkbox" checked={node.text_slot} onChange={(event) => patch({ text_slot: event.target.checked })} /> Có vùng lời văn KHTC tự biên tập</label>
      <section className="card sec"><h2>Biến và nguồn dữ liệu</h2><p>Biến nguồn và biến tính toán luôn khóa. Loại xử lý được hệ thống xác định từ nguồn các biến.</p>
        {error("bindings") ? <p className="field-error" role="alert">{error("bindings")}</p> : null}
        {node.bindings.map((binding, index) => <fieldset key={index} className="card sec"><legend>Biến {index + 1}</legend>
          <TextField label="Tên biến" required value={binding.variable} maxLength={40} onChange={(variable) => setBinding(index, { variable })} hint="Chữ thường không dấu và gạch dưới, ví dụ ten_hang_muc." />
          <SelectField label="Nguồn biến" required value={binding.source} onChange={(source) => { const s = source as TemplateBinding["source"]; if (FIELDS[s]) setBinding(index, { source: s, field: FIELDS[s][0]!, ref: "" }); }} options={Object.entries(SOURCE_LABELS).map(([value, label]) => ({ value, label }))} />
          <SelectField label="Ô dữ liệu" required value={binding.field} onChange={(field) => setBinding(index, { field })} options={FIELDS[binding.source].map((field) => ({ value: field, label: FIELD_LABELS[field] ?? field }))} />
          {binding.source !== "admin" && binding.source !== "text" ? <TextField label="Tham chiếu chỉ tiêu cụ thể (nếu có)" value={binding.ref ?? ""} maxLength={200} onChange={(ref) => setBinding(index, { ref })} hint="Để trống khi lấy giá trị của từng dòng lặp." /> : null}
          <button className="btn" type="button" onClick={() => patch({ body: `${node.body}{${binding.variable}}` })}>Chèn biến vào câu</button>{" "}
          <button className="btn" type="button" onClick={() => patch({ bindings: node.bindings.filter((_, i) => i !== index) })}>Bỏ biến</button>
          {binding.source === "admin" ? <label><input type="checkbox" checked={node.locked_admin.includes(binding.field)} onChange={(event) => patch({ locked_admin: event.target.checked ? [...new Set([...node.locked_admin, binding.field])] : node.locked_admin.filter((field) => field !== binding.field) })} /> Khóa thêm ô hành chính này</label> : <p>{binding.source === "text" ? "Lời văn cho KHTC biên tập." : "Biến này luôn khóa trên bản soạn."}</p>}
        </fieldset>)}
        <button className="btn" type="button" disabled={node.bindings.length >= 32} onClick={() => patch({ bindings: [...node.bindings, { variable: `bien_${node.bindings.length + 1}`, source: "plan", field: "item_name" }] })}>Thêm biến</button>
      </section>
      <label><input type="checkbox" checked={ack} onChange={(event) => setAck(event.target.checked)} /> Tôi xác nhận nếu bỏ vùng lời văn đã dùng: thay đổi chỉ áp dụng cho phiên bản mẫu mới, giữ nội dung và tệp lịch sử.</label>
      <p><button className="btn btn-primary" disabled={act.busy} type="submit">Lưu bản nháp</button></p>
    </form>
    <TemplatePreviewPanel api={api} definition={draftDefinition} periods={periods} />
  </DetailPage>;
}
