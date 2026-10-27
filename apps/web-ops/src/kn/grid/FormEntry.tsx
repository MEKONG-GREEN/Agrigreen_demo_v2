"use client";

// Màn nhập một mẫu báo cáo (dùng chung cho xem trước của trình thiết kế — P3b — và màn nhập của Cấp Xã — P4):
// mỗi xã phụ trách một tab nhập (FN-CH-00.01 "mỗi xã một tab khi nhập"), tab "Tất cả các xã" chỉ xem kèm dòng
// Cộng khu vực / Toàn tỉnh; ô công thức và dòng tổng do máy chủ tính (useFormEvaluation); vấn đề tại ô.
import { useMemo, useState } from "react";
import type { FormCellIssue, FormDefinition } from "@/kn/forms/types";
import { FormGrid, GridLegend } from "./FormGrid";
import { allCommuneRows, communeRows, ISSUE_LABEL, type CellChange, type EvalInputOptions, type GridCommune, type GridValues } from "./model";
import { useFormEvaluation, type FormEvaluator } from "./useFormEvaluation";

export interface FormEntryProps {
  definition: FormDefinition;
  /** Khóa phiên bản (id) — đổi thì tính lại ngay. */
  versionKey: string;
  communes: readonly GridCommune[];
  values: GridValues;
  onValuesChange?: (next: GridValues, changes: CellChange[]) => void;
  evaluate: FormEvaluator | null;
  openSlots?: number[] | null;
  system?: EvalInputOptions["system"];
  xrefs?: EvalInputOptions["xrefs"];
  readOnly?: boolean;
  /** Nhãn trạng thái mỗi tab xã (vd "Lưu nháp", "Chưa nhập"). */
  tabStatus?: Record<string, string>;
  ariaLabel?: string;
  maxHeight?: number | string;
  debounceMs?: number;
}

export const ALL_TAB = "__all__";

/** Áp thay đổi ô lên bảng giá trị (bất biến theo dòng — dòng không đổi giữ tham chiếu). */
export function applyChanges(values: GridValues, changes: readonly CellChange[]): GridValues {
  const next: GridValues = { ...values };
  for (const ch of changes) {
    const row = { ...(next[ch.rowKey] ?? {}) };
    if (ch.value === "") delete row[ch.letter];
    else row[ch.letter] = ch.value;
    next[ch.rowKey] = row;
  }
  return next;
}

export function FormEntry({
  definition,
  versionKey,
  communes,
  values,
  onValuesChange,
  evaluate,
  openSlots = null,
  system,
  xrefs,
  readOnly = false,
  tabStatus,
  ariaLabel,
  maxHeight,
  debounceMs,
}: FormEntryProps) {
  const [tab, setTab] = useState<string>(communes[0]?.code ?? ALL_TAB);
  const [focus, setFocus] = useState<{ rowKey: string; letter: string; nonce: number } | null>(null);
  const [pasteNote, setPasteNote] = useState<string | null>(null);
  const ev = useFormEvaluation({ definition, communes, values, evaluate, openSlots: null, system, xrefs, debounceMs, versionKey });
  const current = tab === ALL_TAB ? null : communes.find((c) => c.code === tab) ?? null;
  const rows = useMemo(() => (current ? communeRows(definition, current) : allCommuneRows(definition, communes)), [current, definition, communes]);
  const issues = ev.result?.issues ?? [];
  const visibleIssues = current ? issues.filter((i) => i.row_key === current.code || i.row_key.startsWith(`${current.code}/`)) : issues;
  const errors = visibleIssues.filter((i) => i.severity === "error").length;
  const warnings = visibleIssues.length - errors;
  const name = (i: FormCellIssue) => {
    const [commune] = i.row_key.split("/");
    return communes.find((c) => c.code === commune)?.name ?? i.row_key;
  };
  const onChange = readOnly || !onValuesChange ? undefined : (changes: CellChange[]) => onValuesChange(applyChanges(values, changes), changes);
  const issueCount = (code: string) => issues.filter((i) => (i.row_key === code || i.row_key.startsWith(`${code}/`)) && i.severity === "error").length;

  return (
    <div className="fentry">
      <div className="ftabs" role="tablist" aria-label="Xã nhập liệu">
        {communes.map((c) => (
          <button key={c.code} type="button" role="tab" aria-selected={tab === c.code} className="ftab" onClick={() => setTab(c.code)}>
            <b>{c.name}</b>
            {tabStatus?.[c.code] ? <span className="ftab-st">{tabStatus[c.code]}</span> : null}
            {issueCount(c.code) ? <span className="count-pill" aria-label={`${issueCount(c.code)} lỗi`}>{issueCount(c.code)}</span> : null}
          </button>
        ))}
        <button type="button" role="tab" aria-selected={tab === ALL_TAB} className="ftab ftab-all" onClick={() => setTab(ALL_TAB)}>
          <b>Tất cả các xã</b>
          <span className="ftab-st">Chỉ xem · có dòng tổng</span>
        </button>
      </div>
      <div className="card fentry-body" role="tabpanel">
        {/* UP-04: khung "Yêu cầu biểu mẫu" luôn có (như ô A1 của tệp gốc); mẫu chưa ghi yêu cầu thì nói rõ. */}
        <div className="alert alert-warn freq" role="note" aria-label="Yêu cầu biểu mẫu">
          <div>
            <b>*Yêu cầu biểu mẫu:</b>
            {definition.requirements ? (
              definition.requirements
                .split(/\r?\n/)
                .map((l) => l.replace(/^\s*-\s*/, "").trim())
                .filter(Boolean)
                .map((l, i) => <div key={i}>- {l}</div>)
            ) : (
              <div className="mute">- Mẫu chưa ghi yêu cầu riêng.</div>
            )}
          </div>
        </div>
        <FormGrid
          definition={definition}
          rows={rows}
          values={values}
          computed={ev.computed}
          system={system}
          communes={communes}
          openSlots={openSlots}
          readOnly={readOnly || tab === ALL_TAB}
          issues={issues}
          onChange={tab === ALL_TAB ? undefined : onChange}
          onPasteResult={(applied, skipped) => setPasteNote(`Đã dán ${applied} ô${skipped ? `, bỏ qua ${skipped} ô không nhập được (cố định, Tỉnh giao, công thức, kỳ đã đóng)` : ""}.`)}
          ariaLabel={ariaLabel ?? `${definition.code} · ${current?.name ?? "Tất cả các xã"}`}
          maxHeight={maxHeight}
          focusCell={focus}
        />
        <div className="fentry-foot">
          <span className="mute small" aria-live="polite">
            {ev.pending ? "Đang tính trên máy chủ…" : ev.problem ? `Không tính được: ${ev.problem.message}` : ev.result ? (ev.result.valid ? "Đã tính — không có lỗi." : `Có ${errors} lỗi, ${warnings} cảnh báo.`) : ""}
            {pasteNote ? ` ${pasteNote}` : ""}
          </span>
          <GridLegend />
        </div>
        {visibleIssues.length ? (
          <section className="fissues" aria-label="Vấn đề tại ô">
            <h3>
              Vấn đề tại ô <span className="count-pill">{visibleIssues.length}</span>
            </h3>
            <ul>
              {visibleIssues.slice(0, 50).map((i, k) => (
                <li key={`${i.row_key}-${i.letter ?? ""}-${i.code}-${k}`} className={i.severity === "error" ? "is-err" : "is-warn"}>
                  <button
                    type="button"
                    onClick={() => {
                      const [commune] = i.row_key.split("/");
                      if (tab !== ALL_TAB && commune !== tab) setTab(commune ?? ALL_TAB);
                      setFocus({ rowKey: i.row_key, letter: i.letter ?? definition.columns[0]!.letter, nonce: Date.now() });
                    }}
                  >
                    <b>{i.severity === "error" ? "Lỗi" : "Cảnh báo"}</b> · {name(i)}
                    {i.letter ? ` · Cột ${i.letter}` : ""}: {ISSUE_LABEL[i.code] ?? i.code} — {i.message}
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>
    </div>
  );
}
