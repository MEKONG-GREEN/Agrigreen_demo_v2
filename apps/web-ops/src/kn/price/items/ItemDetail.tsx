"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField, TextField, validateForm } from "@/components/form";
import { DetailPage } from "@/components/patterns/detail";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { createPriceItemApi, itemLabel, type PriceCatalog, type PriceGroup, type PriceItemApi, type PriceItemDetail } from "./api";
import { initialItemFields, itemBody, validateItemFields, type FieldErrors, type ItemFields } from "./item-form";
import { itemFilters, itemHref, vnItemToday, type ItemFilters, type ItemRoute } from "./item-routes";
import { GroupLog, GroupSummary, ItemDateField, itemFieldErrors, ItemLog, ItemSummary } from "./item-view";
import { isCalendarDay } from "../calendar-routes";

export function PriceItemDetailScreen({ route }: { route: ItemRoute }) {
  const auth = useAuth();
  const query = useSearchParams();
  const filters = itemFilters(query, vnItemToday());
  if (route.kind === "group" || route.kind === "group-new") filters.tab = "groups";
  const pa = useMemo(() => createPriceItemApi(auth.api), [auth.api]);
  const key = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}:${auth.kn.context?.role}:${JSON.stringify(route)}:${JSON.stringify(filters)}`;
  return <ItemData key={key} route={route} pa={pa} filters={filters} showLog={auth.kn.context?.role === "kn_tinh" || auth.kn.context?.role === "admin"} />;
}

function ItemData({ route, pa, filters: f, showLog }: { route: ItemRoute; pa: PriceItemApi; filters: ItemFilters; showLog: boolean }) {
  const status = useLoad(() => pa.items({ at: f.at }), [pa, f.at]);
  const catalog = useLoad(() => pa.catalog(f.at), [pa, f.at]);
  const item = useLoad(route.kind === "item" ? () => pa.item(route.code, f.at) : null, [pa, route.kind, "code" in route ? route.code : "", f.at]);
  const groups = useLoad(route.kind === "group" ? () => pa.groups(f.at) : null, [pa, route.kind, f.at]);
  const back = { href: itemHref("", f), label: "Danh mục mặt hàng" };
  return <LoadState s={status}>{(permissions) => <LoadState s={catalog}>{(cat) => {
    const canEdit = permissions.can_edit;
    const steward = permissions.can_steward;
    if (route.kind === "item-new") return canEdit
      ? <ItemForm pa={pa} catalog={cat} filters={f} showLog={showLog} create />
      : <DetailPage title="Thêm mặt hàng" back={back}><ReadOnly /></DetailPage>;
    if (route.kind === "group-new") return steward
      ? <GroupForm pa={pa} filters={f} />
      : <DetailPage title="Thêm nhóm hàng" back={back}><ReadOnly /></DetailPage>;
    if (route.kind === "group") return <LoadState s={groups}>{(data) => {
      const group = data.groups.find((entry) => entry.code === route.code);
      if (!group) return <DetailPage title="Nhóm hàng" back={back}><p role="alert">Không tìm thấy nhóm hàng tại ngày và phạm vi được cấp.</p></DetailPage>;
      if (route.edit && steward) return <GroupForm key={group.version} group={group} pa={pa} filters={f} />;
      return <DetailPage title={`Nhóm hàng ${group.code}`} back={back} log={showLog ? <GroupLog /> : undefined}
        actions={steward ? <Link className="btn btn-p" href={itemHref(`nhom-hang/${group.code}/sua`, f)}>Sửa tên</Link> : undefined}>
        {route.edit && <ReadOnly />}<GroupSummary group={group} />
      </DetailPage>;
    }}</LoadState>;
    return <LoadState s={item}>{(detail) => {
      const active = detail.state !== "retired";
      if (route.mode === "edit" && active && canEdit && (!detail.has_price || steward))
        return <ItemForm key={detail.version} detail={detail} pa={pa} catalog={cat} filters={f} showLog={showLog} />;
      if (route.mode === "replace" && active && canEdit)
        return <ItemForm key={detail.version} detail={detail} pa={pa} catalog={cat} filters={f} showLog={showLog} create replace />;
      if (route.mode === "retire" && active && steward)
        return <RetireForm detail={detail} pa={pa} filters={f} showLog={showLog} />;
      return <DetailPage title={`${detail.code} — ${itemLabel(detail)}`} back={back} log={showLog ? <ItemLog detail={detail} /> : undefined}
        actions={active ? <span className="row">
          {canEdit && (!detail.has_price || steward) && <Link className="btn btn-p" href={itemHref(`${detail.code}/sua`, f)}>Sửa</Link>}
          {canEdit && <Link className="btn btn-o" href={itemHref(`${detail.code}/thay-the`, f)}>Tạo mặt hàng thay thế</Link>}
          {steward && <Link className="btn btn-d" href={itemHref(`${detail.code}/ngung`, f)}>Ngưng</Link>}
        </span> : undefined}>
        {route.mode !== "view" && <ReadOnly />}<ItemSummary detail={detail} catalog={cat} />
      </DetailPage>;
    }}</LoadState>;
  }}</LoadState>}</LoadState>;
}

function ReadOnly() {
  return <p className="alert alert-info" role="note">Chỉ xem — ngữ cảnh hiện tại chưa có quyền thực hiện thao tác này hoặc mặt hàng đã ngưng.</p>;
}
function Cancel({ href }: { href: string }) { return <Link className="btn btn-o" href={href}>Hủy</Link>; }

// Keep the operation's retry keys and prevent an old context's response from navigating a new form.
function useItemAction() {
  const action = useAction();
  const alive = useRef(true);
  const lock = useRef(false);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);
  const run = async <T,>(fn: () => Promise<T>): Promise<T | null> => {
    if (lock.current || !alive.current) return null;
    lock.current = true;
    try {
      const result = await action.run(fn);
      return alive.current ? result : null;
    } finally { lock.current = false; }
  };
  return { ...action, run };
}

function ItemForm({ pa, catalog, filters: f, detail, showLog, create = false, replace = false }: {
  pa: PriceItemApi; catalog: PriceCatalog; filters: ItemFilters; detail?: PriceItemDetail;
  create?: boolean; replace?: boolean; showLog: boolean;
}) {
  const router = useRouter();
  const action = useItemAction();
  const [fields, setFields] = useState(() => initialItemFields(catalog, detail, replace));
  const [local, setLocal] = useState<FieldErrors>({});
  const locked = !create && detail?.has_price === true;
  const errors = itemFieldErrors(action.problem, local);
  const set = (key: keyof ItemFields) => (value: string) => setFields((old) => ({ ...old, [key]: value }));
  const save = async () => {
    const checked = validateItemFields(fields, catalog, create, locked ? detail : undefined);
    setLocal(checked.errors);
    if (!checked.valid || action.busy) return;
    const body = itemBody(checked.values, create);
    const result = await action.run(() => create ? pa.createItem(body) : pa.reviseItem(detail!.code, body));
    if (result) router.push(itemHref(result.code, { ...f, at: result.valid_from }));
  };
  const groupOptions = catalog.groups.map((group) => ({ value: group.code, label: group.name }));
  const unitOptions = catalog.units.map((unit) => ({ value: unit.code, label: unit.name }));
  if (locked && !groupOptions.some((option) => option.value === detail!.group_code)) groupOptions.push({ value: detail!.group_code, label: `Mã ${detail!.group_code} (đã lưu)` });
  if (locked && !unitOptions.some((option) => option.value === detail!.unit_code)) unitOptions.push({ value: detail!.unit_code, label: `Mã ${detail!.unit_code} (đã lưu)` });
  return <DetailPage title={replace ? `Tạo mặt hàng thay thế cho ${detail!.code}` : create ? "Thêm mặt hàng" : `Sửa mặt hàng ${detail!.code}`}
    back={{ href: itemHref("", f), label: "Danh mục mặt hàng" }}
    log={showLog && detail && !create ? <ItemLog detail={detail} /> : undefined}
    actions={<span className="row"><Cancel href={itemHref("", f)} /><button type="button" className="btn btn-p" disabled={action.busy} onClick={save}>Lưu</button></span>}>
    {action.view}
    {locked && <p className="mute">Mặt hàng đã có giá: chỉ sửa tên hiển thị và nơi bán. <Link href={itemHref(`${detail!.code}/thay-the`, f)}>Tạo mặt hàng thay thế</Link> khi cần đổi đơn vị tính, quy cách hoặc nhóm hàng.</p>}
    <div className="grid2">
      <TextField label="Tên loại" required maxLength={100} value={fields.name} onChange={set("name")} error={errors.name} disabled={action.busy} />
      <TextField label="Giống / phẩm cấp" required maxLength={100} value={fields.variety} onChange={set("variety")} error={errors.variety} disabled={locked || action.busy} />
      <SelectField label="Nhóm hàng" required value={fields.group_code} onChange={set("group_code")} options={groupOptions} error={errors.group_code} disabled={locked || action.busy} />
      <SelectField label="Đơn vị tính" required value={fields.unit_code} onChange={set("unit_code")} options={unitOptions} error={errors.unit_code} disabled={locked || action.busy} />
      <TextField label="Quy cách" maxLength={100} value={fields.spec} onChange={set("spec")} error={errors.spec} disabled={locked || action.busy} />
      <TextField label="Nơi bán" maxLength={200} value={fields.sale_place} onChange={set("sale_place")} error={errors.sale_place} disabled={action.busy} />
      <SelectField label="Loại" required value={fields.kind} onChange={set("kind")} options={[{ value: "basic", label: "Cơ bản — mọi xã" }, { value: "local", label: "Địa phương — gán theo xã" }]} error={errors.kind} disabled={locked || action.busy} />
      <ItemDateField label="Hiệu lực từ" value={fields.valid_from} onChange={set("valid_from")} error={errors.valid_from} disabled={action.busy} hint="Để trống: ngày báo giá kế tiếp; máy chủ kiểm ngày và danh mục có hiệu lực." />
      {create && <>
        <ItemDateField label="Hiệu lực đến" value={fields.valid_to} onChange={set("valid_to")} error={errors.valid_to} disabled={action.busy} />
        <TextField label="Thay thế cho (mã)" value={fields.replaces_code} onChange={set("replaces_code")} error={errors.replaces_code} maxLength={7} disabled={replace || action.busy} />
        {fields.kind === "local" && <TextField label="Xã áp dụng (mã xã, cách nhau bởi dấu phẩy)" value={fields.points} onChange={set("points")} error={errors.points} disabled={action.busy} />}
      </>}
    </div>
  </DetailPage>;
}

function GroupForm({ group, pa, filters: f }: { group?: PriceGroup; pa: PriceItemApi; filters: ItemFilters }) {
  const router = useRouter();
  const action = useItemAction();
  const [name, setName] = useState(group?.name ?? "");
  const [local, setLocal] = useState<FieldErrors>({});
  const errors = itemFieldErrors(action.problem, local);
  const save = async () => {
    const checked = validateForm({ name }, { name: { kind: "text", required: true, maxLength: 100 } });
    setLocal(checked.errors);
    if (!checked.valid || action.busy) return;
    const body = { name: checked.values.name.trim() };
    const result = await action.run(() => group ? pa.reviseGroup(group.code, body) : pa.createGroup(body));
    if (result) router.push(itemHref(`nhom-hang/${result.code}`, { ...f, at: result.valid_from }));
  };
  return <DetailPage title={group ? `Sửa nhóm ${group.code}` : "Thêm nhóm hàng"}
    back={{ href: itemHref("", f), label: "Danh mục mặt hàng" }} log={group ? <GroupLog /> : undefined}
    actions={<span className="row"><Cancel href={itemHref("", f)} /><button type="button" className="btn btn-p" onClick={save} disabled={action.busy}>Lưu</button></span>}>
    {action.view}<TextField label="Tên nhóm" required maxLength={100} value={name} onChange={setName} error={errors.name} disabled={action.busy} />
  </DetailPage>;
}

function RetireForm({ detail, pa, filters: f, showLog }: { detail: PriceItemDetail; pa: PriceItemApi; filters: ItemFilters; showLog: boolean }) {
  const router = useRouter();
  const action = useItemAction();
  const [fields, setFields] = useState({ valid_to: "", reason: "" });
  const [local, setLocal] = useState<FieldErrors>({});
  const errors = itemFieldErrors(action.problem, local, "valid_to");
  const save = async () => {
    const checked = validateForm(fields, { valid_to: { kind: "text", maxLength: 10 }, reason: { kind: "text", required: true, maxLength: 500 } });
    if (fields.valid_to && !isCalendarDay(fields.valid_to)) checked.errors.valid_to = "Ngày không hợp lệ.";
    setLocal(checked.errors);
    if (Object.keys(checked.errors).length || action.busy) return;
    const result = await action.run(() => pa.retireItem(detail.code, { reason: fields.reason.trim(), ...(fields.valid_to ? { valid_to: fields.valid_to } : {}) }));
    if (result) router.push(itemHref(result.code, { ...f, at: result.valid_from }));
  };
  return <DetailPage title={`Ngưng ${detail.code} — ${itemLabel(detail)}`} back={{ href: itemHref("", f), label: "Danh mục mặt hàng" }} log={showLog ? <ItemLog detail={detail} /> : undefined}
    actions={<span className="row"><Cancel href={itemHref("", f)} /><button type="button" className="btn btn-d" onClick={save} disabled={action.busy}>Ngưng</button></span>}>
    {action.view}
    <ItemDateField label="Hiệu lực đến" value={fields.valid_to} onChange={(valid_to) => setFields({ ...fields, valid_to })} error={errors.valid_to} disabled={action.busy} hint="Để trống: hết ngày trước ngày báo giá kế tiếp. Báo cáo các ngày trước vẫn giữ mặt hàng." />
    <TextField label="Lý do" required multiline maxLength={500} value={fields.reason} onChange={(reason) => setFields({ ...fields, reason })} error={errors.reason} disabled={action.busy} />
  </DetailPage>;
}
