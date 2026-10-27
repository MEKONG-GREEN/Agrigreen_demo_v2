"use client";

// BF-TT-PUBLISH (#106, PRD TT-14) — FN-TI-02.02 Phát hành biểu mẫu theo 5 bước thật (stepper):
//   B1 chọn biểu (theo loại báo cáo) → B2 phạm vi xã (toàn tỉnh / khu vực / chọn từng xã từ danh sách, không gõ mã thô)
//   → B3 loại kỳ, khoảng ngày, vụ/đợt, hạn nộp, số ngày chốt, ghi chú (máy chủ tự chia kỳ) → B4 chỉ tiêu "Tỉnh giao"
//   theo xã (nhập tay / Excel / sao từ đợt trước) → B5 xem trước rồi Phát hành.
// "Sao từ đợt trước": web điền sẵn giá trị của đợt đã phát hành gần nhất có cùng biểu (GET provincial-values) để xem và
// sửa; biểu mà mọi xã đã sao còn nguyên được gửi qua copy_provincial_from_previous (máy chủ chép lại, nguồn "previous");
// đã sửa/xóa xã đã sao thì cả biểu gửi trực tiếp trong provincial_values (xem splitProvincial). Không đổi engine pf/forms.
import Link from "next/link";
import { useId, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { NumberField, TextField, parseNumberInput, validateForm } from "@/components/form";
import { DataTable, FilterBar, FilterChip, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { RawFile, type RcBatchInput, type RcFormItem, type RcPreview, type RcProvincialImport } from "../api";
import { LoadState, REPORT_RULE_V2_FROM, dt, reportWeekStart, useAction, useCommuneNames, useLoad, weekRangeName, weekStartLocal, weekStartOfRule, ymdLocal } from "../common";
import { PiiHint } from "@/kn/pii";

// Hạn tính từ ngày cuối kỳ; "" = hạn mặc định của lịch kỳ máy chủ (v2: tuần → 12:00 Thứ Tư sau kỳ; W11.03).
const DEADLINES = [
  { value: "", label: "Mặc định theo lịch kỳ" },
  { value: "1|12:00", label: "1 ngày sau ngày cuối kỳ, 12:00" },
  { value: "1|17:00", label: "1 ngày sau ngày cuối kỳ, 17:00" },
  { value: "0|17:00", label: "Ngày cuối kỳ, 17:00" },
  { value: "2|11:00", label: "2 ngày sau ngày cuối kỳ, 11:00" },
];

type PublishReportType = "weekly" | "monthly" | "seasonal" | "adhoc";
type PeriodChoice = "" | NonNullable<RcBatchInput["period_type"]>;
type Mode = "province" | "regions" | "communes";
type PV = Record<string, Record<string, Record<string, string>>>;

const REPORT_TYPES: { value: PublishReportType; label: string }[] = [
  { value: "weekly", label: "Tuần" },
  { value: "monthly", label: "Tháng" },
  { value: "seasonal", label: "Vụ" },
  { value: "adhoc", label: "Đột xuất" },
];

/**
 * Cách chia kỳ theo loại báo cáo (W11.05b, như reportcycle/domain.ReportPeriodTypes): "" = mặc định của loại báo cáo.
 * Ngày / Tuần KSGM / Quý chỉ chia được với lịch kỳ v2 (đợt bắt đầu từ REPORT_RULE_V2_FROM).
 */
const PERIOD_CHOICES: Record<PublishReportType, { value: PeriodChoice; label: string }[]> = {
  weekly: [
    { value: "", label: "Tuần báo cáo" },
    { value: "ksgm_week", label: "Tuần KSGM (4 tuần/tháng)" },
    { value: "day", label: "Ngày" },
  ],
  monthly: [
    { value: "", label: "Tháng" },
    { value: "quarter", label: "Quý" },
  ],
  seasonal: [{ value: "", label: "Cả vụ (một kỳ)" }],
  adhoc: [
    { value: "", label: "Một kỳ" },
    { value: "day", label: "Ngày" },
  ],
};

export const STEPS = ["Chọn biểu", "Phạm vi xã", "Kỳ và hạn nộp", "Chỉ tiêu Tỉnh giao", "Xem trước & phát hành"] as const;

/** Ô ngày bắt buộc có dấu * và lỗi theo trường (UI-KIT chưa có ô ngày riêng; dùng cùng khung `.field`). */
export function DateInput({ label, value, onChange, error }: { label: string; value: string; onChange: (v: string) => void; error?: string }) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        <span className="req" aria-hidden="true">
          {" "}*
        </span>
      </label>
      <input id={id} className="input" type="date" aria-required value={value} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-err` : undefined} onChange={(e) => onChange(e.target.value)} />
      {error ? (
        <span className="err" id={`${id}-err`} role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function StepError({ text }: { text?: string }) {
  return text ? (
    <p className="err" role="alert">
      {text}
    </p>
  ) : null;
}

/**
 * Tách Tỉnh giao để gửi. Máy chủ (reportcycle Issue) với copy_provincial_from_previous chép giá trị đợt trước cho MỌI xã
 * của đợt chưa có dòng trong provincial_values — API không biểu đạt được "ô đã xóa". Vì vậy chỉ nhờ máy chủ chép một biểu
 * (nguồn previous) khi mọi xã đã sao của biểu còn NGUYÊN và không xã nào có giá trị ở đợt trước đang trống trên màn hình;
 * khi đó xã sao nguyên không gửi, xã khác gửi trực tiếp. Ngược lại (đã sửa/xóa một xã đã sao, hoặc xóa trắng một xã mà
 * đợt trước có giá trị) → KHÔNG gửi cờ chép cho biểu đó, gửi toàn bộ ảnh màn hình qua provincial_values (Codex #106).
 * `copied` = giá trị đã điền sẵn theo biểu → xã; `prev` = toàn bộ giá trị đợt trước đã lấy (mặc định = copied);
 * `scope` = các xã nhận đợt này (không truyền = mọi xã).
 */
export function splitProvincial(pv: PV, copied: PV, picked: string[], prev: PV = copied, scope?: readonly string[]): { provincial: PV; copyFrom: string[]; copiedRows: number } {
  const provincial: PV = {};
  const copyFrom: string[] = [];
  let copiedRows = 0;
  const filled = (cells: Record<string, string> = {}) => Object.fromEntries(Object.entries(cells).filter(([, v]) => v !== ""));
  const same = (a: Record<string, string>, b: Record<string, string>) => Object.keys(a).length === Object.keys(b).length && Object.keys(a).every((k) => a[k] === b[k]);
  const inScope = (cm: string) => !scope || scope.includes(cm);
  for (const tpl of picked) {
    const rows: Record<string, Record<string, string>> = {};
    for (const [cm, cells] of Object.entries(pv[tpl] ?? {})) {
      const kept = filled(cells);
      if (Object.keys(kept).length) rows[cm] = kept;
    }
    const cp = Object.entries(copied[tpl] ?? {}).filter(([cm]) => inScope(cm));
    const untouched = cp.length > 0 && cp.every(([cm, src]) => rows[cm] !== undefined && same(rows[cm]!, filled(src)));
    const noResurrect = Object.entries(prev[tpl] ?? {}).every(([cm, src]) => !inScope(cm) || rows[cm] !== undefined || !Object.keys(filled(src)).length);
    if (untouched && noResurrect) {
      copyFrom.push(tpl);
      copiedRows += cp.length;
      for (const [cm] of cp) delete rows[cm];
    }
    if (Object.keys(rows).length) provincial[tpl] = rows;
  }
  return { provincial, copyFrom, copiedRows };
}

/** FN-TI-02.02 Phát hành biểu mẫu báo cáo (Hình 7) — wizard 5 bước (TT-14). */
export function PublishScreen() {
  const { cycleApi, kn } = useAuth();
  const ws = weekStartLocal(new Date(), reportWeekStart(new Date(), kn.context?.department_code)); // D8: lịch tuần của phòng
  const [step, setStep] = useState(1);
  const [rt, setRt] = useState<PublishReportType>("weekly");
  const [pt, setPt] = useState<PeriodChoice>("");
  const [picked, setPicked] = useState<string[]>([]);
  const [mode, setMode] = useState<Mode>("province");
  const [regions, setRegions] = useState<string[]>([]);
  const [communes, setCommunes] = useState<string[]>([]);
  const [communeQ, setCommuneQ] = useState("");
  const [from, setFrom] = useState(ymdLocal(ws));
  const [to, setTo] = useState(ymdLocal(new Date(ws.getFullYear(), ws.getMonth(), ws.getDate() + 27)));
  const [season, setSeason] = useState("Thu Đông 2026");
  const [deadline, setDeadline] = useState("");
  const [closeAfter, setCloseAfter] = useState("1");
  const [errors, setErrors] = useState<Partial<Record<"from" | "to" | "season" | "closeAfter", string>>>({});
  const [stepErr, setStepErr] = useState<string | undefined>();
  const [pvErr, setPvErr] = useState<Record<string, string>>({});
  const [note, setNote] = useState("");
  const [pv, setPv] = useState<PV>({});
  const [copied, setCopied] = useState<PV>({});
  const [prevPv, setPrevPv] = useState<PV>({}); // toàn bộ giá trị đợt trước đã lấy khi "Sao từ đợt trước" (kể cả xã không điền)
  const [copyNote, setCopyNote] = useState<string | null>(null);
  const [pvSource, setPvSource] = useState<"manual" | "excel">("manual");
  const [pvImport, setPvImport] = useState<RcProvincialImport | null>(null);
  const [preview, setPreview] = useState<RcPreview | null>(null);
  const [issued, setIssued] = useState<string | null>(null);
  const act = useAction();
  const forms = useLoad(() => cycleApi.reportForms(rt, from), [rt, from]);
  const batches = useLoad(() => cycleApi.batches(), [issued]);
  // B2: danh sách xã (kèm khu vực) của tỉnh lấy từ Xem trước phạm vi "Toàn tỉnh" của các biểu đã chọn — không gõ mã xã.
  const scopeKey = step >= 2 && picked.length ? `${rt}|${pt}|${from}|${to}|${picked.join(",")}` : "";
  const provinceScope = useLoad(
    scopeKey ? () => cycleApi.preview({ report_type: rt, template_ids: picked, ...(pt ? { period_type: pt } : {}), scope: { mode: "province" }, from_date: from, to_date: to, season_label: season || "-" }) : null,
    [scopeKey],
  );
  const allCommunes = provinceScope.data?.communes ?? [];
  const regionList = [...new Map(allCommunes.map((c) => [c.region_code, c.region_name])).entries()].filter(([code]) => code);
  const previewCommunes = preview?.communes ?? [];
  const name = useCommuneNames([...allCommunes, ...previewCommunes].map((c) => c.commune_code));
  const pickedForms = (forms.data?.items ?? []).filter((f) => picked.includes(f.template_id));
  const provCols = pickedForms.flatMap((f) => f.provincial_columns.map((c) => ({ f, c })));

  const resetFrom = (s: number) => {
    // Đổi dữ liệu bước trước → xem trước cũ không còn đúng.
    if (s <= 3) setPreview(null);
  };
  /** Bỏ Tỉnh giao (giá trị + lỗi) của biểu bị bỏ chọn; không truyền = bỏ hết (đổi loại báo cáo). Chọn lại thì ô trống. */
  const dropPv = (tpls?: string[]) => {
    const gone = (tpl: string) => !tpls || tpls.includes(tpl);
    setPv((x) => Object.fromEntries(Object.entries(x).filter(([tpl]) => !gone(tpl))));
    setCopied((x) => Object.fromEntries(Object.entries(x).filter(([tpl]) => !gone(tpl))));
    setPrevPv((x) => Object.fromEntries(Object.entries(x).filter(([tpl]) => !gone(tpl))));
    setPvErr((x) => Object.fromEntries(Object.entries(x).filter(([k]) => !gone(k.split("|")[0]!))));
  };

  /** Kiểm B3 (R-A1/R-A4) — không gọi API khi còn lỗi. */
  const checkPeriod = (): number | null => {
    const v = validateForm(
      { from, to, season, closeAfter },
      { from: { kind: "text", required: true }, to: { kind: "text", required: true }, season: { kind: "text", required: true, maxLength: 120 }, closeAfter: { kind: "number", required: true, integer: true, max: 60 } },
    );
    const errs = { ...v.errors };
    if (!errs.to && !errs.from && to < from) errs.to = "Đến ngày phải sau hoặc bằng Từ ngày.";
    setErrors(errs);
    return Object.keys(errs).length ? null : Number(v.values.closeAfter);
  };
  /** Kiểm Tỉnh giao (≥ 0) của biểu đang chọn; trả giá trị đã chuẩn hóa hoặc null khi có lỗi. */
  const checkPv = (): PV | null => {
    const pe: Record<string, string> = {};
    const norm: PV = {};
    for (const tpl of picked) {
      for (const [cm, cells] of Object.entries(pv[tpl] ?? {})) {
        for (const [letter, raw] of Object.entries(cells)) {
          const p = parseNumberInput(raw);
          if (!p.ok) pe[`${tpl}|${cm}|${letter}`] = p.error;
          else ((norm[tpl] ??= {})[cm] ??= {})[letter] = p.value;
        }
      }
    }
    setPvErr(pe);
    return Object.keys(pe).length ? null : norm;
  };
  const body = (closeAfterDays: number, values: PV): RcBatchInput => {
    const [offset, time] = deadline.split("|");
    const { provincial, copyFrom } = splitProvincial(values, copied, picked, prevPv, preview ? previewCommunes.map((c) => c.commune_code) : undefined);
    return {
      report_type: rt,
      template_ids: picked,
      ...(pt ? { period_type: pt } : {}),
      scope: { mode, ...(mode === "regions" ? { regions } : {}), ...(mode === "communes" ? { communes } : {}) },
      from_date: from,
      to_date: to,
      season_label: season,
      ...(deadline ? { deadline: { offset_days: Number(offset), time: time! } } : {}),
      close_after_days: closeAfterDays,
      ...(note.trim() ? { note: note.trim() } : {}),
      provincial_values: provincial,
      provincial_source: pvSource,
      ...(copyFrom.length ? { copy_provincial_from_previous: copyFrom } : {}),
    };
  };

  const next = async () => {
    setStepErr(undefined);
    if (step === 1 && !picked.length) return setStepErr("Chọn ít nhất một biểu.");
    if (step === 2) {
      if (mode === "regions" && !regions.length) return setStepErr("Chọn ít nhất một khu vực.");
      if (mode === "communes" && !communes.length) return setStepErr("Chọn ít nhất một xã/phường.");
    }
    if (step === 3) {
      const ca = checkPeriod();
      if (ca === null) return;
      // Máy chủ tự chia kỳ và trả danh sách xã nhận → dùng cho B4, B5.
      const r = await act.run(() => cycleApi.preview(body(ca, {})));
      if (!r) return;
      setPreview(r);
    }
    if (step === 4) {
      const ca = checkPeriod();
      const values = checkPv();
      if (ca === null || values === null) return;
      const r = await act.run(() => cycleApi.preview(body(ca, values)));
      if (!r) return;
      setPreview(r);
    }
    setStep((s) => Math.min(5, s + 1));
  };
  const issue = async () => {
    const ca = checkPeriod();
    const values = checkPv();
    if (ca === null || values === null) return;
    const r = await act.run(() => cycleApi.issue(body(ca, values)), (x) => `Đã phát hành ${x.batch.task_count.toLocaleString("vi-VN")} công việc cho ${x.batch.communes.length} xã, ${x.periods.length} kỳ.`);
    if (r) {
      setIssued(r.batch.id);
      setPreview(null);
      setPicked([]);
      dropPv();
      setPvImport(null);
      setCopyNote(null);
      setStep(1);
    }
  };

  /** UR-TI-02: tệp mẫu CSV UTF-8 (BOM) — cột Mã xã, Tên xã và mỗi cột Tỉnh giao "<Mã biểu> <Chữ cột>". */
  const downloadPvTemplate = () => {
    const esc = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
    const head = ["Mã xã", "Tên xã", ...provCols.map(({ f, c }) => `${f.code} ${c.letter} ${c.title}${c.unit ? ` (${c.unit})` : ""}`)];
    const lines = [head, ...previewCommunes.map((cm) => [cm.commune_code, name(cm.commune_code), ...provCols.map(({ f, c }) => pv[f.template_id]?.[cm.commune_code]?.[c.letter] ?? "")])];
    const blob = new Blob([String.fromCharCode(0xfeff) + lines.map((l) => l.map(esc).join(",")).join("\r\n") + "\r\n"], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `tinh-giao-${rt}-${from}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importPv = async (file: File) => {
    const xlsx = /\.xlsx$/i.test(file.name);
    const type = xlsx ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" : "text/csv; charset=utf-8";
    const r = await act.run(
      async () => cycleApi.provincialImport({ report_type: rt, from_date: from, template_ids: picked }, new RawFile(await file.arrayBuffer(), type)),
      (x) => `Đã đọc ${x.accepted} dòng từ “${file.name}”${x.rejected ? `, ${x.rejected} dòng lỗi` : ""}.`,
    );
    if (!r) return;
    setPvImport(r);
    setPvSource("excel");
    setPv((cur) => {
      const nx = { ...cur };
      for (const [tpl, byCommune] of Object.entries(r.provincial_values)) nx[tpl] = { ...(nx[tpl] ?? {}), ...byCommune };
      return nx;
    });
  };
  /** Sao từ đợt trước: đợt đã phát hành gần nhất (mã đợt UUIDv7 lớn nhất) có cùng biểu — như máy chủ store.PreviousBatch. */
  const copyPrevious = async () => {
    const all = batches.data?.items ?? [];
    const targets = [...new Set(provCols.map(({ f }) => f.template_id))];
    const r = await act.run(async () => {
      const out: { tpl: string; season: string; values: Record<string, Record<string, string>> }[] = [];
      for (const tpl of targets) {
        const prev = all.filter((b) => b.status === "issued" && b.forms.some((f) => f.template_id === tpl)).sort((a, b) => (a.id < b.id ? 1 : -1))[0];
        if (!prev) continue;
        const got = await cycleApi.provincialValues(prev.id, tpl);
        const values: Record<string, Record<string, string>> = {};
        for (const it of got.items) if (it.template_id === tpl) values[it.commune_code] = it.cells;
        out.push({ tpl, season: prev.season_label, values });
      }
      return out;
    });
    if (!r) return;
    const inScope = new Set(previewCommunes.map((c) => c.commune_code));
    const nextPv: PV = { ...pv };
    const nextCopied: PV = { ...copied };
    const nextPrev: PV = { ...prevPv };
    let n = 0;
    for (const { tpl, values } of r) {
      nextPrev[tpl] = values;
      for (const [cm, cells] of Object.entries(values)) {
        const cur = nextPv[tpl]?.[cm] ?? {};
        if (!inScope.has(cm) || Object.values(cur).some((v) => v !== "")) continue; // chỉ điền xã nhận đợt này, còn trống
        nextPv[tpl] = { ...(nextPv[tpl] ?? {}), [cm]: { ...cells } };
        nextCopied[tpl] = { ...(nextCopied[tpl] ?? {}), [cm]: { ...cells } };
        n++;
      }
    }
    setPv(nextPv);
    setCopied(nextCopied);
    setPrevPv(nextPrev);
    const seasons = [...new Set(r.map((x) => x.season))];
    setCopyNote(r.length ? `Đã sao ${n} dòng xã × biểu từ đợt ${seasons.map((s) => `“${s}”`).join(", ")}; xã đã có giá trị giữ nguyên.` : "Chưa có đợt trước nào có biểu đã chọn.");
  };

  const formCols: Column<RcFormItem>[] = [
    {
      key: "pick",
      header: "Chọn",
      cell: (f) => (
        <input
          type="checkbox"
          aria-label={`Chọn ${f.code}`}
          checked={picked.includes(f.template_id)}
          onChange={(e) => {
            const on = e.target.checked;
            setPicked((p) => (on ? [...p, f.template_id] : p.filter((x) => x !== f.template_id)));
            if (!on) dropPv([f.template_id]);
            resetFrom(1);
          }}
        />
      ),
    },
    { key: "code", header: "Mã", cell: (f) => <b>{f.code}</b> },
    { key: "name", header: "Tên biểu", cell: (f) => f.name },
    { key: "pv", header: "Cột Tỉnh giao", cell: (f) => (f.provincial_columns.length ? f.provincial_columns.map((c) => c.letter).join(", ") : "—") },
    { key: "ver", header: "Mẫu", cell: (f) => <StatusPill tone="info">v{f.version}</StatusPill> },
  ];

  type PreviewCommune = (typeof previewCommunes)[number];
  const pvCols: Column<PreviewCommune>[] = [
    { key: "commune", header: "Xã/phường", cell: (cm) => `${name(cm.commune_code)} · ${cm.region_name}` },
    ...provCols.map(({ f, c }) => ({
      key: `${f.code}${c.letter}`,
      header: `${f.code} · cột ${c.letter} “${c.title}” ${c.unit ? `(${c.unit})` : ""}`,
      cell: (cm: PreviewCommune) => {
        const k = `${f.template_id}|${cm.commune_code}|${c.letter}`;
        const raw = pv[f.template_id]?.[cm.commune_code]?.[c.letter] ?? "";
        const fromPrev = copied[f.template_id]?.[cm.commune_code]?.[c.letter] !== undefined && copied[f.template_id]?.[cm.commune_code]?.[c.letter] === raw && raw !== "";
        return (
          <>
            <input
              className="input"
              inputMode="decimal"
              aria-label={`${f.code} ${c.letter} ${cm.commune_code}`}
              aria-invalid={pvErr[k] ? true : undefined}
              title={fromPrev ? "Sao từ đợt trước" : undefined}
              value={raw}
              onChange={(e) => setPv((x) => ({ ...x, [f.template_id]: { ...(x[f.template_id] ?? {}), [cm.commune_code]: { ...(x[f.template_id]?.[cm.commune_code] ?? {}), [c.letter]: e.target.value } } }))}
              onBlur={() => {
                const p = parseNumberInput(raw);
                setPvErr((x) => {
                  const nx = { ...x };
                  if (p.ok) delete nx[k];
                  else nx[k] = p.error;
                  return nx;
                });
              }}
            />
            {pvErr[k] ? (
              <span className="err" role="alert">
                {pvErr[k]}
              </span>
            ) : null}
          </>
        );
      },
    })),
  ];
  const inPreview = new Set(previewCommunes.map((c) => c.commune_code));
  const outsideImport = pvImport ? pvImport.rows.filter((x) => x.status === "accepted" && previewCommunes.length > 0 && !inPreview.has(x.commune_code)) : [];
  const shownCommunes = allCommunes.filter((c) => !communeQ || `${c.commune_code} ${name(c.commune_code)} ${c.region_name}`.toLowerCase().includes(communeQ.toLowerCase()));
  const pvSummary = splitProvincial(pv, copied, picked, prevPv, preview ? previewCommunes.map((c) => c.commune_code) : undefined);
  const pvCount = Object.values(pvSummary.provincial).reduce((s, m) => s + Object.keys(m).length, 0);

  return (
    <>
      <PageHeader
        title="Phát hành biểu mẫu báo cáo"
        actions={
          <Link className="btn btn-o" href="/kn/nhiem-vu/phat-hanh/dot-da-phat-hanh">
            Đợt đã phát hành ({batches.data?.items.length ?? 0})
          </Link>
        }
      />
      <nav aria-label="Các bước phát hành" className="card sec" style={{ marginBottom: 12 }}>
        <ol className="row" style={{ gap: 8, flexWrap: "wrap", listStyle: "none", margin: 0, padding: 0 }}>
          {STEPS.map((label, i) => {
            const n = i + 1;
            return (
              <li key={label}>
                <button
                  type="button"
                  className={`btn btn-sm ${n === step ? "btn-p" : "btn-o"}`}
                  aria-current={n === step ? "step" : undefined}
                  disabled={n > step || act.busy}
                  onClick={() => (setStepErr(undefined), setStep(n))}
                >
                  {n < step ? <Icon name="check" size={14} /> : null} Bước {n} · {label}
                </button>
              </li>
            );
          })}
        </ol>
      </nav>
      {act.view}

      {step === 1 ? (
        <section className="card sec" aria-label="Bước 1 · Chọn biểu mẫu">
          <h2>Bước 1 · Chọn biểu mẫu</h2>
          <FilterBar>
            <FilterChip
              label="Loại báo cáo"
              value={rt}
              defaultValue="weekly"
              onChange={(v) => (setRt(v as PublishReportType), setPt(""), setPicked([]), dropPv(), resetFrom(1))}
              options={REPORT_TYPES}
            />
          </FilterBar>
          <LoadState s={forms}>
            {(d) => <DataTable caption="Biểu mẫu" columns={formCols} rows={d.items} rowKey={(f) => f.template_id} empty="Chưa có mẫu đã phát hành gán cho loại báo cáo này (FN-AD-04.06)." />}
          </LoadState>
          <p className="small mute">Đã chọn: {pickedForms.map((f) => f.code).join(", ") || "—"}</p>
        </section>
      ) : null}

      {step === 2 ? (
        <section className="card sec" aria-label="Bước 2 · Chọn xã/phường nhận">
          <h2>Bước 2 · Chọn xã/phường nhận</h2>
          <div className="row" role="radiogroup" aria-label="Phạm vi xã">
            {(
              [
                ["province", "Toàn tỉnh"],
                ["regions", "Theo Khu vực"],
                ["communes", "Chọn từng xã"],
              ] as const
            ).map(([v, l]) => (
              <button key={v} type="button" role="radio" aria-checked={mode === v} className={`btn btn-sm ${mode === v ? "btn-p" : "btn-o"}`} onClick={() => (setMode(v), resetFrom(2))}>
                {l}
              </button>
            ))}
          </div>
          <LoadState s={provinceScope}>
            {() => (
              <>
                {mode === "province" ? <p className="small mute">Toàn tỉnh: {allCommunes.length} xã/phường.</p> : null}
                {mode === "regions" ? (
                  <fieldset style={{ marginTop: 8, border: 0, padding: 0 }}>
                    <legend className="small">Khu vực</legend>
                    {regionList.map(([code, rn]) => (
                      <label key={code} className="row" style={{ gap: 4 }}>
                        <input type="checkbox" checked={regions.includes(code)} onChange={(e) => (setRegions((r) => (e.target.checked ? [...r, code] : r.filter((x) => x !== code))), resetFrom(2))} /> {rn} ({allCommunes.filter((c) => c.region_code === code).length} xã)
                      </label>
                    ))}
                    {!regionList.length ? <p className="mute">Tỉnh chưa chia khu vực.</p> : null}
                  </fieldset>
                ) : null}
                {mode === "communes" ? (
                  <div style={{ marginTop: 8 }}>
                    <div className="row" style={{ gap: 8, alignItems: "center" }}>
                      <SearchBox value={communeQ} onChange={setCommuneQ} placeholder="Tìm xã/phường" />
                      <button type="button" className="btn btn-o btn-sm" onClick={() => (setCommunes([...new Set([...communes, ...shownCommunes.map((c) => c.commune_code)])]), resetFrom(2))}>
                        Chọn tất cả ({shownCommunes.length})
                      </button>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => (setCommunes([]), resetFrom(2))}>
                        Bỏ chọn
                      </button>
                      <span className="small">Đã chọn {communes.length} xã</span>
                    </div>
                    <fieldset style={{ border: 0, padding: 0, marginTop: 8, maxHeight: 320, overflow: "auto" }}>
                      <legend className="sr-only">Xã/phường nhận</legend>
                      {shownCommunes.map((c) => (
                        <label key={c.commune_code} className="row" style={{ gap: 4 }}>
                          <input
                            type="checkbox"
                            checked={communes.includes(c.commune_code)}
                            onChange={(e) => (setCommunes((x) => (e.target.checked ? [...x, c.commune_code] : x.filter((y) => y !== c.commune_code))), resetFrom(2))}
                          />
                          {name(c.commune_code)}
                          {name(c.commune_code) !== c.commune_code ? ` (${c.commune_code})` : ""} · {c.region_name}
                          {c.officer_count ? "" : " (chưa có cán bộ phụ trách)"}
                        </label>
                      ))}
                    </fieldset>
                  </div>
                ) : null}
              </>
            )}
          </LoadState>
        </section>
      ) : null}

      {step === 3 ? (
        <section className="card sec" aria-label="Bước 3 · Kỳ và hạn nộp">
          <h2>Bước 3 · Kỳ và hạn nộp</h2>
          <p className="small mute">Loại báo cáo: {REPORT_TYPES.find((r) => r.value === rt)?.label}. Hệ thống tự chia kỳ theo lịch kỳ của phòng.</p>
          <label className="field">
            <span>Loại kỳ (chia kỳ theo)</span>
            <select className="input" value={pt} onChange={(e) => (setPt(e.target.value as PeriodChoice), resetFrom(3))}>
              {PERIOD_CHOICES[rt].map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </label>
          {pt && from < REPORT_RULE_V2_FROM ? (
            <p className="small" role="note" style={{ color: "var(--warn, #b45309)" }}>
              Kỳ ngày / tuần KSGM / quý theo lịch kỳ hệ thống mới — đợt phải bắt đầu từ {REPORT_RULE_V2_FROM.split("-").reverse().join("/")}.
            </p>
          ) : null}
          <DateInput label="Từ ngày" value={from} onChange={(v) => (setFrom(v), resetFrom(3))} error={errors.from} />
          <DateInput label="Đến ngày" value={to} onChange={(v) => (setTo(v), resetFrom(3))} error={errors.to} />
          <TextField label="Vụ / đợt" required value={season} onChange={setSeason} error={errors.season} maxLength={120} />
          <label className="field">
            <span>Hạn nộp mỗi kỳ</span>
            <select className="input" value={deadline} onChange={(e) => setDeadline(e.target.value)}>
              {DEADLINES.map((d) => (
                <option key={d.value} value={d.value}>
                  {d.label}
                </option>
              ))}
            </select>
          </label>
          <NumberField label="Số ngày chốt sau hạn" unit="ngày" required value={closeAfter} onChange={setCloseAfter} rule={{ integer: true, max: 60 }} error={errors.closeAfter} onBlurError={(e) => setErrors((x) => ({ ...x, closeAfter: e }))} />
          <TextField label="Ghi chú giao việc" value={note} onChange={setNote} maxLength={500} hint={<PiiHint text={note} />} />
        </section>
      ) : null}

      {step === 4 ? (
        <section className="card sec" aria-label="Bước 4 · Chỉ tiêu Tỉnh giao">
          <h2>Bước 4 · Chỉ tiêu &quot;Tỉnh giao&quot; theo xã</h2>
          {!provCols.length ? (
            <p className="mute">Các biểu đã chọn không có cột Tỉnh giao — bấm “Tiếp” để xem trước.</p>
          ) : (
            <>
              <div className="row" style={{ gap: 8, marginBottom: 10, flexWrap: "wrap" }} aria-label="Nguồn Tỉnh giao">
                <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !batches.data} onClick={() => void copyPrevious()}>
                  <Icon name="refresh" size={14} /> Sao từ đợt trước
                </button>
                <button type="button" className="btn btn-o btn-sm" disabled={!previewCommunes.length} onClick={downloadPvTemplate}>
                  <Icon name="download" size={14} /> Tải tệp mẫu
                </button>
                <label className="btn btn-o btn-sm">
                  <Icon name="upload" size={14} /> Nạp từ Excel/CSV
                  <input
                    type="file"
                    accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                    aria-label="Tệp Tỉnh giao"
                    style={{ display: "none" }}
                    disabled={act.busy || !picked.length}
                    onChange={(e) => {
                      const file = e.target.files?.[0];
                      e.target.value = "";
                      if (file) void importPv(file);
                    }}
                  />
                </label>
                {pvSource === "excel" ? <StatusPill tone="info">Nguồn: Excel</StatusPill> : null}
                {pvSummary.copiedRows ? <StatusPill tone="info">Sao từ đợt trước: {pvSummary.copiedRows}</StatusPill> : null}
              </div>
              {copyNote ? (
                <p className="small" role="status">
                  {copyNote}
                </p>
              ) : null}
              <DataTable caption="Giá trị Tỉnh giao theo xã" columns={pvCols} rows={previewCommunes} rowKey={(cm) => cm.commune_code} empty="Không có xã nhận đợt này." />
              {pvImport ? (
                <div className="small" role="status" aria-label="Kết quả nạp Tỉnh giao" style={{ marginTop: 8 }}>
                  <div>
                    Cột nhận: {pvImport.columns.map((c) => `${c.form_code} ${c.letter}`).join(", ")}
                    {pvImport.ignored_columns.length ? ` · bỏ qua: ${pvImport.ignored_columns.join(", ")}` : ""}
                  </div>
                  {pvImport.rows
                    .filter((x) => x.status === "rejected")
                    .map((x) => (
                      <div key={x.row} style={{ color: "var(--danger)" }}>
                        Dòng {x.row}
                        {x.commune_code ? ` (${x.commune_code})` : ""}: {x.reason}
                      </div>
                    ))}
                  {outsideImport.length ? <div style={{ color: "var(--warn, #b45309)" }}>Xã không nhận đợt này (sẽ bị từ chối khi phát hành): {outsideImport.map((x) => x.commune_code).join(", ")}</div> : null}
                </div>
              ) : null}
              <p className="small mute" style={{ marginTop: 8 }}>
                Nhập tay trực tiếp vào bảng, nạp tệp (một dòng một xã: cột “Mã xã” và mỗi cột Tỉnh giao có tiêu đề “&lt;Mã biểu&gt; &lt;Chữ cột&gt;”, vd “T01 E”; ô công thức bị từ chối) hoặc sao từ đợt
                đã phát hành gần nhất có cùng biểu. Cấp Xã thấy giá trị này ở dạng chỉ đọc.
              </p>
            </>
          )}
        </section>
      ) : null}

      {step === 5 && preview ? (
        <section className="card sec" aria-label="Bước 5 · Xem trước">
          <h2>Bước 5 · Xem trước và phát hành</h2>
          <dl className="kv">
            <dt>Biểu</dt>
            <dd>{pickedForms.map((f) => f.code).join(", ")}</dd>
            <dt>Số xã</dt>
            <dd>{preview.commune_count}</dd>
            <dt>Số kỳ</dt>
            <dd>{preview.period_count}</dd>
            <dt>Lịch tuần</dt>
            <dd data-testid="calendar">
              {weekRangeName(weekStartOfRule(preview.period_rule))} ({preview.calendar_id} v{preview.calendar_version})
            </dd>
            <dt>Vụ / đợt</dt>
            <dd>{season}</dd>
            <dt>Tỉnh giao</dt>
            <dd>
              {provCols.length ? `${pvCount} dòng xã × biểu nhập tay/Excel · ${pvSummary.copiedRows} dòng sao từ đợt trước` : "Không có cột Tỉnh giao"}
            </dd>
            <dt>Công việc sẽ tạo</dt>
            <dd data-testid="task-count">{preview.task_count.toLocaleString("vi-VN")}</dd>
          </dl>
          {preview.communes_without_officer.length ? (
            <div className="alert alert-warn" role="status">
              <Icon name="alert" />
              <span>
                {preview.communes_without_officer.length} xã chưa có cán bộ phụ trách: {preview.communes_without_officer.map(name).join(", ")}
              </span>
            </div>
          ) : null}
          <h3>Kỳ do hệ thống chia</h3>
          <ol className="list-plain small">
            {preview.periods.map((p) => (
              <li key={p.slot}>
                {p.label} ({p.range_label}){p.period_key ? ` · ${p.period_key}` : ""} — hạn {dt(p.due_at)}
              </li>
            ))}
          </ol>
          {preview.warnings.map((w) => (
            <p key={w} className="small" style={{ color: "var(--warn, #b45309)" }}>
              {w}
            </p>
          ))}
        </section>
      ) : null}

      <StepError text={stepErr} />
      <div className="row" style={{ gap: 8, marginTop: 12 }}>
        {step > 1 ? (
          <button type="button" className="btn btn-o" disabled={act.busy} onClick={() => (setStepErr(undefined), setStep((s) => s - 1))}>
            ← Quay lại
          </button>
        ) : null}
        {step < 5 ? (
          <button type="button" className="btn btn-p" disabled={act.busy} onClick={() => void next()}>
            Tiếp →
          </button>
        ) : (
          <button type="button" className="btn btn-p" disabled={act.busy || !preview} onClick={() => void issue()}>
            <Icon name="send" size={15} /> Phát hành
          </button>
        )}
      </div>
    </>
  );
}
