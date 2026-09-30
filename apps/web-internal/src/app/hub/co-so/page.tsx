"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { Facility, FacilitySpecInput, FacilityVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, Ref, Section, SelectField, SelfHint, TextField, useAction, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { formatTons, isKg } from "@/lib/kg";
import { recent, remember } from "@/lib/recent";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const KIND_LABEL: Record<string, string> = { hub: "Hub", warehouse: "Kho", yard: "Bãi" };

interface SpecForm {
  code: string;
  name: string;
  kind: string;
  province_code: string;
  lat: string;
  lng: string;
  capacity_kg: string;
  status: string;
  effective_from: string;
  reason: string;
}

const EMPTY: SpecForm = { code: "", name: "", kind: "hub", province_code: "", lat: "", lng: "", capacity_kg: "", status: "active", effective_from: new Date().toISOString().slice(0, 10), reason: "" };

function toSpec(f: SpecForm): FacilitySpecInput {
  return {
    code: f.code.trim().toUpperCase(),
    name: f.name.trim(),
    kind: f.kind as FacilitySpecInput["kind"],
    province_code: f.province_code.trim(),
    location: { lat: Number(f.lat), lng: Number(f.lng) },
    capacity_kg: f.capacity_kg.trim(),
    status: f.status as FacilitySpecInput["status"],
    effective_from: new Date(`${f.effective_from}T00:00:00+07:00`).toISOString(),
    ...(f.reason.trim() ? { reason: f.reason.trim() } : {}),
  };
}

function fromFacility(f: Facility): SpecForm {
  return {
    code: f.code ?? "",
    name: f.name ?? "",
    kind: f.kind ?? "hub",
    province_code: f.province_code ?? "",
    lat: String(f.location?.lat ?? ""),
    lng: String(f.location?.lng ?? ""),
    capacity_kg: f.capacity_kg ?? "",
    status: f.status ?? "active",
    effective_from: new Date().toISOString().slice(0, 10),
    reason: "",
  };
}

function SpecFields({ v, set, withReason }: { v: SpecForm; set: (p: Partial<SpecForm>) => void; withReason: boolean }) {
  return (
    <div className="stack">
      <div className="grid3">
        <TextField label="Mã cơ sở" value={v.code} onChange={(code) => set({ code })} hint="In hoa, số, gạch — giữ mãi" />
        <TextField label="Tên cơ sở" value={v.name} onChange={(name) => set({ name })} />
        <SelectField label="Loại cơ sở" value={v.kind} onChange={(kind) => set({ kind })} options={Object.entries(KIND_LABEL).map(([value, label]) => ({ value, label }))} />
        <TextField label="Mã tỉnh" value={v.province_code} onChange={(province_code) => set({ province_code })} hint="2 chữ số" />
        <TextField label="Vĩ độ (WGS84)" value={v.lat} onChange={(lat) => set({ lat })} inputMode="decimal" />
        <TextField label="Kinh độ (WGS84)" value={v.lng} onChange={(lng) => set({ lng })} inputMode="decimal" />
        <TextField label="Sức chứa (kg)" value={v.capacity_kg} onChange={(capacity_kg) => set({ capacity_kg })} inputMode="decimal" hint="Chuỗi thập phân, tối đa 3 chữ số" />
        <SelectField label="Trạng thái" value={v.status} onChange={(status) => set({ status })} options={[{ value: "active", label: "Đang dùng" }, { value: "inactive", label: "Ngừng dùng (không xoá cứng)" }]} />
        <TextField label="Hiệu lực từ ngày" type="date" value={v.effective_from} onChange={(effective_from) => set({ effective_from })} />
      </div>
      {withReason ? <TextField label="Lý do thay đổi" value={v.reason} onChange={(reason) => set({ reason })} multiline hint="Bắt buộc từ phiên bản 2, ít nhất 10 ký tự" /> : null}
    </div>
  );
}

function specError(v: SpecForm, needReason: boolean): string | null {
  if (!v.code || !v.name || !v.province_code) return "Nhập đủ mã, tên, mã tỉnh";
  if (!isKg(v.capacity_kg)) return "Sức chứa phải là số kg (tối đa 3 chữ số thập phân)";
  if (!Number.isFinite(Number(v.lat)) || !Number.isFinite(Number(v.lng)) || !v.lat || !v.lng) return "Nhập tọa độ WGS84";
  if (needReason && !validReason(v.reason)) return "Lý do thay đổi ít nhất 10 ký tự";
  return null;
}

function Versions({ facilityId, onDecided }: { facilityId: string; onDecided: () => void }) {
  const { adminApi, principal } = useAuth();
  const canDecide = hasRole(principal, ROLES.hubApprover);
  const versions = useAsync(async () => (await unwrap(adminApi.GET("/v1/hub/facilities/{id}/versions", { params: { path: { id: facilityId } } }))).items as FacilityVersion[], [adminApi, facilityId]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const decide = (v: FacilityVersion, decision: "approved" | "rejected") =>
    act
      .run(
        () =>
          unwrap(
            adminApi.POST("/v1/hub/facilities/{id}/versions/{version}/decision", {
              params: { path: { id: facilityId, version: v.version! }, header: idem() },
              body: decision === "rejected" ? { decision, reason: reason.trim() } : { decision },
            }),
          ),
        (r) => (decision === "approved" ? `Đã duyệt phiên bản ${v.version} — xuất van hub_facility + hub_capacity (${r.publications?.length ?? 0} bản ghi)` : `Đã từ chối phiên bản ${v.version}`),
      )
      .then((r) => {
        if (r) {
          versions.reload();
          onDecided();
        }
      });
  if (versions.loading) return <Loading />;
  if (versions.error) return <ProblemAlert problem={versions.error} onRetry={versions.reload} />;
  return (
    <div className="stack">
      <ActionStatus a={act} />
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Lịch sử phiên bản">
          <thead>
            <tr>
              <th>Phiên bản</th>
              <th>Trạng thái</th>
              <th>Tên / loại</th>
              <th className="num">Sức chứa</th>
              <th>Hiệu lực</th>
              <th>Lý do</th>
              <th>Người đề xuất</th>
              <th>Thao tác</th>
            </tr>
          </thead>
          <tbody>
            {versions.data?.map((v) => (
              <tr key={v.id}>
                <td>v{v.version}</td>
                <td>
                  <Chip status={v.state} />
                </td>
                <td>
                  {v.name} · {KIND_LABEL[v.kind ?? ""] ?? v.kind} {v.status === "inactive" ? <Chip status="cancelled" label="Ngừng dùng" /> : null}
                </td>
                <td className="num">{formatTons(v.capacity_kg)}</td>
                <td>{formatDate(v.effective_from)}</td>
                <td>{v.change_reason ?? "—"}</td>
                <td>
                  <Ref id={v.proposed_by} />
                </td>
                <td>
                  {v.state === "pending" && canDecide ? (
                    <div className="stack" style={{ gap: 6 }}>
                      <SelfHint proposer={v.proposed_by} />
                      <div className="row">
                        <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void decide(v, "approved")}>
                          Duyệt phiên bản {v.version}
                        </button>
                        <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(reason)} onClick={() => void decide(v, "rejected")}>
                          Từ chối
                        </button>
                      </div>
                    </div>
                  ) : v.decided_at ? (
                    <span className="small mute">{formatDateTime(v.decided_at)}</span>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {versions.data?.some((v) => v.state === "pending") && canDecide ? (
        <TextField label="Lý do từ chối" value={reason} onChange={setReason} hint="Bắt buộc khi từ chối, ít nhất 10 ký tự" />
      ) : null}
    </div>
  );
}

export default function FacilitiesPage() {
  const { adminApi, principal } = useAuth();
  const canPropose = hasRole(principal, ROLES.hubOperator);
  const facilities = useFacilities();
  const [selected, setSelected] = useState("");
  const [mode, setMode] = useState<"none" | "new" | "change">("none");
  const [form, setForm] = useState<SpecForm>(EMPTY);
  const [touched, setTouched] = useState(false);
  const act = useAction();
  const set = (p: Partial<SpecForm>) => setForm((f) => ({ ...f, ...p }));
  const pendingNew = recent("facility").filter((r) => !facilities.data?.some((f) => f.id === r.id));
  const current = facilities.data?.find((f) => f.id === selected);
  const err = specError(form, mode === "change");

  const submit = async () => {
    setTouched(true);
    if (err) return;
    const body = toSpec(form);
    const r =
      mode === "new"
        ? await act.run(() => unwrap(adminApi.POST("/v1/hub/facilities", { params: { header: idem() }, body })), () => `Đã gửi đề xuất cơ sở ${body.code} (phiên bản 1) — chờ người khác duyệt`)
        : await act.run(
            () => unwrap(adminApi.POST("/v1/hub/facilities/{id}/versions", { params: { path: { id: selected }, header: idem() }, body })),
            (v) => `Đã gửi đề xuất phiên bản ${v.version} — chờ người khác duyệt`,
          );
    if (r) {
      if (mode === "new") {
        remember("facility", r.facility_id!, `${body.code} (chờ duyệt)`);
        setSelected(r.facility_id!);
      }
      setMode("none");
      setTouched(false);
    }
  };

  return (
    <div className="stack">
      <PageHead
        title="Cơ sở & phiên bản"
        subtitle="Hub, kho, bãi là dữ liệu chủ có phiên bản: người đề xuất (hub_operator) ≠ người duyệt (hub_approver). Duyệt → xuất van hub_facility và hub_capacity."
        actions={
          canPropose ? (
            <button
              type="button"
              className="btn btn-p btn-sm"
              onClick={() => {
                setForm(EMPTY);
                setMode("new");
                act.clear();
              }}
            >
              Đề xuất cơ sở mới
            </button>
          ) : null
        }
      />
      <ActionStatus a={act} />
      {mode !== "none" ? (
        <Section title={mode === "new" ? "Đề xuất cơ sở mới" : `Đề xuất thay đổi ${current?.code ?? ""}`}>
          <SpecFields v={form} set={set} withReason={mode === "change"} />
          {touched && err ? (
            <p className="err" role="alert" style={{ color: "var(--red)", marginTop: 8 }}>
              {err}
            </p>
          ) : null}
          <div className="row" style={{ marginTop: 12 }}>
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void submit()}>
              Gửi đề xuất
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setMode("none")}>
              Huỷ
            </button>
          </div>
        </Section>
      ) : null}
      <Section title="Cơ sở hiện hành (đã duyệt)">
        {facilities.loading ? <Loading /> : null}
        {facilities.error ? <ProblemAlert problem={facilities.error} onRetry={facilities.reload} /> : null}
        {facilities.data?.length === 0 ? <Empty>Chưa có cơ sở nào được duyệt.</Empty> : null}
        {facilities.data?.length ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Mã</th>
                  <th>Tên</th>
                  <th>Loại</th>
                  <th>Tỉnh</th>
                  <th className="num">Sức chứa</th>
                  <th>Phiên bản</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {facilities.data.map((f) => (
                  <tr key={f.id} aria-selected={f.id === selected}>
                    <td className="mono">{f.code}</td>
                    <td>{f.name}</td>
                    <td>{KIND_LABEL[f.kind ?? ""] ?? f.kind}</td>
                    <td>{f.province_code}</td>
                    <td className="num">{formatTons(f.capacity_kg)}</td>
                    <td>v{f.version}</td>
                    <td className="row">
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(f.id!)} aria-label={`Xem phiên bản ${f.code}`}>
                        Phiên bản
                      </button>
                      {canPropose ? (
                        <button
                          type="button"
                          className="btn btn-o btn-sm"
                          aria-label={`Đề xuất thay đổi ${f.code}`}
                          onClick={() => {
                            setSelected(f.id!);
                            setForm(fromFacility(f));
                            setMode("change");
                            act.clear();
                          }}
                        >
                          Đề xuất thay đổi
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {pendingNew.length ? (
          <div className="row" style={{ marginTop: 12 }}>
            <span className="small mute">Đề xuất mới chưa duyệt:</span>
            {pendingNew.map((p) => (
              <button key={p.id} type="button" className="btn btn-o btn-sm" onClick={() => setSelected(p.id)}>
                {p.label}
              </button>
            ))}
          </div>
        ) : null}
      </Section>
      {selected ? (
        <Section title={`Lịch sử phiên bản ${current?.code ?? recent("facility").find((r) => r.id === selected)?.label ?? ""}`}>
          <Versions key={selected} facilityId={selected} onDecided={facilities.reload} />
        </Section>
      ) : null}
    </div>
  );
}
