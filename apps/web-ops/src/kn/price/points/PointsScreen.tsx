"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import {
  DataTable,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import {
  createPricePointApi,
  officerLabel,
  POINT_STATE_LABEL,
  type PricePoint,
  type PricePointApi,
  type PriceRegion,
} from "./api";
import {
  pointFilters,
  pointHref,
  vnPointToday,
  type PointFilters,
} from "./point-routes";
import { RetireConfirmation } from "./point-retire-dialog";

export function usePricePointApi(): PricePointApi {
  const { api } = useAuth();
  return useMemo(() => createPricePointApi(api), [api]);
}
export function PointsScreen() {
  const auth = useAuth();
  const search = useSearchParams();
  const filters = pointFilters(search, vnPointToday());
  const session = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}`;
  return (
    <PointList
      key={`${session}:${JSON.stringify(filters)}`}
      filters={filters}
    />
  );
}
function PointList({ filters: f }: { filters: PointFilters }) {
  const pa = usePricePointApi();
  const router = useRouter();
  const [nonce, setNonce] = useState(0);
  const [retire, setRetire] = useState<
    | { kind: "point"; value: PricePoint }
    | { kind: "region"; value: PriceRegion }
    | null
  >(null);
  const status = useLoad(() => pa.status(), [pa]);
  const regions = useLoad(() => pa.regions(f.at), [pa, f.at, nonce]);
  const points = useLoad(
    f.tab === "points"
      ? () => pa.points({ at: f.at, region: f.region || undefined })
      : null,
    [pa, f.at, f.region, f.tab, nonce],
  );
  const canManage = status.data?.can_manage ?? false;
  const navigate = (p: Partial<PointFilters>) =>
    router.replace(pointHref("", { ...f, ...p }));
  const path = (p: string, tab = f.tab) => pointHref(p, { ...f, tab });
  const columns: Column<PricePoint>[] = [
    {
      key: "code",
      header: "Mã",
      cell: (p) => <Link href={path(p.code)}>{p.code}</Link>,
    },
    { key: "name", header: "Xã", cell: (p) => p.name },
    {
      key: "unit",
      header: "Mã ĐVHC",
      cell: (p) => (
        <>
          {p.admin_unit_code}
          {!p.admin_unit_valid && (
            <StatusPill tone="warn">
              Hết hiệu lực
              {p.successor_codes.length
                ? ` → ${p.successor_codes.join(", ")}`
                : ""}
            </StatusPill>
          )}
        </>
      ),
    },
    {
      key: "region",
      header: "Nhóm vùng",
      cell: (p) => p.region_name || p.region_code,
    },
    {
      key: "officer",
      header: "Cán bộ Xã phụ trách",
      cell: (p) =>
        officerLabel(p.officer_ref, p.officer_name, "Chưa có cán bộ Xã"),
    },
    {
      key: "owner",
      header: "Cán bộ Tỉnh",
      cell: (p) => officerLabel(p.owner_ref, p.owner_name, "Chưa phân công"),
    },
    { key: "items", header: "Số mặt hàng", cell: (p) => p.item_count },
    {
      key: "state",
      header: "Trạng thái",
      cell: (p) => (
        <StatusPill
          tone={
            p.state === "active"
              ? "ok"
              : p.state === "pending"
                ? "info"
                : "neutral"
          }
        >
          {POINT_STATE_LABEL[p.state]}
        </StatusPill>
      ),
    },
    {
      key: "acts",
      header: "Thao tác",
      cell: (p) => (
        <span className="row">
          <Link className="btn btn-o btn-sm" href={path(p.code)}>
            Chi tiết
          </Link>
          {canManage && p.state !== "retired" && (
            <>
              <Link className="btn btn-o btn-sm" href={path(`${p.code}/sua`)}>
                Sửa
              </Link>
              <button
                type="button"
                className="btn btn-d btn-sm"
                onClick={() => setRetire({ kind: "point", value: p })}
              >
                Ngưng
              </button>
            </>
          )}
        </span>
      ),
    },
  ];
  return (
    <>
      <PageHeader title="Xã báo giá">
        Mỗi xã báo giá liên kết một mã ĐVHC dùng chung và một nhóm vùng; phân
        công cán bộ theo ngày hiệu lực.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <label htmlFor="pp-at">Tại ngày</label>
        <input
          id="pp-at"
          type="date"
          className="input"
          value={f.at}
          onChange={(e) => e.target.value && navigate({ at: e.target.value })}
        />
        {status.data && !canManage && (
          <span className="muted">Chỉ xem — Admin quản lý danh mục xã.</span>
        )}
      </div>
      <div className="dtabs" role="tablist" aria-label="Xã báo giá">
        {(
          [
            { id: "points", label: "Xã báo giá" },
            { id: "regions", label: "Nhóm vùng" },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={f.tab === t.id}
            onClick={() => navigate({ tab: t.id })}
          >
            {t.label}
          </button>
        ))}
      </div>
      <LoadState s={regions}>
        {(r) =>
          f.tab === "points" ? (
            <>
              <div className="row" style={{ marginBottom: 12 }}>
                <label htmlFor="pp-region">Nhóm vùng</label>
                <select
                  id="pp-region"
                  className="input"
                  value={f.region}
                  onChange={(e) => navigate({ region: e.target.value })}
                >
                  <option value="">Toàn tỉnh</option>
                  {r.regions.map((region) => (
                    <option key={region.code} value={region.code}>
                      {region.name}
                    </option>
                  ))}
                </select>
                {canManage && (
                  <Link className="btn btn-p" href={path("them-moi")}>
                    Thêm xã báo giá
                  </Link>
                )}
              </div>
              <LoadState s={points}>
                {(d) => (
                  <DataTable
                    caption="Danh mục xã báo giá"
                    columns={columns}
                    rows={d.points}
                    rowKey={(p) => p.code}
                  />
                )}
              </LoadState>
            </>
          ) : (
            <>
              {canManage && (
                <Link
                  className="btn btn-p"
                  href={path("nhom-vung/them-moi", "regions")}
                >
                  Thêm nhóm vùng
                </Link>
              )}
              <DataTable
                caption="Nhóm vùng"
                columns={[
                  {
                    key: "code",
                    header: "Mã",
                    cell: (r: PriceRegion) => (
                      <Link href={path(`nhom-vung/${r.code}`)}>{r.code}</Link>
                    ),
                  },
                  { key: "name", header: "Tên nhóm vùng", cell: (r) => r.name },
                  {
                    key: "points",
                    header: "Số xã đang hoạt động",
                    cell: (r) => r.points,
                  },
                  {
                    key: "status",
                    header: "Trạng thái",
                    cell: (r) => (
                      <StatusPill
                        tone={r.status === "active" ? "ok" : "neutral"}
                      >
                        {r.status === "active" ? "Hoạt động" : "Ngưng"}
                      </StatusPill>
                    ),
                  },
                  {
                    key: "acts",
                    header: "Thao tác",
                    cell: (r) => (
                      <span className="row">
                        <Link
                          className="btn btn-o btn-sm"
                          href={path(`nhom-vung/${r.code}`)}
                        >
                          Chi tiết
                        </Link>
                        {canManage && r.status === "active" && (
                          <>
                            <Link
                              className="btn btn-o btn-sm"
                              href={path(`nhom-vung/${r.code}/sua`)}
                            >
                              Sửa tên
                            </Link>
                            <button
                              type="button"
                              className="btn btn-d btn-sm"
                              onClick={() =>
                                setRetire({ kind: "region", value: r })
                              }
                            >
                              Ngưng
                            </button>
                          </>
                        )}
                      </span>
                    ),
                  },
                ]}
                rows={r.regions}
                rowKey={(r) => r.code}
              />
            </>
          )
        }
      </LoadState>
      {retire && (
        <RetireConfirmation
          key={`${retire.kind}:${retire.value.code}`}
          value={retire}
          pa={pa}
          canManage={canManage}
          onClose={(changed) => {
            setRetire(null);
            if (changed) setNonce((n) => n + 1);
          }}
        />
      )}
    </>
  );
}
