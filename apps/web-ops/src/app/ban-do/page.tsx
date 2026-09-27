"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { MapView, type AreaCollection, type PointCollection } from "@/components/MapView";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

export default function MapPage() {
  const { api, principal } = useAuth();
  const level = principal?.level === "tw" || principal?.scopeAll ? "province" : "commune";
  const [showAreas, setShowAreas] = useState(true);
  const [showCoops, setShowCoops] = useState(true);
  const [selected, setSelected] = useState<string | null>(null);

  const areas = useAsync(() => unwrap(api.GET("/v1/admin-units/boundaries", { params: { query: { level } } })), [api, level]);
  const coops = useAsync(() => unwrap(api.GET("/v1/cooperatives/locations")), [api]);

  const areaFc = useMemo<AreaCollection | null>(() => {
    if (!areas.data || !showAreas) return null;
    return {
      type: "FeatureCollection",
      features: areas.data.features.map((f) => ({
        ...f,
        properties: { ...f.properties, fill: f.properties.code === selected ? "#f0b429" : "#9fd8b4" },
      })),
    };
  }, [areas.data, showAreas, selected]);

  const pointFc = useMemo<PointCollection | null>(() => (coops.data && showCoops ? coops.data : null), [coops.data, showCoops]);

  const listed = useMemo(() => {
    const all = coops.data?.features ?? [];
    return selected ? all.filter((f) => f.properties.province_code === selected || f.properties.commune_code === selected) : all;
  }, [coops.data, selected]);

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
                Thửa ruộng (lớp tạm — chờ module geo)
              </label>
            </fieldset>
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
                {areas.data.features.map((f) => (
                  <option key={f.properties.code} value={f.properties.code}>
                    {f.properties.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <ul style={{ listStyle: "none" }}>
            {listed.map((f) => (
              <li key={f.properties.id} style={{ padding: "8px 0", borderBottom: "1px solid #edf3ef" }}>
                <Link href={`/htx/${f.properties.id}`} style={{ fontWeight: 600 }}>
                  {f.properties.name}
                </Link>
                <div className="mute small">
                  {f.properties.code} · {formatDecimal(f.properties.area_ha, 1)} ha
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </>
  );
}
