"use client";

import {
  validatePointFields,
  type FieldErrors,
  type PointFields,
} from "./point-form";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField, TextField, validateForm } from "@/components/form";
import { DetailPage, LabelValueBlock } from "@/components/patterns/detail";
import { LoadState, useLoad } from "@/kn/cycle/common";
import {
  createPricePointApi,
  dmy,
  officerLabel,
  POINT_STATE_LABEL,
  type PricePointApi,
  type PricePointDetail,
  type PriceRegion,
} from "./api";
import {
  pointFilters,
  pointHref,
  vnPointToday,
  type PointFilters,
  type PointRoute,
} from "./point-routes";
import {
  OfficerHistory,
  PointDateField,
  PointLog,
  RegionLog,
  RegionSummary,
  pointFieldErrors,
} from "./point-view";
import { usePointAction } from "./point-action";
import { RetireConfirmation } from "./point-retire-dialog";

export function PricePointDetailScreen({ route }: { route: PointRoute }) {
  const auth = useAuth();
  const query = useSearchParams();
  const filters = pointFilters(query, vnPointToday());
  const pa = useMemo(() => createPricePointApi(auth.api), [auth.api]);
  const key = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}:${JSON.stringify(route)}:${JSON.stringify(filters)}`;
  return <PointData key={key} route={route} pa={pa} filters={filters} />;
}
function PointData({
  route,
  pa,
  filters: f,
}: {
  route: PointRoute;
  pa: PricePointApi;
  filters: PointFilters;
}) {
  const [nonce, setNonce] = useState(0);
  const status = useLoad(() => pa.status(), [pa]);
  const regions = useLoad(() => pa.regions(f.at), [pa, f.at, nonce]);
  const point = useLoad(
    route.kind === "point" ? () => pa.point(route.code, f.at) : null,
    [pa, route.kind, "code" in route ? route.code : "", f.at, nonce],
  );
  const canManage = status.data?.can_manage === true;
  const back = { href: pointHref("", f), label: "Danh mục xã báo giá" };
  return (
    <LoadState s={status}>
      {() => (
        <LoadState s={regions}>
          {(r) => {
            if (route.kind === "point-new")
              return canManage ? (
                <PointForm
                  mode="create"
                  pa={pa}
                  regions={r.regions}
                  filters={f}
                  canManage={canManage}
                />
              ) : (
                <ReadOnly back={back} title="Thêm xã báo giá" />
              );
            if (route.kind === "region-new")
              return canManage ? (
                <RegionForm pa={pa} filters={f} canManage={canManage} />
              ) : (
                <ReadOnly back={back} title="Thêm nhóm vùng" />
              );
            if (route.kind === "region") {
              const region = r.regions.find((v) => v.code === route.code);
              if (!region)
                return (
                  <DetailPage title="Nhóm vùng" back={back}>
                    <p role="alert">
                      Không tìm thấy nhóm vùng trong ngày và phạm vi được cấp.
                    </p>
                  </DetailPage>
                );
              if (route.edit && canManage && region.status === "active")
                return (
                  <RegionForm
                    key={region.version}
                    pa={pa}
                    region={region}
                    filters={f}
                    canManage={canManage}
                  />
                );
              return (
                <DetailPage
                  title={`Nhóm vùng ${region.code}`}
                  back={back}
                  log={<RegionLog />}
                  actions={
                    canManage && region.status === "active" ? (
                      <Link
                        className="btn btn-p"
                        href={pointHref(`nhom-vung/${region.code}/sua`, f)}
                      >
                        Sửa tên
                      </Link>
                    ) : undefined
                  }
                >
                  {route.edit && <ReadOnlyNote />}
                  <RegionSummary region={region} />
                </DetailPage>
              );
            }
            return (
              <LoadState s={point}>
                {(detail) => {
                  if (
                    route.mode === "edit" &&
                    canManage &&
                    detail.point.state !== "retired"
                  )
                    return (
                      <PointForm
                        key={detail.point.version}
                        mode="revise"
                        pa={pa}
                        regions={r.regions}
                        detail={detail}
                        filters={f}
                        canManage={canManage}
                      />
                    );
                  return (
                    <PointDetail
                      pa={pa}
                      onRetired={() => setNonce((value) => value + 1)}
                      detail={detail}
                      filters={f}
                      canManage={canManage}
                      showReadOnly={route.mode !== "view"}
                    />
                  );
                }}
              </LoadState>
            );
          }}
        </LoadState>
      )}
    </LoadState>
  );
}
function ReadOnlyNote() {
  return (
    <p className="alert alert-info" role="note">
      Chỉ xem — bạn chưa có quyền quản lý xã báo giá trong ngữ cảnh hiện tại,
      hoặc bản ghi đã ngưng.
    </p>
  );
}
function ReadOnly({
  title,
  back,
}: {
  title: string;
  back: { href: string; label: string };
}) {
  return (
    <DetailPage title={title} back={back}>
      <ReadOnlyNote />
    </DetailPage>
  );
}
function Cancel({ href }: { href: string }) {
  return (
    <Link className="btn btn-o" href={href}>
      Hủy
    </Link>
  );
}
function PointDetail({
  pa,
  onRetired,
  detail: d,
  filters: f,
  canManage,
  showReadOnly,
}: {
  pa: PricePointApi;
  onRetired: () => void;
  detail: PricePointDetail;
  filters: PointFilters;
  canManage: boolean;
  showReadOnly: boolean;
}) {
  const p = d.point;
  const [retire, setRetire] = useState(false);
  return (
    <DetailPage
      title={`Xã ${p.code} — ${p.name}`}
      back={{ href: pointHref("", f), label: "Danh mục xã báo giá" }}
      log={<PointLog detail={d} />}
      actions={
        canManage && p.state !== "retired" ? (
          <span className="row">
            <Link className="btn btn-p" href={pointHref(`${p.code}/sua`, f)}>
              Sửa
            </Link>
            <button
              type="button"
              className="btn btn-d"
              onClick={() => setRetire(true)}
            >
              Ngưng
            </button>
          </span>
        ) : undefined
      }
    >
      {showReadOnly && <ReadOnlyNote />}
      <LabelValueBlock
        title="Thông tin xã báo giá"
        items={[
          { label: "Mã xã", value: p.code },
          { label: "Xã", value: p.name },
          { label: "Mã ĐVHC", value: p.admin_unit_code },
          { label: "Nhóm vùng", value: p.region_name || p.region_code },
          { label: "Trạng thái", value: POINT_STATE_LABEL[p.state] },
          { label: "Ngày bắt đầu báo giá", value: dmy(p.since) },
          { label: "Hiệu lực đến", value: dmy(p.valid_to) },
          {
            label: "Cán bộ Xã phụ trách",
            value: officerLabel(
              p.officer_ref,
              p.officer_name,
              "Chưa có cán bộ Xã",
            ),
          },
          {
            label: "Cán bộ Tỉnh",
            value: officerLabel(p.owner_ref, p.owner_name, "Chưa phân công"),
          },
          { label: "Số mặt hàng", value: p.item_count },
        ]}
      />
      {!p.admin_unit_valid && (
        <p className="alert alert-warn">
          ĐVHC hết hiệu lực
          {p.successor_codes.length
            ? `; mã kế thừa: ${p.successor_codes.join(", ")}`
            : ""}
          .
        </p>
      )}
      <OfficerHistory detail={d} />
      {retire && (
        <RetireConfirmation
          value={{ kind: "point", value: p }}
          pa={pa}
          canManage={canManage}
          onClose={(changed) => {
            setRetire(false);
            if (changed) onRetired();
          }}
        />
      )}
    </DetailPage>
  );
}
function PointForm({
  mode,
  pa,
  regions,
  detail,
  filters: f,
  canManage,
}: {
  mode: "create" | "revise";
  pa: PricePointApi;
  regions: PriceRegion[];
  detail?: PricePointDetail;
  filters: PointFilters;
  canManage: boolean;
}) {
  const p = detail?.point;
  const act = usePointAction();
  const router = useRouter();
  const [b, setB] = useState<PointFields>({
    admin_unit_code: mode === "create" ? "" : (p?.admin_unit_code ?? ""),
    region_code:
      p?.region_code ?? regions.find((v) => v.status === "active")?.code ?? "",
    valid_from: "",
    note: "",
    reason: "",
  });
  const [local, setLocal] = useState<FieldErrors>({});
  const unitDay = b.valid_from || f.at;
  const units = useLoad(
    () => pa.adminUnits(unitDay).then((data) => ({ ...data, at: unitDay })),
    [pa, unitDay],
  );
  const errors = pointFieldErrors(act.problem, local);
  const set = (key: keyof PointFields) => (value: string) =>
    setB((old) => ({ ...old, [key]: value }));
  const save = async () => {
    if (!canManage || act.busy || units.data?.at !== unitDay) return;
    const v = validatePointFields(b, mode);
    setLocal(v.errors);
    if (!v.valid) return;
    const unit = units.data?.admin_units.find(
      (u) => u.code === b.admin_unit_code,
    );
    if (!unit || (unit.taken_by && unit.taken_by !== p?.code)) {
      setLocal({
        admin_unit_code: "Chọn ĐVHC còn hiệu lực và chưa gắn xã báo giá khác.",
      });
      return;
    }
    if (
      !regions.some((r) => r.status === "active" && r.code === b.region_code)
    ) {
      setLocal({ region_code: "Chọn nhóm vùng đang hoạt động." });
      return;
    }
    const body = {
      admin_unit_code: b.admin_unit_code,
      region_code: b.region_code,
      valid_from: b.valid_from || undefined,
      note: b.note.trim() || undefined,
    };
    const result = await act.run(() =>
      mode === "create" ? pa.createPoint(body) : pa.revisePoint(p!.code, body),
    );
    if (result) router.push(pointHref(result.code, f));
  };
  const back = { href: pointHref("", f), label: "Danh mục xã báo giá" };
  return (
    <DetailPage
      title={
        mode === "create" ? "Thêm xã báo giá" : `Sửa xã ${p?.code} — ${p?.name}`
      }
      back={back}
      log={detail ? <PointLog detail={detail} /> : undefined}
      actions={
        <span className="row">
          <Cancel href={back.href} />
          <button
            type="button"
            className="btn btn-p"
            disabled={
              !canManage ||
              act.busy ||
              units.loading ||
              !!units.problem ||
              units.data?.at !== unitDay
            }
            onClick={save}
          >
            Lưu
          </button>
        </span>
      }
    >
      {act.view}
      <p className="muted">
        Mã do hệ thống tự sinh; tên xã lấy theo ĐVHC. Dữ liệu cũ giữ theo phiên
        bản tại ngày báo giá.
      </p>
      <LoadState s={units}>
        {(u) => (
          <SelectField
            label={
              mode === "create" ? "ĐVHC (xã)" : "ĐVHC (xã) / chuyển mã mới"
            }
            required
            value={b.admin_unit_code}
            onChange={set("admin_unit_code")}
            error={errors.admin_unit_code}
            options={u.admin_units
              .filter((a) => !a.taken_by || a.taken_by === p?.code)
              .map((a) => ({ value: a.code, label: `${a.code} — ${a.name}` }))}
          />
        )}
      </LoadState>
      <SelectField
        label="Nhóm vùng"
        required
        value={b.region_code}
        onChange={set("region_code")}
        error={errors.region_code}
        options={regions
          .filter((r) => r.status === "active")
          .map((r) => ({ value: r.code, label: r.name }))}
      />
      <PointDateField
        label={mode === "create" ? "Ngày bắt đầu báo giá" : "Áp dụng từ ngày"}
        value={b.valid_from}
        onChange={set("valid_from")}
        error={errors.valid_from}
      />
      {mode === "revise" && (
        <TextField
          label="Ghi chú (vd sắp xếp ĐVHC)"
          multiline
          maxLength={500}
          value={b.note}
          onChange={set("note")}
          error={errors.note}
        />
      )}
    </DetailPage>
  );
}
function RegionForm({
  pa,
  region,
  filters: f,
  canManage,
}: {
  pa: PricePointApi;
  region?: PriceRegion;
  filters: PointFilters;
  canManage: boolean;
}) {
  const [name, setName] = useState(region?.name ?? "");
  const [local, setLocal] = useState<FieldErrors>({});
  const act = usePointAction();
  const router = useRouter();
  const errors = pointFieldErrors(act.problem, local);
  const save = async () => {
    if (!canManage || act.busy) return;
    const checked = validateForm(
      { name },
      { name: { kind: "text", required: true, maxLength: 100 } },
    );
    setLocal(checked.errors);
    if (!checked.valid) return;
    const result = await act.run(() =>
      region
        ? pa.reviseRegion(region.code, { name: name.trim() })
        : pa.createRegion({ name: name.trim() }),
    );
    if (result)
      router.push(
        pointHref(`nhom-vung/${result.code}`, { ...f, tab: "regions" }),
      );
  };
  const back = {
    href: pointHref("", { ...f, tab: "regions" }),
    label: "Nhóm vùng",
  };
  return (
    <DetailPage
      title={region ? `Sửa nhóm vùng ${region.code}` : "Thêm nhóm vùng"}
      back={back}
      log={region ? <RegionLog /> : undefined}
      actions={
        <span className="row">
          <Cancel href={back.href} />
          <button
            type="button"
            className="btn btn-p"
            disabled={!canManage || act.busy}
            onClick={save}
          >
            Lưu
          </button>
        </span>
      }
    >
      {act.view}
      <TextField
        label="Tên nhóm vùng"
        required
        maxLength={100}
        value={name}
        onChange={setName}
        error={errors.name}
      />
    </DetailPage>
  );
}
