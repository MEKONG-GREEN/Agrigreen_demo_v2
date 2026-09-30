"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { LogisticsVessel, LogisticsVesselInput } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Chip, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { normalizeVesselCode, RATE_TYPE_LABEL, REGISTRATION_LABEL, VESSEL_KIND_LABEL } from "@/lib/g4labels";
import { formatDecimalString, formatPrice } from "@/lib/money";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

interface Form {
  code: string;
  kind: "ghe" | "sa_lan" | "truck";
  name: string;
  owner_ref: string;
  registered_tons: string;
  straw_payload_tons: string;
  registration_expiry: string;
  rate_type: "" | "per_ton" | "per_trip" | "per_ton_km";
  rate_vnd: string;
  status: "active" | "inactive";
  reason: string;
}
const EMPTY: Form = { code: "", kind: "ghe", name: "", owner_ref: "", registered_tons: "", straw_payload_tons: "", registration_expiry: "", rate_type: "", rate_vnd: "", status: "active", reason: "" };
const TONS = /^\d+(\.\d{1,3})?$/;
const PER: Record<string, "tấn" | "chuyến" | "t·km"> = { per_ton: "tấn", per_trip: "chuyến", per_ton_km: "t·km" };

function formOf(v: LogisticsVessel): Form {
  return { code: v.code, kind: v.kind, name: v.name ?? "", owner_ref: v.owner_ref ?? "", registered_tons: v.registered_tons ?? "", straw_payload_tons: v.straw_payload_tons ?? "", registration_expiry: v.registration_expiry ?? "", rate_type: (v.rate_type ?? "") as Form["rate_type"], rate_vnd: v.rate_vnd ?? "", status: v.status, reason: "" };
}

function formError(f: Form, revising: boolean): string | null {
  if (!normalizeVesselCode(f.code)) return "Nhập số hiệu";
  for (const k of ["registered_tons", "straw_payload_tons"] as const) if (f[k] && !TONS.test(f[k].trim())) return "Tải trọng: số tấn, tối đa 3 số lẻ";
  if (f.rate_type && !/^\d+(\.\d{1,4})?$/.test(f.rate_vnd.trim())) return "Có loại đơn giá thì đơn giá > 0 (GH-01)";
  if (revising && !validReason(f.reason)) return "Sửa phương tiện cần lý do ≥ 10 ký tự";
  return null;
}

function toBody(f: Form, expected?: number): LogisticsVesselInput {
  return {
    code: normalizeVesselCode(f.code),
    kind: f.kind,
    ...(f.name.trim() ? { name: f.name.trim() } : {}),
    owner_ref: f.owner_ref.trim() || null,
    registered_tons: f.registered_tons.trim() || null,
    straw_payload_tons: f.straw_payload_tons.trim() || null,
    registration_expiry: f.registration_expiry || null,
    rate_type: f.rate_type,
    rate_vnd: f.rate_type ? f.rate_vnd.trim() : null,
    status: f.status,
    ...(expected !== undefined ? { reason: f.reason.trim(), expected_version: expected } : {}),
  };
}

export default function VesselsPage() {
  const { api } = useAuth();
  const list = useAsync(async () => (await unwrap(api.GET("/v1/logistics/vessels"))).items, [api]);
  const [edit, setEdit] = useState<{ v: LogisticsVessel | null; f: Form } | null>(null);
  const act = useAction();
  const revising = !!edit?.v;
  const err = edit ? formError(edit.f, revising) : null;
  const set = (x: Partial<Form>) => edit && setEdit({ ...edit, f: { ...edit.f, ...x } });
  const save = async () => {
    if (!edit) return;
    const r = edit.v
      ? await act.run(() => unwrap(api.POST("/v1/logistics/vessels/{id}/versions", { params: { path: { id: edit.v!.vessel_id }, header: idem() }, body: toBody(edit.f, edit.v!.version) })), (x) => `Đã lưu phiên bản ${x.vessel.version} của ${x.vessel.code}`)
      : await act.run(() => unwrap(api.POST("/v1/logistics/vessels", { params: { header: idem() }, body: toBody(edit.f) })), (x) => `Đã thêm phương tiện ${x.vessel.code} (số hiệu đã chuẩn hoá)`);
    if (r) {
      setEdit(null);
      list.reload();
    }
  };
  return (
    <div className="stack">
      <PageHead
        title="Danh mục ghe / xe"
        subtitle="Số hiệu chuẩn hoá (bỏ khoảng trắng, chữ hoa) và duy nhất (GH-01); đăng kiểm hết / sắp hết hạn chỉ CẢNH BÁO, không chặn xếp hàng (GH-02); đơn giá thuê dùng cho cước thực (GH-03). Sửa = phiên bản mới có lý do."
        actions={
          <button type="button" className="btn btn-p btn-sm" onClick={() => setEdit({ v: null, f: EMPTY })}>
            Thêm phương tiện
          </button>
        }
      />
      <ActionStatus a={act} />
      {edit ? (
        <Section title={edit.v ? `Sửa ${edit.v.code} (phiên bản ${edit.v.version} → ${edit.v.version + 1})` : "Thêm phương tiện"}>
          <div className="stack">
            <div className="grid3">
              <TextField label="Số hiệu" value={edit.f.code} onChange={(code) => set({ code })} hint={edit.f.code ? `Lưu thành: ${normalizeVesselCode(edit.f.code)}` : "vd TG-12345"} />
              <SelectField label="Loại phương tiện" value={edit.f.kind} onChange={(kind) => set({ kind: kind as Form["kind"] })} options={Object.entries(VESSEL_KIND_LABEL).map(([value, label]) => ({ value, label }))} />
              <TextField label="Tên gọi" value={edit.f.name} onChange={(name) => set({ name })} />
              <TextField label="Mã chủ ghe (vault)" value={edit.f.owner_ref} onChange={(owner_ref) => set({ owner_ref })} hint="UUID ở vault MKG (RB-3) — không nhập tên" />
              <TextField label="Tải đăng kiểm (t)" value={edit.f.registered_tons} onChange={(registered_tons) => set({ registered_tons })} inputMode="decimal" />
              <TextField label="Tải rơm thực chở (t)" value={edit.f.straw_payload_tons} onChange={(straw_payload_tons) => set({ straw_payload_tons })} inputMode="decimal" hint="Trống = tham số #51 (90 t, tạm) với ghe" />
              <TextField label="Hạn đăng kiểm" type="date" value={edit.f.registration_expiry} onChange={(registration_expiry) => set({ registration_expiry })} />
              <SelectField label="Loại đơn giá thuê" value={edit.f.rate_type} onChange={(rate_type) => set({ rate_type: rate_type as Form["rate_type"] })} options={Object.entries(RATE_TYPE_LABEL).map(([value, label]) => ({ value, label }))} />
              {edit.f.rate_type ? <TextField label="Đơn giá thuê (VND)" value={edit.f.rate_vnd} onChange={(rate_vnd) => set({ rate_vnd })} inputMode="decimal" /> : null}
              <SelectField label="Tình trạng" value={edit.f.status} onChange={(status) => set({ status: status as Form["status"] })} options={[{ value: "active", label: "Đang dùng" }, { value: "inactive", label: "Ngừng dùng" }]} />
            </div>
            {revising ? <TextField label="Lý do sửa" value={edit.f.reason} onChange={(reason) => set({ reason })} hint="Ít nhất 10 ký tự" /> : null}
            {err ? <p className="small mute">{err}</p> : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!!err || act.busy} onClick={() => void save()}>
                {revising ? "Lưu phiên bản mới" : "Thêm vào danh mục"}
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setEdit(null)}>
                Huỷ
              </button>
            </div>
          </div>
        </Section>
      ) : null}
      <Section title="Danh mục">
        {list.loading ? <Loading /> : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.data && !list.data.length ? <Empty>Chưa có phương tiện.</Empty> : null}
        {list.data?.length ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Danh mục phương tiện">
              <thead>
                <tr>
                  <th>Số hiệu</th>
                  <th>Loại</th>
                  <th>Tên</th>
                  <th className="num">Tải rơm (t)</th>
                  <th>Đăng kiểm</th>
                  <th>Đơn giá thuê</th>
                  <th>Tình trạng</th>
                  <th className="num">Phiên bản</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {list.data.map((v) => (
                  <tr key={v.vessel_id}>
                    <td className="mono">{v.code}</td>
                    <td>{VESSEL_KIND_LABEL[v.kind]}</td>
                    <td>
                      {v.name ?? "—"} <span className="small mute">chủ <Ref id={v.owner_ref} /></span>
                    </td>
                    <td className="num">{v.straw_payload_tons ? formatDecimalString(v.straw_payload_tons) : v.kind === "truck" ? "—" : "90 (#51)"}</td>
                    <td>
                      <Chip status={v.registration_status} label={REGISTRATION_LABEL[v.registration_status]} /> {formatDate(v.registration_expiry)}
                      {v.registration_days_left !== null && v.registration_days_left !== undefined ? <span className="small mute"> ({v.registration_days_left} ngày)</span> : null}
                    </td>
                    <td>{v.rate_type ? formatPrice(v.rate_vnd, PER[v.rate_type]) : <span className="mute small">Đơn giá tham số</span>}</td>
                    <td>
                      <Chip status={v.status} label={v.status === "active" ? "Đang dùng" : "Ngừng dùng"} />
                    </td>
                    <td className="num">{v.version}</td>
                    <td>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setEdit({ v, f: formOf(v) })} aria-label={`Sửa ${v.code}`}>
                        Sửa
                      </button>
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
