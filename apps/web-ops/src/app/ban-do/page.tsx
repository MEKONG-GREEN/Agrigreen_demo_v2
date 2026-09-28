"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { loadBoundaries, mergeBoundaries, type BoundaryResult } from "@/api/adapters/boundaries";
import { cooperativePoints, coopPlace, listAllCooperatives } from "@/api/adapters/cooperatives";
import { unitResolver } from "@/api/adapters/units";
import type { ApiClient } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import type { Principal } from "@/auth/principal";
import { MapView, type AreaCollection, type PointCollection } from "@/components/MapView";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

/**
 * Ranh giới cần vẽ, mỗi tỉnh MỘT lời gọi GET /v1/geo/admin-boundaries (G-05): toàn quốc → các tỉnh;
 * tỉnh → các xã của tỉnh; xã → xã của mình (lọc từ các xã cùng tỉnh).
 */
async function loadScopeAreas(api: ApiClient, p: Principal | null): Promise<{ level: "province" | "commune" } & BoundaryResult> {
  if (!p || p.scopeAll || p.level === "tw" || p.level === "cuc" || p.scopeUnits.length === 0) {
    return { level: "province", ...(await loadBoundaries(api, { level: "province" })) };
  }
  const units = await unitResolver(api).many(p.scopeUnits, false);
  const provinces = new Set<string>();
  const communes = new Set<string>();
  for (const u of units.values()) {
    if (u.level === "province") provinces.add(u.code);
    else if (u.level === "commune") {
      communes.add(u.code);
      if (u.parent_code) provinces.add(u.parent_code);
    }
  }
  const merged = mergeBoundaries(await Promise.all(Array.from(provinces).map((parent) => loadBoundaries(api, { level: "commune", parent }))));
  // Cán bộ tỉnh thấy mọi xã của tỉnh; cán bộ xã chỉ thấy xã được phân công.
  const keepAll = Array.from(provinces).some((code) => p.scopeUnits.includes(code));
  if (keepAll) return { level: "commune", ...merged };
  return {
    level: "commune",
    collection: { type: "FeatureCollection", features: merged.collection.features.filter((f) => communes.has(f.properties.code)) },
    missing: merged.missing.filter((c) => communes.has(c)),
  };
}

export default function MapPage() {
  const { api, principal } = useAuth();
  const [showAreas, setShowAreas] = useState(true);
  const [showCoops, setShowCoops] = useState(true);
  const [selected, setSelected] = useState<string | null>(null);

  const areas = useAsync(() => loadScopeAreas(api, principal), [api, principal]);
  const coops = useAsync(() => listAllCooperatives(api), [api]);
  const level = areas.data?.level ?? "province";

  const areaFc = useMemo<AreaCollection | null>(() => {
    if (!areas.data || !showAreas) return null;
    return {
      type: "FeatureCollection",
      features: areas.data.collection.features.map((f) => ({
        ...f,
        properties: { ...f.properties, fill: f.properties.code === selected ? "#f0b429" : "#9fd8b4" },
      })),
    };
  }, [areas.data, showAreas, selected]);

  const points = useMemo(() => cooperativePoints(coops.data ?? []), [coops.data]);
  const pointFc = useMemo<PointCollection | null>(() => (showCoops ? points : null), [points, showCoops]);

  const listed = useMemo(() => {
    const all = coops.data ?? [];
    if (!selected) return all;
    return all.filter((c) => c.admin_unit_code === selected || c.province_code === selected);
  }, [coops.data, selected]);

  const withoutLocation = (coops.data ?? []).length - points.features.length;

  return (
    <>
      <PageHead title="Bản đồ khuyến nông" subtitle="Ranh giới hành chính và vị trí HTX trong phạm vi được phân quyền" />
      {areas.error ? <ProblemAlert problem={areas.error} onRetry={areas.reload} /> : null}
      {coops.error ? <ProblemAlert problem={coops.error} onRetry={coops.reload} /> : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 2.2fr) minmax(260px, 1fr)" }}>
        <div className="map-wrap card">
          <div className="map-panel card">
            <fieldset style={{ border: "none" }}>
              <legend style={{ fontWeight: 700, marginBottom: 8 }}>Lớp bản đồ</legend>
              <label className="row" style={{ marginBottom: 6 }}>
                <input type="checkbox" checked={showAreas} onChange={(e) => setShowAreas(e.target.checked)} />
                Ranh giới {level === "province" ? "tỉnh" : "xã"}
              </label>
              <label className="row" style={{ marginBottom: 6 }}>
                <input type="checkbox" checked={showCoops} onChange={(e) => setShowCoops(e.target.checked)} />
                <span className="sw" style={{ width: 12, height: 12, borderRadius: 6, background: "#f0871a", display: "inline-block" }} aria-hidden="true" />
                Hợp tác xã
              </label>
              <label className="row mute">
                <input type="checkbox" disabled />
                Thửa ruộng — xem trong hồ sơ HTX
              </label>
            </fieldset>
            {areas.data && areas.data.missing.length > 0 ? (
              <p className="mute small" style={{ marginTop: 6 }}>
                {areas.data.missing.length} đơn vị chưa nhập ranh giới.
              </p>
            ) : null}
          </div>
          {areas.loading && !areas.data ? <Loading label="Đang tải ranh giới…" /> : null}
          <MapView label="Bản đồ ranh giới hành chính và HTX" areas={areaFc} points={pointFc} onAreaClick={(c) => setSelected((s) => (s === c ? null : c))} />
        </div>
        <div className="card sec" style={{ maxHeight: "calc(100vh - 170px)", overflow: "auto" }}>
          <div className="sec-h">
            <h2>HTX {selected ? "trong vùng đã chọn" : "trong phạm vi"}</h2>
            {selected ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(null)}>
                Bỏ chọn
              </button>
            ) : null}
          </div>
          {areas.data ? (
            <div className="field" style={{ marginBottom: 12 }}>
              <label htmlFor="area-pick">Chọn vùng (thay cho bấm trên bản đồ)</label>
              <select id="area-pick" className="select" value={selected ?? ""} onChange={(e) => setSelected(e.target.value || null)}>
                <option value="">Tất cả</option>
                {areas.data.collection.features.map((f) => (
                  <option key={f.properties.code} value={f.properties.code}>
                    {f.properties.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          {withoutLocation > 0 ? <p className="mute small">{withoutLocation} HTX chưa có toạ độ trụ sở (vị trí tạm) — không vẽ trên bản đồ.</p> : null}
          {coops.loading && !coops.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }}>
            {listed.map((c) => (
              <li key={c.ref} style={{ padding: "8px 0", borderBottom: "1px solid #edf3ef" }}>
                <Link href={`/htx/${c.ref}`} style={{ fontWeight: 600 }}>
                  {c.name}
                </Link>
                <div className="mute small">
                  {c.code} · {coopPlace(c)} · {c.area_ha ? `${formatDecimal(c.area_ha, 1)} ha` : "chưa khai diện tích"}
                  {c.location_provisional ? " · vị trí tạm" : ""}
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </>
  );
}
