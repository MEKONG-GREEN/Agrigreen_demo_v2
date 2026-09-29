"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import type { CropDashGroup } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { KpiTile, Loading, PageHead } from "@/components/ui";
import { useCatalog } from "@/htx/common";
import { toCsv, todayVN } from "@/htx/rules";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { saveBlob } from "@/lib/files";
import { useAsync } from "@/lib/useAsync";

const tonnes = (kg: string | null | undefined) => (kg ? Number(kg) / 1000 : 0);

/**
 * Biểu đồ cột nhóm Dự kiến – Thực tế (tấn) có lưới trục Y và chú thích khi di chuột (HTX US-DASH-01 AC-2).
 * Chỉ để VẼ nên đổi sang number; số liệu gốc giữ dạng chuỗi thập phân.
 */
function YieldChart({ groups, label }: { groups: { key: string; name: string; expected: number; actual: number }[]; label: string }) {
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(1, ...groups.flatMap((g) => [g.expected, g.actual]));
  const step = Math.pow(10, Math.floor(Math.log10(max)));
  const top = Math.ceil(max / step) * step;
  const ticks = Array.from({ length: 5 }, (_, i) => (top / 4) * i);
  const W = 640;
  const H = 260;
  const left = 56;
  const bottom = 36;
  const plotH = H - bottom - 12;
  const bw = Math.min(36, (W - left - 10) / Math.max(groups.length, 1) / 2.6);
  const gx = (i: number) => left + ((W - left - 10) / Math.max(groups.length, 1)) * (i + 0.5);
  const y = (v: number) => 12 + plotH - (v / top) * plotH;
  const fmt = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 1 });
  return (
    <figure style={{ margin: 0 }}>
      <svg viewBox={`0 0 ${W} ${H}`} width="100%" role="img" aria-label={label}>
        {ticks.map((t) => (
          <g key={t}>
            <line x1={left} x2={W - 10} y1={y(t)} y2={y(t)} stroke="#e3ece6" />
            <text x={left - 6} y={y(t) + 4} fontSize={11} textAnchor="end" fill="#5b6b63">
              {fmt.format(t)}
            </text>
          </g>
        ))}
        {groups.map((g, i) => (
          <g key={g.key} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} data-testid={`bar-${g.key}`}>
            <rect x={gx(i) - bw - 2} y={y(g.expected)} width={bw} height={Math.max(12 + plotH - y(g.expected), 1)} fill="#9cc9ad" rx={3}>
              <title>{`${g.name} — dự kiến ${fmt.format(g.expected)} tấn`}</title>
            </rect>
            <rect x={gx(i) + 2} y={y(g.actual)} width={bw} height={Math.max(12 + plotH - y(g.actual), 1)} fill="#1b8a54" rx={3}>
              <title>{`${g.name} — thực tế ${fmt.format(g.actual)} tấn`}</title>
            </rect>
            <text x={gx(i)} y={H - 16} fontSize={11.5} textAnchor="middle" fill="#33473f">
              {g.name.length > 14 ? `${g.name.slice(0, 13)}…` : g.name}
            </text>
          </g>
        ))}
        {hover !== null && groups[hover] ? (
          <g pointerEvents="none">
            <rect x={Math.min(gx(hover) + 10, W - 190)} y={14} width={180} height={44} rx={6} fill="#0f1f1a" opacity={0.9} />
            <text x={Math.min(gx(hover) + 18, W - 182)} y={32} fontSize={11.5} fill="#fff">
              Dự kiến: {fmt.format(groups[hover].expected)} tấn
            </text>
            <text x={Math.min(gx(hover) + 18, W - 182)} y={50} fontSize={11.5} fill="#fff">
              Thực tế: {fmt.format(groups[hover].actual)} tấn
            </text>
          </g>
        ) : null}
      </svg>
      <figcaption className="row small" style={{ gap: 14 }}>
        <span>
          <span aria-hidden="true" style={{ display: "inline-block", width: 10, height: 10, background: "#9cc9ad", marginRight: 4 }} />
          Dự kiến (diện tích × năng suất giống)
        </span>
        <span>
          <span aria-hidden="true" style={{ display: "inline-block", width: 10, height: 10, background: "#1b8a54", marginRight: 4 }} />
          Thực tế (sản lượng đã khai)
        </span>
      </figcaption>
      <table className="sr-only">
        <caption>{label}</caption>
        <thead>
          <tr>
            <th>Nhóm</th>
            <th>Dự kiến (tấn)</th>
            <th>Thực tế (tấn)</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <tr key={g.key}>
              <td>{g.name}</td>
              <td>{fmt.format(g.expected)}</td>
              <td>{fmt.format(g.actual)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}

function pct(v: string | null | undefined): string | null {
  return v === null || v === undefined ? null : formatDecimal(v, 1);
}

/** Bảng điều hành sản xuất HTX (HTX US-DASH-01): dự kiến – thực tế, diện tích, nông hộ, tiến độ SOP; xuất CSV (US-DASH-02). */
export default function CropDashboardPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const seasons = useCatalog("season");
  const varieties = useCatalog("rice_variety");
  const [seasonCode, setSeasonCode] = useState("");
  const [year, setYear] = useState(todayVN().slice(0, 4));
  const [variety, setVariety] = useState("");
  const [by, setBy] = useState<"by_variety" | "by_season" | "by_cooperative">("by_variety");
  const d = useAsync(
    () =>
      unwrap(
        api.GET("/v1/cropcycle/dashboard", {
          params: { query: { cooperative_ref: scope.coopRef || undefined, season_code: seasonCode || undefined, season_year: year ? Number(year) : undefined, variety_code: variety || undefined } },
        }),
      ),
    [api, scope.coopRef, seasonCode, year, variety],
  );
  const t = d.data?.totals;
  const nameOf = (g: CropDashGroup) => {
    if (by === "by_variety") return varieties.find((v) => v.code === g.key)?.name ?? g.key ?? "—";
    if (by === "by_season") {
      const [code, yr] = (g.key ?? ":").split(":");
      return `${seasons.find((s) => s.code === code)?.name ?? code} ${yr}`;
    }
    return scope.coopName(g.key);
  };
  const groups = d.data?.[by] ?? [];
  const chart = groups.map((g) => ({ key: g.key ?? "", name: nameOf(g), expected: tonnes(g.expected_kg), actual: tonnes(g.actual_kg) }));

  const exportCsv = () => {
    const header = ["Nhóm", "Số vụ", "Vụ đang mở", "Vụ hoàn thành", "Nông hộ", "Diện tích (ha)", "Dự kiến (kg)", "Thực tế (kg)", "Diện tích đã thu (ha)", "Năng suất thực tế (t/ha)", "Tiến độ SOP (%)", "Nhật ký đã duyệt", "Nhật ký chờ duyệt"];
    const row = (name: string, g: CropDashGroup) => [name, g.seasons, g.open_seasons, g.completed_seasons, g.households, g.area_ha, g.expected_kg, g.actual_kg, g.harvested_area_ha, g.actual_yield_t_per_ha ?? "", g.sop_progress_pct ?? "", g.approved_logs, g.pending_logs];
    const filter = [
      `Bộ lọc: HTX=${scope.coopRef ? scope.coopName(scope.coopRef) : "Tất cả trong phạm vi"}`,
      `Vụ=${seasonCode || "Tất cả"}`,
      `Năm=${year || "Tất cả"}`,
      `Giống=${variety || "Tất cả"}`,
      `Xuất lúc ${new Date().toISOString()}`,
    ];
    const csv = toCsv([filter, header, ...groups.map((g) => row(nameOf(g), g)), ...(t ? [row("Tổng", t)] : [])]);
    saveBlob(new Blob([csv], { type: "text/csv;charset=utf-8" }), `san-luong-htx-${year || "tat-ca"}${seasonCode ? `-${seasonCode}` : ""}.csv`);
  };

  return (
    <>
      <PageHead
        title="Bảng điều hành sản xuất"
        subtitle="Sản lượng dự kiến – thực tế, diện tích, nông hộ và tiến độ SOP (chỉ tính nhật ký đã duyệt)"
        actions={
          <button type="button" className="btn btn-o btn-sm" onClick={exportCsv} disabled={!d.data}>
            <Icon name="download" size={15} />
            Xuất Excel (CSV)
          </button>
        }
      />
      <div className="alert alert-info small" style={{ marginBottom: 14 }} role="note">
        <Icon name="info" />
        <span>
          Chưa có mẫu báo cáo sản lượng HTX trong bộ máy báo cáo (pf/reports), nên tệp được dựng ngay trên trình duyệt từ số liệu đang xem, khớp đúng bộ lọc hiện hành
          (CSV mở bằng Excel). Xuất PDF, xuất nền và lập lịch gửi báo cáo qua email sẽ có khi bổ sung mẫu báo cáo HTX.
        </span>
      </div>
      <div className="card sec" style={{ marginBottom: 16 }}>
        <form className="row" role="search" style={{ alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
          <CoopPicker scope={scope} />
          <div className="field">
            <label htmlFor="d-season">Vụ</label>
            <select id="d-season" className="select" value={seasonCode} onChange={(e) => setSeasonCode(e.target.value)}>
              <option value="">Tất cả</option>
              {seasons.map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="d-year">Năm</label>
            <input id="d-year" className="input" type="number" value={year} onChange={(e) => setYear(e.target.value)} style={{ width: 100 }} />
          </div>
          <div className="field">
            <label htmlFor="d-variety">Giống lúa</label>
            <select id="d-variety" className="select" value={variety} onChange={(e) => setVariety(e.target.value)}>
              <option value="">Tất cả</option>
              {varieties.map((v) => (
                <option key={v.code} value={v.code}>
                  {v.name}
                </option>
              ))}
            </select>
          </div>
        </form>
      </div>
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {t ? (
        <>
          <div className="grid4" style={{ marginBottom: 16 }}>
            <KpiTile label="Tổng diện tích" value={formatDecimal(t.area_ha, 1)} unit="ha" color="green" icon="area" footer={`${formatInt(t.seasons)} vụ · ${formatInt(t.open_seasons)} đang mở`} />
            <KpiTile label="Tổng nông hộ" value={formatInt(t.households)} unit="hộ" color="blue" icon="coop" />
            <KpiTile
              label="Tiến độ SOP"
              value={pct(t.sop_progress_pct)}
              unit="%"
              color="violet"
              icon="sop"
              footer={`${formatInt(t.approved_logs)} nhật ký đã duyệt · ${formatInt(t.pending_logs)} chờ duyệt`}
              missingHint="Chưa có vụ nào trong bộ lọc"
            />
            <KpiTile
              label="Sản lượng thực tế / dự kiến"
              value={formatDecimal(tonnes(t.actual_kg), 1)}
              unit={`/ ${formatDecimal(tonnes(t.expected_kg), 1)} tấn`}
              color="orange"
              icon="harvest"
              footer={t.actual_yield_t_per_ha ? `Năng suất thực tế ${formatDecimal(t.actual_yield_t_per_ha, 2)} t/ha` : "Chưa có khai sản lượng"}
            />
          </div>
          <div className="card sec">
            <div className="sec-h">
              <h2>Dự kiến – thực tế</h2>
              <div className="field">
                <label htmlFor="d-by">Theo</label>
                <select id="d-by" className="select" value={by} onChange={(e) => setBy(e.target.value as typeof by)}>
                  <option value="by_variety">Giống lúa</option>
                  <option value="by_season">Vụ</option>
                  <option value="by_cooperative">HTX</option>
                </select>
              </div>
            </div>
            <YieldChart groups={chart} label="Biểu đồ sản lượng dự kiến và thực tế (tấn)" />
            <div className="tbl-wrap" style={{ marginTop: 12 }}>
              <table className="tbl">
                <caption className="sr-only">Số liệu theo nhóm</caption>
                <thead>
                  <tr>
                    <th scope="col">Nhóm</th>
                    <th scope="col" className="num">Vụ</th>
                    <th scope="col" className="num">Nông hộ</th>
                    <th scope="col" className="num">Diện tích (ha)</th>
                    <th scope="col" className="num">Dự kiến (tấn)</th>
                    <th scope="col" className="num">Thực tế (tấn)</th>
                    <th scope="col" className="num">t/ha</th>
                    <th scope="col" className="num">Tiến độ SOP (%)</th>
                  </tr>
                </thead>
                <tbody>
                  {groups.map((g) => (
                    <tr key={g.key}>
                      <td>{nameOf(g)}</td>
                      <td className="num">{formatInt(g.seasons)}</td>
                      <td className="num">{formatInt(g.households)}</td>
                      <td className="num">{formatDecimal(g.area_ha, 2)}</td>
                      <td className="num">{formatDecimal(tonnes(g.expected_kg), 1)}</td>
                      <td className="num">{formatDecimal(tonnes(g.actual_kg), 1)}</td>
                      <td className="num">{g.actual_yield_t_per_ha ? formatDecimal(g.actual_yield_t_per_ha, 2) : "—"}</td>
                      <td className="num">{pct(g.sop_progress_pct) ?? "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      ) : null}
    </>
  );
}
