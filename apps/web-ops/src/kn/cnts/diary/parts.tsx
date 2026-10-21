"use client";

// Phần dùng chung của màn nhật ký nuôi (W11.48): kết quả kiểm tra tự động, bảng chỉ số so lần trước, vị trí lúc ghi so với
// điểm, biểu đồ tăng trọng theo lần cân, ảnh (mở qua máy chủ — bản rõ chỉ trong bộ nhớ trình duyệt).
import { useEffect, useState } from "react";
import { StatusPill } from "@/components/patterns/list";
import { metricsOf, type CntsDiaryApi, type Check, type Entry, type EntryDetail, type Form, PHOTO_KIND_LABEL, STATUS_LABEL, STATUS_TONE } from "./api";

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
