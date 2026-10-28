"use client";

// Cấp Xã › Dữ liệu (FN-XA-03.03..03.06) trên API P7 /v1/kn/*: HTX & nông hộ (hồ sơ KN riêng #67 — ProducerScreens.tsx),
// máy móc theo HTX (khai báo thay toàn bộ, tổng xã = Σ HTX; năm SX ở hồ sơ KN riêng #119), thửa ruộng (danh sách; vẽ ở field-app), vật tư (ghi nhận,
// tổng hợp theo xã). Cấp Tỉnh/Quốc gia chỉ xem (useCycleReadOnly + máy chủ 403 *.read_only).
import { useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { parseNumberInput } from "@/components/form";
import { DetailLayout } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";
import { vnDate } from "@/kn/schedule/week";
import { normalizeNumberInput } from "@/lib/exactDecimal";
import { HouseholdAddRoute, ProducerDetailScreen, ProducerFormScreen, ProducerListScreen } from "./ProducerScreens";

/** Ghi chú phần còn ở nơi khác (nạp Excel, vẽ bản đồ ngoại tuyến…). */
export function GapNotice({ what }: { what: string }) {
  return (
    <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
      <Icon name="info" />
      <span>{what}</span>
    </div>
  );
}

/** Bộ lọc Xã/phường theo các xã phụ trách của phiên KN (dùng chung các màn Dữ liệu xã). */
export function useCommuneFilter() {
  const { kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [commune, setCommune] = useState("");
  const chip = <FilterChip label="Xã/phường" value={commune} onChange={setCommune} options={[{ value: "", label: "Tất cả xã phụ trách" }, ...communes.map((c) => ({ value: c.code, label: c.name }))]} />;
  return { commune, chip, communes };
}

// FN-XA-03.03 HTX & nông hộ: từ KNSPLIT-2-KN-RECORDS (#67) chạy trên hồ sơ KN riêng /v1/kn/producer-records (ProducerScreens.tsx)
// — KN không còn ghi HTX qua /v1/kn/cooperatives. Giữ tên export cũ cho các trang app/kn/du-lieu/htx/*.

/** FN-XA-03.03 Danh sách HTX (Hình 16a) — hồ sơ KN loại HTX. */
export function CoopsScreen() {
  return <ProducerListScreen kind="cooperative" />;
}

/** FN-XA-03.03 Chi tiết HTX (Hình 16b): Thông tin chung, Nông hộ, Máy móc/Thửa (HTX đã liên kết); Log. */
export function CoopDetailScreen() {
  return <ProducerDetailScreen kind="cooperative" />;
}

/** FN-XA-03.03 Thêm HTX trên trang riêng (R-A2). Bắt buộc theo TT-08: Xã/phường, Tên, Người đại diện, Điện thoại. */
export function CoopCreateScreen() {
  return <ProducerFormScreen kind="cooperative" back={{ href: "/kn/du-lieu/htx", label: "Danh sách HTX" }} />;
}

/** FN-XA-03.03 Thêm nông hộ vào HTX trên trang riêng (R-A2) — hồ sơ KN loại Nông hộ thuộc HTX. */
export function HouseholdCreateScreen() {
  return <HouseholdAddRoute parentKind="cooperative" />;
}

type MachineLine = { machine_type: string; quantity: string; year_made: string; note: string };
type MachineField = "type" | "quantity" | "year";

/** Năm hiện tại theo lịch Việt Nam — cùng mốc máy chủ #119 dùng cho "Năm SX ≤ năm hiện tại". */
export const vnYear = (ms = Date.now()) => Number(vnDate(ms).slice(0, 4));
/** TT-10 (R-A1): Số lượng là số nguyên 1 – 1.000.000 như pf/mechanization (#113 BF-SRV-GUARD). */
export const MACHINE_QTY_RULE = { required: true, integer: true, positive: true, max: 1_000_000 } as const;
/** Năm nhỏ nhất máy chủ nhận (giả định #119, chờ khách). */
export const MACHINE_YEAR_MIN = 1900;

/** TT-10: Năm SX bắt buộc, số nguyên, 1900 ≤ năm ≤ năm hiện tại (giờ VN). Trả lỗi tiếng Việt hoặc undefined. */
export function machineYearError(raw: string, maxYear = vnYear()): string | undefined {
  const p = parseNumberInput(raw, { required: true, integer: true, max: maxYear });
  if (!p.ok) return p.error;
  return Number(p.value) < MACHINE_YEAR_MIN ? `Không được nhỏ hơn ${MACHINE_YEAR_MIN}.` : undefined;
}

/** Kiểm cả bảng khai báo trước khi Lưu: lỗi theo `${dòng}.${trường}`; trùng loại máy báo ở dòng sau (máy chủ cũng chặn). */
export function validateMachineLines(rows: MachineLine[], maxYear = vnYear()): Record<string, string> {
  const errs: Record<string, string> = {};
  const seen = new Set<string>();
  rows.forEach((l, i) => {
    if (!l.machine_type) errs[`${i}.type`] = "Bắt buộc nhập.";
    else if (seen.has(l.machine_type)) errs[`${i}.type`] = "Trùng loại máy — sửa dòng đã có.";
    seen.add(l.machine_type);
    const q = parseNumberInput(l.quantity, MACHINE_QTY_RULE);
    if (!q.ok) errs[`${i}.quantity`] = q.error;
    const y = machineYearError(l.year_made, maxYear);
    if (y) errs[`${i}.year`] = y;
  });
  return errs;
}

/**
 * Lỗi theo trường máy chủ trả → ô của dòng: pf/mechanization `lines[i].quantity` (#113), hồ sơ KN
 * `lines.<loại máy>.year_made` | `lines.<loại máy>.machine_type` (#119).
 */
export function machineServerErrors(fields: Record<string, string>, rows: { machine_type: string }[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [f, reason] of Object.entries(fields)) {
    const byIdx = /^lines\[(\d+)\]\.(quantity|machine_type)$/.exec(f);
    if (byIdx) {
      out[`${byIdx[1]}.${byIdx[2] === "quantity" ? "quantity" : "type"}`] = reason;
      continue;
    }
    const byType = /^lines\.(.+)\.(year_made|machine_type)$/.exec(f);
    if (!byType) continue;
    const i = rows.findIndex((r) => r.machine_type === byType[1]);
    if (i >= 0) out[`${i}.${byType[2] === "year_made" ? "year" : "type"}`] = reason;
  }
  return out;
}

/** Ô nhập số trong bảng (số lượng, năm) — lỗi tiếng Việt ngay dưới ô (UI-KIT R-A1). */
function CellInput({ label, value, error, disabled, onChange, onBlur }: { label: string; value: string; error?: string; disabled: boolean; onChange: (v: string) => void; onBlur: () => void }) {
  return (
    <>
      <input className="input" aria-label={label} aria-required aria-invalid={error ? true : undefined} disabled={disabled} inputMode="numeric" value={value} onChange={(e) => onChange(e.target.value)} onBlur={onBlur} />
      {error ? (
        <span className="err" role="alert">
          {error}
        </span>
      ) : null}
    </>
  );
}

/**
 * FN-XA-03.04 Máy móc HTX: trái danh sách HTX + tổng xã theo loại; phải bảng khai báo của HTX đã chọn.
 * TT-10 (#103): HTX bắt buộc chọn trước khi khai báo; mỗi dòng bắt buộc Loại máy, Số lượng > 0, Năm SX (1900 – năm hiện
 * tại). Số lượng lưu ở pf/mechanization (/v1/kn/machine-counts), năm ở hồ sơ KN riêng (/v1/kn/machine-profiles, #119):
 * lưu số lượng trước rồi năm trong cùng một lượt thao tác (bấm lại sau lỗi dùng lại khóa chống gửi trùng).
 */
export function MachinesScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const act = useAction();
  const coops = useLoad(() => collectApi.cooperatives({ commune: f.commune || undefined }), [f.commune]);
  const overview = useLoad(() => collectApi.machines(f.commune || undefined), [f.commune, act.note]);
  const [ref, setRef] = useState("");
  const cur = useLoad(
    ref
      ? async () => {
          const [m, p] = await Promise.all([collectApi.coopMachines(ref), collectApi.machineProfile(ref)]);
          return { ...m, years: new Map(p.items.map((x) => [x.machine_type, x.year_made])) };
        }
      : null,
    [ref, act.note],
  );
  const [lines, setLines] = useState<MachineLine[] | null>(null);
  const rows = lines ?? (cur.data?.lines ?? []).map((l) => ({ machine_type: l.machine_type, quantity: String(l.quantity), year_made: String(cur.data?.years.get(l.machine_type) ?? ""), note: l.note ?? "" }));
  const name = useCommuneNames(overview.data?.commune_totals.map((t) => t.commune_code) ?? []);
  const TYPES = [
    ["may_cay", "Máy cày"],
    ["may_cay_xoi", "Máy xới"],
    ["may_gieo_sa", "Máy gieo sạ"],
    ["may_cay_lua", "Máy cấy"],
    ["drone", "Thiết bị bay không người lái"],
    ["may_gat", "Máy gặt đập liên hợp"],
    ["may_say", "Máy sấy"],
  ];
  const [errs, setErrs] = useState<Record<string, string>>({});
  const server = act.problem ? machineServerErrors(fieldErrorMap(act.problem), rows) : {};
  const err = (i: number, k: MachineField) => errs[`${i}.${k}`] ?? server[`${i}.${k}`];
  const setErr = (i: number, k: MachineField, e: string | undefined) =>
    setErrs((x) => {
      const next = { ...x };
      if (e) next[`${i}.${k}`] = e;
      else delete next[`${i}.${k}`];
      return next;
    });
  const setRow = (i: number, patch: Partial<MachineLine>) => setLines(rows.map((x, k) => (k === i ? { ...x, ...patch } : x)));
  const saveMachines = async () => {
    const e = validateMachineLines(rows);
    setErrs(e);
    if (Object.keys(e).length) return;
    const counts = rows.map((l) => ({ machine_type: l.machine_type, quantity: Number(normalizeNumberInput(l.quantity)), note: l.note.trim() || null }));
    const years = rows.map((l) => ({ machine_type: l.machine_type, year_made: Number(normalizeNumberInput(l.year_made)) }));
    const r = await act.run(
      async (idem) => {
        const saved = await collectApi.putMachines(ref, counts);
        // Sau await: bọc bằng idem của CÙNG lượt để bấm lại sau mất phản hồi giữ khóa cho cả hai bước (#129).
        await idem(() => collectApi.putMachineProfile(ref, years));
        return saved;
      },
      (r) => `Đã lưu khai báo máy (${r.total} máy).`,
    );
    if (r) setLines(null);
  };
  const machineCols: Column<MachineLine & { i: number }>[] = [
    {
      key: "type",
      header: "Loại máy",
      cell: (l) => (
        <>
          <select className="select" aria-label={`Loại máy dòng ${l.i + 1}`} aria-required aria-invalid={err(l.i, "type") ? true : undefined} disabled={readOnly} value={l.machine_type} onChange={(e) => (setRow(l.i, { machine_type: e.target.value }), setErr(l.i, "type", undefined))}>
            {TYPES.map(([v, t]) => (
              <option key={v} value={v}>
                {t}
              </option>
            ))}
          </select>
          {err(l.i, "type") ? (
            <span className="err" role="alert">
              {err(l.i, "type")}
            </span>
          ) : null}
        </>
      ),
    },
    {
      key: "qty",
      header: "Số lượng",
      cell: (l) => {
        const blur = () => {
          const p = parseNumberInput(l.quantity, MACHINE_QTY_RULE);
          setErr(l.i, "quantity", p.ok ? undefined : p.error);
        };
        return <CellInput label={`Số lượng dòng ${l.i + 1}`} value={l.quantity} error={err(l.i, "quantity")} disabled={readOnly} onChange={(v) => setRow(l.i, { quantity: v })} onBlur={blur} />;
      },
    },
    {
      key: "year",
      header: "Năm SX",
      cell: (l) => <CellInput label={`Năm sản xuất dòng ${l.i + 1}`} value={l.year_made} error={err(l.i, "year")} disabled={readOnly} onChange={(v) => setRow(l.i, { year_made: v })} onBlur={() => setErr(l.i, "year", machineYearError(l.year_made))} />,
    },
    { key: "note", header: "Ghi chú", cell: (l) => <input className="input" aria-label={`Ghi chú dòng ${l.i + 1}`} disabled={readOnly} value={l.note} onChange={(e) => setRow(l.i, { note: e.target.value })} /> },
    {
      key: "del",
      header: "",
      cell: (l) =>
        readOnly ? null : (
          <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa dòng ${l.i + 1}`} onClick={() => (setLines(rows.filter((_, k) => k !== l.i)), setErrs({}))}>
            Xóa
          </button>
        ),
    },
  ];
  // Dòng mới mặc định loại máy chưa khai báo (tránh trùng loại ngay khi thêm).
  const nextType = TYPES.find(([v]) => !rows.some((r) => r.machine_type === v))?.[0] ?? "may_cay";
  return (
    <>
      <PageHeader title="Máy móc HTX" />
      <FilterBar>
        {f.chip}
        <FilterChip label="HTX" value={ref} onChange={(v) => (setRef(v), setLines(null), setErrs({}))} options={[{ value: "", label: "Chọn HTX" }, ...(coops.data?.items ?? []).map((c) => ({ value: c.ref, label: c.name }))]} />
      </FilterBar>
      {act.view}
      <DetailLayout
        main={
          ref ? (
            <LoadState s={cur}>
              {(m) => (
                <>
                  <h2>{m.name}</h2>
                  <DataTable caption="Khai báo máy" columns={machineCols} rows={rows.map((l, i) => ({ ...l, i }))} rowKey={(l) => String(l.i)} empty="HTX chưa khai báo máy." />
                  {readOnly ? null : (
                    <div className="row" style={{ gap: 8, marginTop: 8 }}>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setLines([...rows, { machine_type: nextType, quantity: "1", year_made: "", note: "" }])}>
                        + Thêm dòng
                      </button>
                      <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void saveMachines()}>
                        Lưu
                      </button>
                    </div>
                  )}
                </>
              )}
            </LoadState>
          ) : (
            <p className="mute">Chọn một HTX để khai báo máy (bắt buộc).</p>
          )
        }
        log={
          <aside className="card sec" aria-label="Tổng xã theo loại máy">
            <h2>Tổng xã theo loại máy</h2>
            <ul className="list-plain">
              {(overview.data?.commune_totals ?? []).map((t) => (
                <li key={`${t.commune_code}-${t.machine_type}`}>
                  {name(t.commune_code)} · {t.machine_type_name}: <b>{t.quantity}</b>
                </li>
              ))}
            </ul>
          </aside>
        }
      />
    </>
  );
}

// FN-XA-03.05 Vẽ thửa ruộng (danh sách, thêm, sửa — TT-11 #104): xem PlotScreens.tsx.

/** FN-XA-03.06 Vật tư — bảng nhập nhiều dòng (BF-TT-MAT #105) ở MaterialsScreen.tsx. */
export { MaterialsScreen } from "./MaterialsScreen";
