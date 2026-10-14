"use client";

// Thân phiếu "Báo cáo cho KHTC" dùng chung cho màn nhập của phòng (FN-PN-03.02, Hình 22–25) và màn rà soát của KHTC
// (FN-TI-02.04, Hình 11): thanh tab chương trình; lưới theo mẫu với hàng chữ cái cột và dòng loại cột; ô nhập viền xanh,
// ô cố định/Từ Kế hoạch năm nền xám, Lũy kế kỳ trước xám nhạt, ô tự tính xanh nhạt, vượt ngưỡng đỏ nhạt; khối "Điều chỉnh
// kỳ trước"; mục lời văn; kết quả kiểm tra (3 quy tắc); lịch sử gửi.
import { useMemo, useState } from "react";
import { StatusPill } from "@/components/patterns/list";
import { dt, num } from "@/kn/cycle/common";
import {
  COLUMN_KIND_LABEL,
  RULE_LABEL,
  liveCells,
  type KhtcFormAdjustment,
  type KhtcFormCell,
  type KhtcFormColumn,
  type KhtcFormContent,
  type KhtcFormIssue,
  type KhtcFormSheet,
} from "./api";

const KIND_BG: Record<KhtcFormColumn["kind"], string> = {
  fixed: "#eef1f4",
  from_plan: "#eef1f4",
  prior_cumulative: "#f6f7f9",
  input: "#fff",
  formula: "#eaf6ef",
};

function issueKey(i: KhtcFormIssue): string {
  return `${i.entry_ref ?? ""}:${i.letter ?? ""}`;
}

export function KhtcFormView({
  form,
  content,
  onChange,
}: {
  form: KhtcFormSheet;
  content: KhtcFormContent;
  /** Có → ô nhập sửa được (phòng, phiếu đang mở cho phòng). */
  onChange?: (c: KhtcFormContent) => void;
}) {
  const editable = !!onChange;
  const [tab, setTab] = useState(0);
  const cols = form.template.columns;
  const issues = useMemo(() => {
    const m = new Map<string, KhtcFormIssue[]>();
    for (const i of form.issues)
      m.set(issueKey(i), [...(m.get(issueKey(i)) ?? []), i]);
    return m;
  }, [form.issues]);
  const adjYears = useMemo(
    () => Object.fromEntries(form.adj_periods.map((p) => [p.id, p.year])),
    [form.adj_periods],
  );
  const rows = form.tabs.flatMap((t) => t.rows);
  const cur = form.tabs[Math.min(tab, Math.max(form.tabs.length - 1, 0))];

  const setCell = (ref: string, key: keyof KhtcFormCell, v: string) => {
    if (!onChange) return;
    const cells = {
      ...content.cells,
      [ref]: { ...(content.cells[ref] ?? {}), [key]: v },
    };
    onChange({ ...content, cells });
  };
  const setAdj = (i: number, patch: Partial<KhtcFormAdjustment>) => {
    if (!onChange) return;
    onChange({
      ...content,
      adjustments: content.adjustments.map((a, j) =>
        j === i ? { ...a, ...patch } : a,
      ),
    });
  };
  const cellText = (row: (typeof rows)[number], c: KhtcFormColumn): string => {
    if (editable && c.kind === "formula") {
      const l = liveCells(row, content, adjYears, form.period.year);
      if (row.module_sourced) return "";
      if (c.formula === "adjust")
        return l.G === null ? "" : num(String(l.G), 6);
      if (c.formula === "cumulative") return num(String(l.H), 6);
      if (c.formula === "percent") return l.I;
    }
    const v = row.cells[c.letter] ?? "";
    if (c.formula === "percent") return v ? num(v, 1) : "";
    return c.dtype === "number" && v ? num(v, 6) : v;
  };

  return (
    <div className="khtc-form">
      {form.grid ? (
        form.plan_missing ? (
          <p className="alert alert-warn" role="status">
            Chưa có Kế hoạch năm {form.period.year} hiệu lực tại ngày chốt —
            phiếu chưa có dòng hạng mục.
          </p>
        ) : form.tabs.length === 0 ? (
          <p className="mute">
            Phòng không phụ trách hạng mục nào trong Kế hoạch năm — chỉ nhập lời
            văn.
          </p>
        ) : (
          <>
            <div className="tabs" role="tablist" aria-label="Chương trình">
              {form.tabs.map((t, i) => (
                <button
                  key={t.key}
                  type="button"
                  role="tab"
                  aria-selected={t === cur}
                  onClick={() => setTab(i)}
                >
                  {t.label}
                </button>
              ))}
            </div>
            <div style={{ overflowX: "auto" }}>
              <table
                className="dt"
                aria-label={`Bảng số liệu ${cur?.label ?? ""}`}
              >
                <thead>
                  <tr>
                    {cols.map((c) => (
                      <th key={c.letter} style={{ textAlign: "center" }}>
                        {c.letter}
                      </th>
                    ))}
                  </tr>
                  <tr>
                    {cols.map((c) => (
                      <th key={c.letter} title={c.note}>
                        {c.title}
                        {c.unit ? ` (${c.unit})` : ""}
                      </th>
                    ))}
                  </tr>
                  <tr aria-label="Loại cột">
                    {cols.map((c) => (
                      <th
                        key={c.letter}
                        className="mute"
                        style={{ fontSize: 11 }}
                      >
                        {COLUMN_KIND_LABEL[c.kind]}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {cur?.rows.map((r) => {
                    const over = editable
                      ? liveCells(r, content, adjYears, form.period.year).over
                      : r.over_plan;
                    return (
                      <tr key={r.entry_ref} data-entry={r.entry_ref}>
                        {cols.map((c) => {
                          const bad = issues.get(`${r.entry_ref}:${c.letter}`);
                          const style = {
                            background:
                              bad?.some((i) => i.severity === "error") ||
                              (over &&
                                c.kind === "formula" &&
                                c.formula !== "adjust")
                                ? "#fdecec"
                                : KIND_BG[c.kind],
                            minWidth:
                              c.kind === "input" && c.dtype === "text"
                                ? 160
                                : undefined,
                          };
                          const title = bad?.map((i) => i.message).join("; ");
                          if (c.kind === "input" && c.input) {
                            if (r.module_sourced && c.input === "done")
                              return (
                                <td
                                  key={c.letter}
                                  style={style}
                                  title="Lấy từ module (BR-DL-10) — không nhập"
                                >
                                  {r.module_filled && r.cells["F"] ? (
                                    <>
                                      {num(r.cells["F"], 6)}{" "}
                                      {r.module_provisional ? (
                                        <span className="mute">(Tạm tính)</span>
                                      ) : null}
                                    </>
                                  ) : (
                                    <span className="mute">Lấy từ module</span>
                                  )}
                                </td>
                              );
                            const v =
                              content.cells[r.entry_ref]?.[c.input] ?? "";
                            return (
                              <td key={c.letter} style={style} title={title}>
                                {editable ? (
                                  <input
                                    className="input"
                                    aria-label={`${c.letter} ${r.name}`}
                                    inputMode={
                                      c.dtype === "text" ? "text" : "decimal"
                                    }
                                    value={v}
                                    style={{
                                      border: "1px solid #3b82f6",
                                      width: c.dtype === "text" ? 160 : 90,
                                    }}
                                    onChange={(e) =>
                                      setCell(
                                        r.entry_ref,
                                        c.input as keyof KhtcFormCell,
                                        e.target.value,
                                      )
                                    }
                                  />
                                ) : c.dtype === "text" ? (
                                  v
                                ) : v ? (
                                  num(v, 6)
                                ) : (
                                  ""
                                )}
                              </td>
                            );
                          }
                          return (
                            <td key={c.letter} style={style} title={title}>
                              {cellText(r, c)}
                            </td>
                          );
                        })}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <h3>Điều chỉnh kỳ trước</h3>
            <p className="mute">
              Số bù (dương) hoặc trừ (âm) cho kỳ đã khóa; bắt buộc chọn kỳ được
              điều chỉnh và lý do. Chỉ cộng vào lũy kế, không vào số trong kỳ
              (BR-DL-12). Kỳ năm trước chỉ lưu vết.
            </p>
            <table className="dt" aria-label="Điều chỉnh kỳ trước">
              <thead>
                <tr>
                  <th>Hạng mục</th>
                  <th>Kỳ được điều chỉnh</th>
                  <th>Số điều chỉnh</th>
                  <th>Lý do</th>
                  {editable ? <th /> : null}
                </tr>
              </thead>
              <tbody>
                {content.adjustments.map((a, i) => {
                  const bad = form.issues.filter((x) => x.adjustment === i);
                  return (
                    <tr
                      key={i}
                      style={bad.length ? { background: "#fdecec" } : undefined}
                      title={bad.map((x) => x.message).join("; ")}
                    >
                      <td>
                        {editable ? (
                          <select
                            className="input"
                            aria-label="Hạng mục điều chỉnh"
                            value={a.entry_ref}
                            onChange={(e) =>
                              setAdj(i, { entry_ref: e.target.value })
                            }
                          >
                            <option value="">— chọn —</option>
                            {rows
                              .filter((r) => !r.module_sourced)
                              .map((r) => (
                                <option key={r.entry_ref} value={r.entry_ref}>
                                  {r.name}
                                </option>
                              ))}
                          </select>
                        ) : (
                          (rows.find((r) => r.entry_ref === a.entry_ref)
                            ?.name ?? a.entry_ref)
                        )}
                      </td>
                      <td>
                        {editable ? (
                          <select
                            className="input"
                            aria-label="Kỳ được điều chỉnh"
                            value={a.period_id}
                            onChange={(e) =>
                              setAdj(i, { period_id: e.target.value })
                            }
                          >
                            <option value="">— chọn kỳ đã khóa —</option>
                            {form.adj_periods.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                          </select>
                        ) : (
                          (a.period_name ??
                          form.adj_periods.find((p) => p.id === a.period_id)
                            ?.name ??
                          a.period_id)
                        )}
                      </td>
                      <td>
                        {editable ? (
                          <input
                            className="input"
                            aria-label="Số điều chỉnh"
                            inputMode="decimal"
                            value={a.amount}
                            style={{ width: 90 }}
                            onChange={(e) =>
                              setAdj(i, { amount: e.target.value })
                            }
                          />
                        ) : (
                          num(a.amount, 6)
                        )}
                      </td>
                      <td>
                        {editable ? (
                          <input
                            className="input"
                            aria-label="Lý do điều chỉnh"
                            value={a.reason}
                            onChange={(e) =>
                              setAdj(i, { reason: e.target.value })
                            }
                          />
                        ) : (
                          a.reason
                        )}
                      </td>
                      {editable ? (
                        <td>
                          <button
                            type="button"
                            className="btn"
                            onClick={() =>
                              onChange!({
                                ...content,
                                adjustments: content.adjustments.filter(
                                  (_, j) => j !== i,
                                ),
                              })
                            }
                          >
                            Bỏ
                          </button>
                        </td>
                      ) : null}
                    </tr>
                  );
                })}
                {content.adjustments.length === 0 ? (
                  <tr>
                    <td colSpan={editable ? 5 : 4} className="mute">
                      Không có điều chỉnh.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
            {editable ? (
              <button
                type="button"
                className="btn"
                disabled={form.adj_periods.length === 0}
                title={
                  form.adj_periods.length === 0
                    ? "Chưa có kỳ KHTC tuần đã khóa"
                    : undefined
                }
                onClick={() =>
                  onChange!({
                    ...content,
                    adjustments: [
                      ...content.adjustments,
                      { entry_ref: "", period_id: "", amount: "", reason: "" },
                    ],
                  })
                }
              >
                Thêm điều chỉnh
              </button>
            ) : null}
          </>
        )
      ) : (
        <p className="mute">
          Phiếu {form.period.report_type === "week" ? "" : "tháng, quý, năm "}
          chỉ nhập lời văn (BR-DL-06).
        </p>
      )}

      <h3>Lời văn</h3>
      {form.template.sections.map((s) => {
        const bad = form.issues.filter((i) => i.section === s.key);
        return (
          <label
            key={s.key}
            className="field"
            style={{ display: "block", marginBottom: 12 }}
          >
            <span>
              {s.title}
              {s.required ? " *" : ""}
            </span>
            {editable ? (
              <textarea
                className="input"
                rows={3}
                style={{ width: "100%" }}
                aria-label={s.title}
                value={content.texts[s.key] ?? ""}
                onChange={(e) =>
                  onChange!({
                    ...content,
                    texts: { ...content.texts, [s.key]: e.target.value },
                  })
                }
              />
            ) : (
              <p style={{ whiteSpace: "pre-wrap" }}>
                {content.texts[s.key] || <span className="mute">—</span>}
              </p>
            )}
            {bad.length ? (
              <span className="bad">
                {bad.map((i) => i.message).join("; ")}
              </span>
            ) : null}
          </label>
        );
      })}

      <h3>Kết quả kiểm tra</h3>
      {form.issues.length === 0 ? (
        <p className="mute">Đạt 3 quy tắc kiểm tra.</p>
      ) : (
        <ul aria-label="Kết quả kiểm tra">
          {form.issues.map((i, k) => (
            <li key={k}>
              <StatusPill tone={i.severity === "error" ? "bad" : "warn"}>
                {RULE_LABEL[i.rule]}
              </StatusPill>{" "}
              {i.entry_ref
                ? `${rows.find((r) => r.entry_ref === i.entry_ref)?.name ?? ""} ${i.letter ?? ""}: `
                : ""}
              {i.adjustment >= 0 ? `Điều chỉnh dòng ${i.adjustment + 1}: ` : ""}
              {i.message}
            </li>
          ))}
        </ul>
      )}

      <h3>Lịch sử gửi</h3>
      {form.submissions.length === 0 ? (
        <p className="mute">Chưa gửi lần nào.</p>
      ) : (
        <ul aria-label="Lịch sử gửi">
          {form.submissions.map((s) => (
            <li key={s.id}>
              Lần {s.attempt} — {dt(s.submitted_at)}
              {s.late ? " (nộp trễ)" : ""}
              {s.decision === "returned"
                ? ` · Yêu cầu bổ sung: ${s.review_note}`
                : s.decision === "accepted"
                  ? " · Đã xác nhận"
                  : ""}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
