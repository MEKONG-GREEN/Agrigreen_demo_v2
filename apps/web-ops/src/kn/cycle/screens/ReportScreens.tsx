"use client";

// Báo cáo KN v7.7: Dashboard Tỉnh/TW (FN-TI-02.01, FN-TW-01.01), Báo cáo tổng hợp các xã (FN-TI-02.05 — "Cập nhật",
// thay đổi chờ cập nhật, nhật ký, ô thay đổi), Báo cáo TW tuần/tháng/vụ/năm chỉ xem (FN-TW-01.02), Báo cáo Chi cục
// (FN-TI-02.06 — xuất Excel/PDF, gửi email 202 + trạng thái, tải; Google Sheet tắt theo ADR-0026).
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { DetailLayout, LogPanel } from "@/components/patterns/detail";
import { AlertList, HotMetric, ProgressList, SectionTitle, type DashboardAlert } from "@/components/patterns/dashboard";
import { FilterBar, FilterChip, PageHeader, StatusPill } from "@/components/patterns/list";
import { formLayout } from "@/kn/forms/api";
import type { FormDefinition } from "@/kn/forms/types";
import { FormGrid, allCommuneRows, computedCells, type GridCommune } from "@/kn/grid";
import { Greeting } from "@/kn/ui";
import type { ChangedCell, PendingItem, RcChange, RcRun, SnapRow, SnapTotal } from "../api";
import { LoadState, ReadOnlyNotice, dt, ddmm, num, toLog, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";
import { useBatchPeriod } from "./TinhScreens";

const KIND_LABEL: Record<RcChange["kind"], string> = { new: "Gửi mới", resubmit: "Gửi lại", adjusted: "Điều chỉnh theo yêu cầu", computed: "Tự tính" };
const KIND_TONE = { new: "info", resubmit: "neutral", adjusted: "warn", computed: "ok" } as const;

function downloadText(name: string, text: string) {
  const a = document.createElement("a");
  a.href = `data:text/csv;charset=utf-8,${encodeURIComponent("﻿" + text)}`;
  a.download = name;
  a.click();
}

/** Lưới tổng hợp đúng bố cục mẫu: dòng xã theo khu vực + dòng Cộng khu vực / Toàn tỉnh tô xanh; ô thay đổi viền vàng. */
function ConsolidationGrid({ def, rows, totals, regions, changed, missing, ariaLabel }: { def: FormDefinition; rows: SnapRow[]; totals: SnapTotal[]; regions: RcRun["regions"]; changed: ChangedCell[]; missing: string[]; ariaLabel: string }) {
  const communes: GridCommune[] = useMemo(() => {
    const refs = regions as { commune: string; commune_name: string; region: string; region_name: string }[];
    return refs.map((r) => ({ code: r.commune, name: r.commune_name, region: r.region, regionName: r.region_name }));
  }, [regions]);
  const gridRows = useMemo(() => allCommuneRows(def, communes), [def, communes]);
  const computed = useMemo(() => {
    const c = computedCells(rows, totals as never, def, communes);
    for (const m of missing) {
      const name = communes.find((x) => x.code === m);
      if (name) c[m] = { ...(c[m] ?? {}), [def.columns.find((x) => x.kind !== "fixed")?.letter ?? "D"]: "chưa nộp" };
    }
    return c;
  }, [rows, totals, def, communes, missing]);
  const chg = useMemo(() => new Map(changed.map((c) => [`${c.row_key}|${c.letter}`, `Thay đổi: ${c.old ?? "—"} → mới · ${c.changed_by_display || "—"} · ${dt(c.submitted_at)} · ${KIND_LABEL[c.kind]}`])), [changed]);
  return <FormGrid definition={def} rows={gridRows} computed={computed} values={{}} communes={communes} readOnly changed={chg} ariaLabel={ariaLabel} maxHeight={480} />;
}

/** FN-TI-02.05 Báo cáo tổng hợp các xã (Hình 10). readOnly: Admin xem / TW. */
export function ConsolidationScreen({ title = "Báo cáo tổng hợp các xã", reportType, readOnly: forceRO = false }: { title?: string; reportType?: string; readOnly?: boolean }) {
  const { cycleApi, api } = useAuth();
  const roleRO = useCycleReadOnly();
  const readOnly = forceRO || roleRO;
  const bp = useBatchPeriod(null, reportType);
  const [form, setForm] = useState<string | null>(null);
  const [region, setRegion] = useState("");
  const [tab, setTab] = useState<"runs" | "pending">("runs");
  const [runSel, setRunSel] = useState<string | null>(null);
  const [compare, setCompare] = useState(false);
  const act = useAction();
  const forms = bp.detail.data?.batch.forms ?? [];
  const f = forms.find((x) => x.template_id === form) ?? forms[0];
  const pid = bp.period?.id;
  const view = useLoad(
    pid && f
      ? async () => {
          const v = await cycleApi.consolidation({ period_id: pid, template_id: f.template_id, region: region || undefined, ...(compare ? { compare: "previous_year" as const } : {}) });
          const b = bp.detail.data!.batch;
          const lay = await formLayout(api, f.template_id, f.version, { period_label: bp.detail.data!.periods.map((p) => p.label), season: b.season_label });
          return { v, def: lay.definition as FormDefinition };
        }
      : null,
    [pid, f?.template_id, region, compare, act.note],
  );
  const runs = useLoad(pid && f ? () => cycleApi.runs(pid, f.template_id) : null, [pid, f?.template_id, act.note]);
  const selRun = runSel ?? runs.data?.items[0]?.id ?? null;
  const changes = useLoad(selRun ? () => cycleApi.runChanges(selRun) : null, [selRun]);
  const name = useCommuneNames(changes.data?.items.map((c) => c.commune_code) ?? []);
  return (
    <>
      <PageHeader
        title={title}
        actions={
          readOnly ? null : (
            <>
              {(["pdf", "xlsx"] as const).map((fmt) => (
                <button
                  key={fmt}
                  type="button"
                  className="btn btn-o"
                  disabled={!pid || act.busy}
                  onClick={async () => {
                    const r = await act.run(() => cycleApi.exportDispatch({ period_id: pid!, form_codes: f ? [f.code] : undefined, formats: [fmt] }));
                    const file = r?.files[0];
                    if (r && file) {
                      const u = await cycleApi.download(r.dispatch.id, file.id);
                      const a = document.createElement("a");
                      a.href = u.url;
                      a.download = file.file_name;
                      a.click();
                      act.setNote(`Đã xuất ${file.file_name}.`);
                    }
                  }}
                >
                  <Icon name="download" size={15} /> {fmt === "pdf" ? "PDF" : "Xuất Excel theo mẫu"}
                </button>
              ))}
              <button type="button" className="btn btn-p" disabled={!pid || act.busy} onClick={() => void act.run(() => cycleApi.consolidate(pid!), (r) => r.message || `Đã cập nhật ${r.runs.map((x) => `${x.form_code}: ${x.communes_changed} xã, ${x.cells_changed} ô thay đổi`).join("; ")}.`)}>
                <Icon name="refresh" size={15} /> Cập nhật
              </button>
            </>
          )
        }
      />
      {readOnly ? <ReadOnlyNotice /> : null}
      <FilterBar onReset={() => (setRegion(""), setCompare(false))}>
        <FilterChip label="Vụ/đợt" value={bp.active ?? ""} onChange={bp.setBatchId} options={(bp.batches.data?.items ?? []).filter((b) => b.status === "issued").map((b) => ({ value: b.id, label: b.season_label }))} />
        <FilterChip label="Kỳ" value={pid ?? ""} onChange={bp.setPeriodId} options={(bp.detail.data?.periods ?? []).map((p) => ({ value: p.id, label: `${p.label} (${p.range_label})` }))} />
        <FilterChip
          label="Khu vực"
          value={region}
          onChange={setRegion}
          options={[
            { value: "", label: "Tất cả" },
            { value: "LX", label: "Khu vực Long Xuyên" },
            { value: "CD", label: "Khu vực Châu Đốc" },
          ]}
        />
        <label className="fchip" style={{ cursor: "pointer" }}>
          <input type="checkbox" checked={compare} onChange={(e) => setCompare(e.target.checked)} /> So sánh cùng kỳ năm trước
        </label>
      </FilterBar>
      {act.view}
      <LoadState s={view}>
        {({ v, def }) => (
          <>
            <p className="row small" style={{ gap: 8, margin: "4px 0 10px" }} aria-label="Lần cập nhật gần nhất">
              <Icon name="clock" size={14} /> Cập nhật gần nhất: <b>{dt(v.run.at)}</b> · {(v.run as { actor_display?: string }).actor_display} · dữ liệu {v.run.communes_with_data}/{v.run.communes_total} xã
              <StatusPill tone={v.status_label === "Đã chốt" ? "ok" : "warn"}>{v.status_label === "Đã chốt" ? "Đã chốt" : "Tạm tính – chưa chốt"}</StatusPill>
            </p>
            {v.pending.changes ? (
              <div className="alert alert-warn" role="status" aria-label="Thay đổi chờ cập nhật">
                <Icon name="bell" />
                <span>
                  Có <b>{v.pending.changes} thay đổi mới từ {v.pending.communes} xã</b> kể từ lần cập nhật {dt(v.run.at)}.{readOnly ? "" : " Bấm Cập nhật để lấy dữ liệu mới nhất."}
                </span>
              </div>
            ) : null}
            <div className="card sec">
              <div className="dtabs" role="tablist" aria-label="Biểu">
                {forms.map((x) => (
                  <button key={x.template_id} type="button" role="tab" aria-selected={x.template_id === f?.template_id} onClick={() => setForm(x.template_id)}>
                    {x.code} {x.name}
                  </button>
                ))}
              </div>
              <ConsolidationGrid def={def} rows={v.rows as SnapRow[]} totals={v.totals as SnapTotal[]} regions={v.run.regions} changed={v.changed_cells as unknown as ChangedCell[]} missing={(v.missing as { commune_code: string }[]).map((m) => m.commune_code)} ariaLabel={`Tổng hợp ${f?.code ?? ""}`} />
              <p className="small mute" style={{ marginTop: 8 }}>
                Ô viền vàng ● là ô thay đổi trong lần cập nhật gần nhất (rê chuột xem giá trị cũ, người sửa, thời điểm). Dòng tổng Khu vực, Toàn tỉnh tự tính lại mỗi lần cập nhật.
                {(v.missing as unknown[]).length ? ` Xã chưa nộp: ${(v.missing as { commune_name: string }[]).map((m) => m.commune_name).join(", ")}.` : ""}
              </p>
              {v.compare ? (
                <p className="small" aria-label="So sánh cùng kỳ năm trước">
                  Cùng kỳ năm trước — {(v.compare as { label: string }).label}: {JSON.stringify((v.compare as { total: SnapTotal }).total.cells)}
                </p>
              ) : compare ? (
                <p className="small mute">Chưa có số liệu cùng kỳ năm trước (Q-20: nạp số liệu lịch sử, chờ khách).</p>
              ) : null}
            </div>
          </>
        )}
      </LoadState>
      <section className="card sec" style={{ marginTop: 16 }} aria-label="Nhật ký cập nhật">
        <div className="row" style={{ justifyContent: "space-between" }}>
          <h2>Nhật ký cập nhật</h2>
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={!changes.data}
            onClick={() =>
              downloadText(
                "nhat-ky-cap-nhat.csv",
                ["Thời điểm xã gửi,Xã,Biểu - cột,Cũ,Mới,Người sửa,Loại", ...(changes.data?.items ?? []).map((c) => [dt(c.submitted_at), name(c.commune_code), `${changes.data!.run.form_code}·${c.letter}`, c.old ?? "", c.new ?? "", c.changed_by_display, KIND_LABEL[c.kind]].join(","))].join("\n"),
              )
            }
          >
            <Icon name="download" size={14} /> Xuất nhật ký
          </button>
        </div>
        <div className="dtabs" role="tablist" aria-label="Nhật ký">
          <button type="button" role="tab" aria-selected={tab === "runs"} onClick={() => setTab("runs")}>
            Các lần cập nhật
          </button>
          <button type="button" role="tab" aria-selected={tab === "pending"} onClick={() => setTab("pending")}>
            Thay đổi chờ cập nhật ({view.data?.v.pending.changes ?? 0})
          </button>
        </div>
        {tab === "runs" ? (
          <table className="dt" aria-label="Các lần cập nhật">
            <thead>
              <tr>
                <th scope="col">Lần cập nhật</th>
                <th scope="col">Người bấm</th>
                <th scope="col">Số xã có dữ liệu</th>
                <th scope="col">Số xã thay đổi</th>
                <th scope="col">Số ô thay đổi</th>
                <th scope="col" />
              </tr>
            </thead>
            <tbody>
              {(runs.data?.items ?? []).map((r) => (
                <tr key={r.id} className={r.id === selRun ? "hl" : undefined}>
                  <td>{dt(r.at)}</td>
                  <td>{r.trigger === "system" ? "Hệ thống (tự động)" : (r as { actor_display?: string }).actor_display}</td>
                  <td>
                    {r.communes_with_data} / {r.communes_total}
                  </td>
                  <td>{r.communes_changed}</td>
                  <td>{r.cells_changed}</td>
                  <td>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setRunSel(r.id)}>
                      Xem chi tiết
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <ul className="list-plain">
            {((view.data?.v.pending.items ?? []) as unknown as PendingItem[]).map((p) => (
              <li key={p.submission_id}>
                {ddmm(p.submitted_at)} · {p.commune_code} · {p.form_code} · <StatusPill tone={KIND_TONE[p.kind]}>{KIND_LABEL[p.kind]}</StatusPill>
              </li>
            ))}
            {!view.data?.v.pending.changes ? <li className="mute">Dữ liệu đã mới nhất.</li> : null}
          </ul>
        )}
        {changes.data ? (
          <>
            <h3 style={{ marginTop: 12 }}>Chi tiết lần cập nhật {dt(changes.data.run.at)}</h3>
            <table className="dt" aria-label="Chi tiết lần cập nhật">
              <thead>
                <tr>
                  <th scope="col">Thời điểm xã gửi</th>
                  <th scope="col">Xã/phường</th>
                  <th scope="col">Biểu · cột</th>
                  <th scope="col">Giá trị cũ → mới</th>
                  <th scope="col">Người sửa</th>
                  <th scope="col">Loại thay đổi</th>
                </tr>
              </thead>
              <tbody>
                {changes.data.items.map((c) => (
                  <tr key={c.id}>
                    <td>{ddmm(c.submitted_at)}</td>
                    <td>{name(c.commune_code)}</td>
                    <td>
                      {changes.data!.run.form_code} · {c.letter}
                    </td>
                    <td>
                      {num(c.old)} → <b>{num(c.new)}</b>
                    </td>
                    <td>{c.kind === "computed" ? "(công thức)" : c.changed_by_display}</td>
                    <td>
                      <StatusPill tone={KIND_TONE[c.kind]}>{KIND_LABEL[c.kind]}</StatusPill>
                    </td>
                  </tr>
                ))}
                {!changes.data.items.length ? (
                  <tr>
                    <td colSpan={6} className="empty">
                      Lần này không có ô thay đổi.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </>
        ) : null}
      </section>
    </>
  );
}

/** FN-TW-01.02 (và Cấp Xã/Admin xem): báo cáo tuần/tháng/vụ/năm của tỉnh — chỉ xem, không nút sửa. */
export function ProvinceReportScreen({ title, reportType }: { title: string; reportType: string }) {
  const { cycleApi } = useAuth();
  const periods = useLoad(() => cycleApi.periods({ report_type: reportType }), [reportType]);
  if (periods.data && !periods.data.items.length) {
    return (
      <>
        <PageHeader title={title} />
        <ReadOnlyNotice />
        <div className="card sec">
          <p>
            <b>Chưa có dữ liệu.</b>
          </p>
          <p className="mute">
            {reportType === "annual" ? "Báo cáo năm: chưa có mẫu năm trong 14 mẫu — cộng dồn kỳ vụ/tháng hay mẫu riêng chờ khách (Q-07)." : "Chưa có đợt phát hành nào của loại báo cáo này."}
          </p>
        </div>
      </>
    );
  }
  return <ConsolidationScreen title={title} reportType={reportType} readOnly />;
}

function metricV(c: unknown): string | null {
  return (c as { value: string | null } | undefined)?.value ?? null;
}

/** FN-TI-02.01 / FN-TW-01.01 Dashboard trồng trọt (Hình 3d/3e) — chỉ đọc, từ lần tổng hợp gần nhất. */
export function DashboardScreen() {
  const { cycleApi, kn } = useAuth();
  const role = kn.context?.role;
  const isProvince = role === "kn_tinh";
  const [province, setProvince] = useState("91");
  const [region, setRegion] = useState("");
  const [period, setPeriod] = useState("");
  const [nonce, setNonce] = useState(0);
  const s = useLoad(() => cycleApi.dashboard({ province: isProvince ? undefined : province, region: region || undefined, period_id: period || undefined }), [province, region, period, nonce]);
  return (
    <>
      <Greeting />
      <FilterBar onReset={() => (setRegion(""), setPeriod(""))}>
        {isProvince ? null : <FilterChip label="Tỉnh" value={province} onChange={setProvince} options={[{ value: "91", label: "An Giang" }]} defaultValue="91" />}
        <FilterChip label="Kỳ" value={period} onChange={setPeriod} options={[{ value: "", label: "Gần nhất" }, ...(s.data?.periods ?? []).map((p) => ({ value: p.id, label: `${p.label} (${p.range_label})` }))]} />
        {isProvince ? (
          <FilterChip
            label="Khu vực"
            value={region}
            onChange={setRegion}
            options={[
              { value: "", label: "Tất cả" },
              { value: "LX", label: "Khu vực Long Xuyên" },
              { value: "CD", label: "Khu vực Châu Đốc" },
            ]}
          />
        ) : null}
        <button type="button" className="btn btn-o btn-sm" onClick={() => setNonce((n) => n + 1)}>
          <Icon name="refresh" size={14} /> Làm mới
        </button>
        <button
          type="button"
          className="btn btn-o btn-sm"
          disabled={!s.data}
          onClick={() => downloadText("dashboard.csv", ["Xã,DT xuống giống,KH,Đã nộp", ...((s.data?.by_commune ?? []) as { commune_name: string; sown_area: string | null; sown_plan: string | null; submitted: boolean }[]).map((r) => [r.commune_name, r.sown_area ?? "", r.sown_plan ?? "", r.submitted ? "Có" : "Chưa"].join(","))].join("\n"))}
        >
          <Icon name="download" size={14} /> Xuất Excel
        </button>
      </FilterBar>
      <ReadOnlyNotice>Chỉ xem — dữ liệu lần cập nhật gần nhất của Báo cáo tổng hợp các xã; chỉ tính báo cáo đã gửi.</ReadOnlyNotice>
      <LoadState s={s}>
        {(d) => {
          if (d.empty) {
            return (
              <div className="card sec">
                <b>Chưa có dữ liệu</b>
                <p className="mute">Kỳ chưa có lần cập nhật tổng hợp nào.</p>
              </div>
            );
          }
          const c = d.cards as Record<string, unknown>;
          const sub = c.submitted as { communes_submitted: number; communes_total: number };
          const weeks = d.sown_by_week as { label: string; value: string }[];
          const alerts: DashboardAlert[] = d.alerts.map((a, i) => ({ id: `${a.code}-${i}`, level: a.level === "warning" ? "attention" : a.level, title: a.message, at: d.updated_at ?? new Date().toISOString() }));
          return (
            <>
              <p className="small mute">
                {d.period?.label} ({d.period?.range_label}) · cập nhật {dt(d.updated_at)}
              </p>
              <SectionTitle>Chỉ số nóng</SectionTitle>
              <div className="hot-grid">
                <HotMetric label="DT xuống giống lúa / KH (ha)" value={`${num(metricV(c.sown_area))} / ${num(metricV(c.sown_plan))}`} delta={`${num(metricV(c.sown_pct))} % kế hoạch`} trend="neutral" series={weeks.map((w) => Number(w.value))} />
                <HotMetric label="DT rau màu xuống giống (ha)" value={num(metricV(c.veg_sown))} delta={`Thu hoạch ${num(metricV(c.veg_harvest_area))} ha`} trend="neutral" tone="info" />
                <HotMetric label="DT nhiễm sâu bệnh (ha)" value={num(metricV(c.pest_area))} delta={metricV(c.pest_area) ? `Nặng ${num(metricV(c.pest_severe))} ha` : "Chờ mẫu T05 (Q-01)"} trend="neutral" tone="bad" />
                <HotMetric label="Xã đã nộp" value={`${sub.communes_submitted}/${sub.communes_total}`} delta={d.period?.label} trend="neutral" />
              </div>
              <SectionTitle>Chỉ số so sánh</SectionTitle>
              <div className="cmp-grid">
                <ProgressList title="Tiến độ xuống giống theo khu vực" items={(d.sown_by_region as { label: string; pct: string | null; value: string; plan: string | null }[]).map((r) => ({ label: r.label, pct: Number(r.pct ?? 0), detail: `${num(r.value)} / ${num(r.plan)} ha` }))} />
                <section className="card cmp" aria-label="Tiến độ xuống giống theo tuần">
                  <h3>Tiến độ xuống giống theo tuần (ha)</h3>
                  {weeks.map((w) => {
                    const max = Math.max(1, ...weeks.map((x) => Number(x.value)));
                    return (
                      <div className="prog-row" key={w.label}>
                        <div className="prog-top">
                          <span>{w.label}</span>
                          <b>{num(w.value)}</b>
                        </div>
                        <div className="prog-bar">
                          <span style={{ width: `${(Number(w.value) / max) * 100}%`, background: "var(--chart-blue)" }} />
                        </div>
                      </div>
                    );
                  })}
                </section>
              </div>
              <section className="card sec" aria-label="Xã chưa nộp" style={{ marginTop: 12 }}>
                <div className="row" style={{ justifyContent: "space-between" }}>
                  <h3>Xã chưa nộp ({d.unsubmitted.length})</h3>
                  {isProvince ? (
                    <Link className="btn btn-o btn-sm" href={`/kn/nhiem-vu/trang-thai-nop${d.batch ? `?dot=${(d.batch as { id: string }).id}` : ""}`}>
                      <Icon name="bell" size={14} /> Nhắc nhở
                    </Link>
                  ) : null}
                </div>
                <ul className="list-plain">
                  {(d.unsubmitted as { commune_code: string; commune_name: string; pending_forms: string[]; no_officer: boolean }[]).map((u) => (
                    <li key={u.commune_code}>
                      {u.commune_name} — {u.pending_forms.join(", ")}
                      {u.no_officer ? " · Chưa có cán bộ" : ""}
                    </li>
                  ))}
                </ul>
              </section>
              <AlertList alerts={alerts} />
            </>
          );
        }}
      </LoadState>
    </>
  );
}

/** FN-TI-02.06 Báo cáo Chi cục (Hình 11). */
export function DispatchScreen() {
  const { cycleApi } = useAuth();
  const bp = useBatchPeriod();
  const [forms, setForms] = useState<string[] | null>(null);
  const [fmts, setFmts] = useState<("xlsx" | "pdf")[]>(["xlsx", "pdf"]);
  const [current, setCurrent] = useState<string | null>(null);
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [nonce, setNonce] = useState(0);
  const act = useAction();
  const pid = bp.period?.id;
  const recips = useLoad(() => cycleApi.recipients(), [act.note]);
  const history = useLoad(pid ? () => cycleApi.dispatches({ period_id: pid }) : null, [pid, act.note, nonce]);
  const cur = current ?? history.data?.items[0]?.id ?? null;
  const view = useLoad(cur ? () => cycleApi.dispatch(cur) : null, [cur, act.note, nonce]);
  const periodView = useLoad(pid ? () => cycleApi.period(pid) : null, [pid]);
  const batchForms = bp.detail.data?.batch.forms ?? [];
  const pickedForms = forms ?? batchForms.map((f) => f.code);
  const to = (recips.data?.items ?? []).filter((r) => r.kind === "to" && r.is_default);
  const cc = (recips.data?.items ?? []).filter((r) => r.kind === "cc" && r.is_default);
  const label = bp.period?.label ?? "";
  return (
    <>
      <PageHeader title={`Gửi báo cáo Chi cục${label ? ` · ${label}` : ""}`} />
      <FilterBar>
        <FilterChip label="Vụ/đợt" value={bp.active ?? ""} onChange={bp.setBatchId} options={(bp.batches.data?.items ?? []).map((b) => ({ value: b.id, label: b.season_label }))} />
        <FilterChip label="Kỳ" value={pid ?? ""} onChange={bp.setPeriodId} options={(bp.detail.data?.periods ?? []).map((p) => ({ value: p.id, label: `${p.label} (${p.range_label})` }))} />
        <FilterChip label="Phạm vi" value="province" onChange={() => undefined} options={[{ value: "province", label: "Toàn tỉnh" }]} defaultValue="province" />
      </FilterBar>
      {act.view}
      <DetailLayout
        main={
          <>
            <section aria-label="Bước 1 · Chọn dữ liệu và xuất file">
              <h2>Bước 1 · Chọn dữ liệu và xuất file</h2>
              <fieldset className="row" style={{ gap: 12, border: 0 }}>
                <legend className="small">Biểu cần gửi</legend>
                {batchForms.map((f) => (
                  <label key={f.code} className="row" style={{ gap: 4 }}>
                    <input type="checkbox" checked={pickedForms.includes(f.code)} onChange={(e) => setForms(e.target.checked ? [...pickedForms, f.code] : pickedForms.filter((x) => x !== f.code))} /> {f.code} {f.name}
                  </label>
                ))}
              </fieldset>
              <fieldset className="row" style={{ gap: 12, border: 0 }}>
                <legend className="small">Định dạng</legend>
                {(
                  [
                    ["xlsx", "Excel theo mẫu"],
                    ["pdf", "PDF"],
                  ] as const
                ).map(([k, l]) => (
                  <label key={k} className="row" style={{ gap: 4 }}>
                    <input type="checkbox" checked={fmts.includes(k)} onChange={(e) => setFmts(e.target.checked ? [...fmts, k] : fmts.filter((x) => x !== k))} /> {l}
                  </label>
                ))}
                <label className="row" style={{ gap: 4 }} title="Bộ chuyển Google Sheet TẮT mặc định — ADR-0026 (Đề xuất), chờ khách Q-14">
                  <input type="checkbox" disabled aria-describedby="gsheet-note" /> Tạo đường link Google Sheet
                </label>
              </fieldset>
              <p id="gsheet-note" className="small mute">
                Google Sheet đang tắt (ADR-0026 “Đề xuất”, chờ khách Q-14): dữ liệu không đưa ra ngoài lãnh thổ khi chưa được chấp thuận.
              </p>
              <button type="button" className="btn btn-o" disabled={!pid || act.busy || !fmts.length || !pickedForms.length} onClick={async () => {
                const r = await act.run(() => cycleApi.exportDispatch({ period_id: pid!, form_codes: pickedForms, formats: fmts }), (x) => `Đã xuất ${x.files.length} file${x.dispatch.provisional ? " (Tạm tính)" : ""}.`);
                if (r) {
                  setCurrent(r.dispatch.id);
                  setSubject(`TTKN An Giang gửi báo cáo trồng trọt ${r.dispatch.period_label}`);
                  setBody(`Kính gửi Chi cục, TTKN tỉnh An Giang gửi báo cáo trồng trọt ${r.dispatch.period_label}. File đính kèm ở bên dưới.`);
                }
              }}>
                <Icon name="download" size={15} /> Xuất file
              </button>
              {view.data ? (
                <div style={{ marginTop: 10 }} aria-label="File đã tạo">
                  <p className="small">
                    File đã tạo · {dt(view.data.dispatch.created_at)}
                    {view.data.dispatch.provisional ? ` · Tạm tính${view.data.dispatch.missing_communes.length ? ` — xã chưa nộp: ${view.data.dispatch.missing_communes.join(", ")}` : ""}` : ""}
                  </p>
                  <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
                    {view.data.files.map((file) => (
                      <button
                        key={file.id}
                        type="button"
                        className="chip"
                        onClick={async () => {
                          const u = await cycleApi.download(view.data!.dispatch.id, file.id);
                          const a = document.createElement("a");
                          a.href = u.url;
                          a.download = file.file_name;
                          a.click();
                        }}
                      >
                        <Icon name="file" size={14} /> {file.file_name} {Math.max(1, Math.round(file.size_bytes / 1024))} KB
                      </button>
                    ))}
                  </div>
                  <p className="small mute">Số liệu lấy từ lần cập nhật gần nhất của Báo cáo tổng hợp các xã, không sửa tại đây.</p>
                </div>
              ) : null}
            </section>
            <section aria-label="Bước 2 · Gửi email cho Chi cục" style={{ marginTop: 18 }}>
              <h2>Bước 2 · Gửi email cho Chi cục</h2>
              <p>
                Gửi đến: {to.map((r) => `${r.label} <${r.address}>`).join(", ") || "—"}
                <br />
                CC: {cc.map((r) => r.address).join(", ") || "—"}
              </p>
              <label className="field">
                <span>Tiêu đề*</span>
                <input className="input" value={subject} onChange={(e) => setSubject(e.target.value)} />
              </label>
              <label className="field">
                <span>Nội dung</span>
                <textarea className="textarea" value={body} onChange={(e) => setBody(e.target.value)} />
              </label>
              <button
                type="button"
                className="btn btn-p"
                disabled={!view.data || act.busy || view.data.dispatch.status === "sending"}
                onClick={() => void act.run(() => cycleApi.send(view.data!.dispatch.id, { subject, body }), "Đã xếp gửi email (202) — trạng thái cập nhật khi máy chủ gửi xong.").then(() => setTimeout(() => setNonce((n) => n + 1), 2000))}
              >
                <Icon name="send" size={15} /> {view.data?.dispatch.status === "failed" ? "Gửi lại" : "Gửi email"}
              </button>
              <button type="button" className="btn btn-o btn-sm" style={{ marginLeft: 8 }} onClick={() => setNonce((n) => n + 1)}>
                Làm mới trạng thái
              </button>
            </section>
          </>
        }
        log={
          <div className="stack">
            <aside className="card sec" aria-label="Thông tin">
              <h2>Thông tin</h2>
              <dl className="kv">
                <dt>Kỳ</dt>
                <dd>{label || "—"}</dd>
                <dt>Số xã có dữ liệu</dt>
                <dd>{periodView.data ? `${periodView.data.submitted} / ${periodView.data.total}` : "—"}</dd>
                <dt>Trạng thái</dt>
                <dd data-testid="dispatch-status">
                  <StatusPill tone={view.data?.dispatch.status === "sent" ? "ok" : view.data?.dispatch.status === "failed" ? "bad" : "warn"}>
                    {{ exported: "Chưa gửi", sending: "Đang gửi", sent: "Đã gửi", failed: "Gửi lỗi" }[view.data?.dispatch.status ?? "exported"]}
                  </StatusPill>
                </dd>
              </dl>
            </aside>
            <LogPanel
              title="Lịch sử gửi"
              entries={toLog(
                (view.data?.log ?? []).map((l) => {
                  const x = l as { event: string; actor_ref: string; at: string; on_behalf_of: string | null };
                  return { event: x.event, actor_ref: x.actor_ref, actor_display: x.event === "dispatch.sent" ? "Hệ thống" : "", at: x.at, changes: [], reason: null, on_behalf_of: x.on_behalf_of };
                }).reverse(),
              )}
            />
            <aside className="card sec" aria-label="Các lần xuất/gửi">
              <h3>Các lần xuất/gửi của kỳ</h3>
              <ul className="list-plain small">
                {(history.data?.items ?? []).map((d) => (
                  <li key={d.id}>
                    <button type="button" className="link-all" onClick={() => setCurrent(d.id)}>
                      {dt(d.created_at)} · {d.forms.join(", ")} · {{ exported: "Chưa gửi", sending: "Đang gửi", sent: "Đã gửi", failed: "Gửi lỗi" }[d.status]}
                    </button>
                  </li>
                ))}
              </ul>
            </aside>
          </div>
        }
      />
    </>
  );
}
