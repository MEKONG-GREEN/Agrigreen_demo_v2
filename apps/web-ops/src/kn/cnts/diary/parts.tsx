"use client";

// Phần dùng chung của màn nhật ký nuôi (W11.48): kết quả kiểm tra tự động, bảng chỉ số so lần trước, vị trí lúc ghi so với
// điểm, biểu đồ tăng trọng theo lần cân, ảnh (mở qua máy chủ — bản rõ chỉ trong bộ nhớ trình duyệt).
import { Fragment, useEffect, useState } from "react";
import { StatusPill } from "@/components/patterns/list";
import {
  AMOUNT_OF,
  dmy,
  growthTable,
  lineAmount,
  MAX_GROWTH_WEEK,
  metricsOf,
  money,
  PHOTO_KIND_LABEL,
  STATUS_LABEL,
  STATUS_TONE,
  TABLE_FIELDS,
  TABLE_LABEL,
  tablesOf,
  TECH_TARGETS,
  vn,
  type CntsDiaryApi,
  type Check,
  type Cycle,
  type Entry,
  type EntryDetail,
  type Form,
  type Tables,
} from "./api";

export function StatusBadge({ status }: { status: Entry["status"] }) {
  return <StatusPill tone={STATUS_TONE[status]}>{STATUS_LABEL[status]}</StatusPill>;
}

const LEVEL_TONE = { block: "bad", warn: "warn", info: "info" } as const;

/** Kết quả kiểm tra tự động (chặn / cảnh báo / thông tin). */
export function Checks({ checks }: { checks: Check[] }) {
  if (!checks.length) return <p className="mute">Không có cảnh báo.</p>;
  return (
    <ul className="list-plain" aria-label="Kết quả kiểm tra tự động">
      {checks.map((c, i) => (
        <li key={`${c.code}-${i}`}>
          <StatusPill tone={LEVEL_TONE[c.level]}>{c.level === "block" ? "Chặn" : c.level === "warn" ? "Cảnh báo" : "Thông tin"}</StatusPill> {c.message}
        </li>
      ))}
    </ul>
  );
}

/** Có cờ cảnh báo vị trí / chỉ số. */
export const flagged = (e: Pick<Entry, "checks">) => e.checks.some((c) => c.level === "warn");

/** Bảng chỉ số của lần ghi, kèm cột lần trước (nếu có). */
export function MetricsTable({
  form,
  entry,
  previous,
}: {
  form: Form;
  entry: Pick<Entry, "entry_kind" | "metrics">;
  previous?: Record<string, string> | null;
}) {
  const defs = metricsOf(form).filter((d) => d.kinds.includes(entry.entry_kind));
  const label = (d: (typeof defs)[number], v: string | undefined) => (v ? (d.values?.find((x) => x[0] === v)?.[1] ?? v) : "—");
  return (
    <table className="table">
      <thead>
        <tr>
          <th>Chỉ số</th>
          <th>Lần này</th>
          {previous ? <th>Lần trước</th> : null}
        </tr>
      </thead>
      <tbody>
        {defs.map((d) => (
          <tr key={d.code}>
            <td>
              {d.label}{" "}
              <span className="mute">
                ({d.ref}
                {d.unit ? `, ${d.unit}` : ""})
              </span>
            </td>
            <td>{label(d, entry.metrics[d.code])}</td>
            {previous ? <td>{label(d, previous[d.code])}</td> : null}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** Vị trí lúc ghi so với điểm (NT-15): khoảng cách, sai số, cờ; tọa độ khi người xem có quyền. */
export function LocationInfo({ e }: { e: Pick<EntryDetail, "location"> & Partial<Pick<EntryDetail, "fix" | "target">> }) {
  const l = e.location;
  if (!l.has_fix) return <p>Lần ghi không có vị trí GPS.</p>;
  return (
    <div>
      <p>
        {l.no_target_point ? (
          "Hộ/điểm chưa có tọa độ — chưa tính khoảng cách."
        ) : (
          <>
            Cách điểm mô hình <b>{l.distance_m} m</b>
          </>
        )}
        {l.accuracy_m ? <> · sai số {l.accuracy_m} m</> : null} {l.far ? <StatusPill tone="warn">Xa điểm</StatusPill> : null}{" "}
        {l.weak_accuracy ? <StatusPill tone="warn">GPS yếu</StatusPill> : null}{" "}
        {l.mock_suspected ? <StatusPill tone="bad">Nghi vị trí giả lập</StatusPill> : null}
      </p>
      {e.fix ? (
        <p className="mute">
          Lúc ghi: {e.fix.lat.toFixed(6)}, {e.fix.lng.toFixed(6)}
          {e.target ? (
            <>
              {" "}
              · Điểm: {e.target.lat.toFixed(6)}, {e.target.lng.toFixed(6)}
            </>
          ) : null}
        </p>
      ) : l.fix_stored ? (
        <p className="mute">Tọa độ lúc ghi chỉ người ghi, người rà soát xem được (hoặc đã xóa theo hạn lưu).</p>
      ) : null}
    </div>
  );
}

/** Biểu đồ tăng trọng theo lần cân (UR-TINH-12). */
export function WeightChart({ points }: { points: { date: string; weight_avg_g: string }[] }) {
  if (points.length < 2) return points.length ? <p className="mute">Một lần cân: {points[0]!.weight_avg_g} g/con.</p> : null;
  const w = 320,
    h = 120,
    pad = 20;
  const ys = points.map((p) => Number(p.weight_avg_g));
  const max = Math.max(...ys),
    min = Math.min(...ys);
  const x = (i: number) => pad + (i * (w - 2 * pad)) / (points.length - 1);
  const y = (v: number) => h - pad - ((v - min) / (max - min || 1)) * (h - 2 * pad);
  return (
    <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} role="img" aria-label="Biểu đồ trọng lượng bình quân theo lần cân">
      <polyline fill="none" stroke="currentColor" strokeWidth="2" points={ys.map((v, i) => `${x(i)},${y(v)}`).join(" ")} />
      {ys.map((v, i) => (
        <g key={i}>
          <circle cx={x(i)} cy={y(v)} r="3" fill="currentColor" />
          <text x={x(i)} y={y(v) - 6} fontSize="9" textAnchor="middle">
            {v}
          </text>
        </g>
      ))}
    </svg>
  );
}

/** Ảnh của lần ghi — tải nội dung qua máy chủ (có kiểm quyền, ghi sổ truy cập). */
export function Photos({ dapi, photos }: { dapi: CntsDiaryApi; photos: EntryDetail["photos"] }) {
  const [urls, setUrls] = useState<Record<string, string>>({});
  useEffect(() => {
    let alive = true;
    const made: string[] = [];
    for (const p of photos) {
      dapi
        .photoUrl(p.file_ref)
        .then((u) => {
          made.push(u);
          if (alive) setUrls((m) => ({ ...m, [p.file_ref]: u }));
        })
        .catch(() => undefined);
    }
    return () => {
      alive = false;
      made.forEach((u) => URL.revokeObjectURL(u));
    };
  }, [dapi, photos]);
  if (!photos.length) return <p className="mute">Không có ảnh.</p>;
  return (
    <div className="row">
      {photos.map((p) => (
        <figure key={p.file_ref}>
          {/* eslint-disable-next-line @next/next/no-img-element -- ảnh blob đã giải mã, không qua bộ tối ưu ảnh */}
          {urls[p.file_ref] ? <img src={urls[p.file_ref]} alt={PHOTO_KIND_LABEL[p.kind]} width={160} /> : <span className="mute">Đang mở ảnh…</span>}
          <figcaption className="mute">{PHOTO_KIND_LABEL[p.kind]}</figcaption>
        </figure>
      ))}
    </div>
  );
}

/** Đầu sổ (NC-01): mô hình, thời gian thực hiện, cán bộ theo dõi điểm, chủ hộ, địa điểm, thông tin nhận giống (NC-02). */
export function CycleHeader({ c }: { c: Cycle }) {
  const rows: [string, string][] = [
    ["Mô hình", `${c.project_name} (${c.project_code})`],
    ["Thời gian thực hiện", `từ ${dmy(c.project_start)}${c.project_end ? ` đến ${dmy(c.project_end)}` : ""}`],
    ["Cán bộ theo dõi điểm", c.officer_display ?? (c.officer_ref ? "—" : "Chưa phân công")],
    ["Chủ hộ", c.owner_label ?? "—"],
    ["Địa điểm thực hiện mô hình", c.site_name],
    ["Ngày nhận giống; giống", `${dmy(c.received_date)}; ${c.breed}`],
    ["Số lượng thả nuôi đầu kỳ", `${c.initial_count} con`],
    ["Trọng lượng nhận; ngày tuổi", `${c.initial_weight_g ? `${vn(Number(c.initial_weight_g))} g/con` : "—"}; ${c.age_days ?? "—"}`],
  ];
  return (
    <dl className="kv" aria-label="Đầu sổ">
      {rows.map(([k, v]) => (
        <Fragment key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

/** Mục 1 "Thông tin chung" của sổ: vật tư nhận (NC-03). */
export function SuppliesView({ supplies }: { supplies: Cycle["supplies"] }) {
  if (!supplies.length) return null;
  return (
    <table className="table" aria-label="Vật tư nhận">
      <thead>
        <tr>
          <th>Stt</th>
          <th>Vật tư</th>
          <th>Ngày nhận</th>
          <th>Số lượng</th>
          <th>Chủng loại vật tư</th>
        </tr>
      </thead>
      <tbody>
        {supplies.map((s, i) => (
          <tr key={i}>
            <td>{i + 1}</td>
            <td>{s.item || "—"}</td>
            <td>{dmy(s.date)}</td>
            <td>
              {s.quantity} {s.unit}
            </td>
            <td>{s.kind || "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** Bảng phụ của lần ghi (mục 3–7, phần 3 của sổ) — thành tiền tự tính. */
export function TablesView({ form, tables }: { form: Form; tables: Tables }) {
  const names = tablesOf(form).filter((n) => (tables[n] ?? []).length > 0);
  if (!names.length) return null;
  return (
    <>
      {names.map((n) => {
        const fields = TABLE_FIELDS[n];
        const amount = AMOUNT_OF[n];
        const rows = (tables[n] ?? []) as unknown as Record<string, string | number | undefined>[];
        return (
          <table className="table" key={n} aria-label={TABLE_LABEL[n]}>
            <caption>{TABLE_LABEL[n]}</caption>
            <thead>
              <tr>
                {fields.map((f) => (
                  <th key={f.key}>{f.label}</th>
                ))}
                {amount ? <th>Thành tiền</th> : null}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>
                  {fields.map((f) => {
                    const v = r[f.key];
                    return <td key={f.key}>{v === undefined || v === "" || (f.key === "shot" && !v) ? "—" : f.type === "date" ? dmy(String(v)) : String(v)}</td>;
                  })}
                  {amount ? <td>{money(lineAmount(r[amount[0]], r[amount[1]]))}</td> : null}
                </tr>
              ))}
            </tbody>
          </table>
        );
      })}
    </>
  );
}

/** Phần 2 của sổ (cán bộ theo dõi điểm): yêu cầu kỹ thuật cần đạt và chỉ tiêu sinh trưởng theo tuần tuổi (NC-12). */
export function GrowthSection({ c, entries }: { c: Cycle; entries: Pick<Entry, "entry_date" | "metrics" | "status">[] }) {
  const g = growthTable(c, entries);
  const targets = TECH_TARGETS[c.form] ?? [];
  const animal = c.form === "BM-09" ? "vịt" : "gà";
  return (
    <section aria-label="Chỉ tiêu sinh trưởng">
      <h4>Phần 2 — Cán bộ theo dõi điểm</h4>
      {targets.length ? (
        <table className="table" aria-label="Yêu cầu kỹ thuật cần đạt">
          <caption>1. Các yêu cầu kỹ thuật cần đạt</caption>
          <thead>
            <tr>
              <th>Tên chỉ tiêu</th>
              <th>ĐVT</th>
              <th>Chỉ tiêu cần đạt</th>
              <th>Thực tế</th>
              <th>Đánh giá</th>
            </tr>
          </thead>
          <tbody>
            {targets.map((t) => {
              const v = t.actual(g);
              return (
                <tr key={t.label}>
                  <td>{t.label}</td>
                  <td>{t.unit}</td>
                  <td>{t.target}</td>
                  <td>{vn(v)}</td>
                  <td>{v === null ? "Chưa có số liệu" : t.ok(v) ? "Đạt" : "Chưa đạt"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : null}
      <table className="table" aria-label="Chỉ tiêu sinh trưởng theo tuần tuổi">
        <caption>2. Chỉ tiêu sinh trưởng của {animal} (tự tính từ lần ghi đã gửi)</caption>
        <thead>
          <tr>
            <th>Stt</th>
            <th>Tuần tuổi</th>
            <th>Số lượng {animal} sống (con)</th>
            <th>Tỉ lệ sống (%)</th>
            <th>Nguyên nhân</th>
            <th>Thức ăn trong kỳ (kg)</th>
            <th>Tiêu tốn thức ăn (kg TA/kg tăng trọng)</th>
            <th>Trọng lượng trung bình (kg/con)</th>
          </tr>
        </thead>
        <tbody>
          {g.rows.map((r, i) => (
            <tr key={r.week}>
              <td>{i + 1}</td>
              <td>Tuần {r.week}</td>
              <td>{r.alive ?? "—"}</td>
              <td>{vn(r.survival)}</td>
              <td>{r.dead ? `${r.dead} con${r.causes ? ` — ${r.causes}` : ""}` : "—"}</td>
              <td>{r.feedKg ? vn(r.feedKg) : "—"}</td>
              <td>{vn(r.fcr)}</td>
              <td>{vn(r.weightKg)}</td>
            </tr>
          ))}
          <tr>
            <td colSpan={2}>
              <b>Cộng</b>
            </td>
            <td>{g.total.alive ?? "—"}</td>
            <td>{vn(g.total.survival)}</td>
            <td>{g.total.dead ? `${g.total.dead} con` : "—"}</td>
            <td>{vn(g.total.feedKg)}</td>
            <td>{vn(g.total.fcr)}</td>
            <td>—</td>
          </tr>
          <tr>
            <td colSpan={2}>
              <b>Bình quân</b>
            </td>
            <td colSpan={3} className="mute">
              Thức ăn/con đầu kỳ: {vn(g.avg.feedPerHead)} kg
            </td>
            <td>—</td>
            <td>—</td>
            <td>{vn(g.avg.weightKg)}</td>
          </tr>
        </tbody>
      </table>
      {g.outOfRange.length ? (
        <p role="note" className="mute">
          Dữ liệu tuần tuổi bất thường: {g.outOfRange.length} lần ghi có tuần tuổi ngoài 1–{MAX_GROWTH_WEEK} không đưa vào bảng (
          {g.outOfRange
            .slice(0, 5)
            .map((x) => `ngày ${dmy(x.entry_date)} — ${Number.isFinite(x.week) ? `tuần ${x.week}` : "không xác định"}`)
            .join("; ")}
          {g.outOfRange.length > 5 ? `; và ${g.outOfRange.length - 5} lần khác` : ""}). Nhờ người ghi điều chỉnh lần ghi.
        </p>
      ) : null}
    </section>
  );
}
