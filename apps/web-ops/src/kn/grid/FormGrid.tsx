"use client";

// Lưới mẫu báo cáo đúng bố cục sheet Excel (FN-XA-03.02, FN-AD-04.06; Hình 14, 15, 27): hàng chữ cái cột,
// tiêu đề gộp nhiều tầng, cột cố định (freeze_through), cột theo kỳ chỉ khe đang mở được nhập, ô công thức do
// máy chủ tính, dòng tổng, vấn đề tại ô; bàn phím như Excel (mũi tên, Enter, Tab, F2, Delete, Esc) và dán khối
// từ Excel. Ô chỉ nhận chuỗi; số là decimal dạng chuỗi (không float), hiển thị vi-VN.
import { memo, useCallback, useEffect, useId, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent, type MouseEvent } from "react";
import { KIND_SHORT, type FormCellIssue, type FormColumn, type FormDefinition } from "@/kn/forms/types";
import { columnIndex, type Rect } from "./letters";
import {
  applyPaste,
  buildHeader,
  cellRole,
  columnWidth,
  displayCell,
  editText,
  freezeModel,
  isEditableRole,
  issueIndex,
  normalizeCellInput,
  parseClipboard,
  type CellChange,
  type CellRole,
  type GridCommune,
  type GridRow,
  type GridValues,
} from "./model";

export interface FormGridProps {
  definition: FormDefinition;
  rows: readonly GridRow[];
  /** Giá trị người nhập theo dòng (decimal dạng chuỗi máy chủ). */
  values?: GridValues;
  /** Kết quả tính của máy chủ theo dòng (ô cố định, Tỉnh giao, công thức; dòng tổng). */
  computed?: GridValues;
  /** Ô hệ thống (Tỉnh giao…) hiển thị khi chưa có kết quả tính. */
  system?: GridValues;
  communes?: readonly GridCommune[];
  /** Khe kỳ đang mở; null/undefined = mọi khe mở (xem trước). */
  openSlots?: readonly number[] | null;
  readOnly?: boolean;
  issues?: readonly FormCellIssue[];
  onChange?: (changes: CellChange[]) => void;
  /** Báo số ô bị bỏ qua khi dán (không nhập được hoặc ra ngoài lưới). */
  onPasteResult?: (applied: number, skipped: number) => void;
  ariaLabel: string;
  /** Trình thiết kế: dòng loại cột (Cố định, Tỉnh giao, Nhập, Kỳ, ƒx), chọn cột, chọn vùng tiêu đề. */
  showKindRow?: boolean;
  selectedColumn?: string | null;
  onSelectColumn?: (letter: string) => void;
  headerSelection?: Rect | null;
  onHeaderCellClick?: (row: number, col: number, extend: boolean) => void;
  maxHeight?: number | string;
  /** Ô đang chọn từ ngoài (vd bấm một vấn đề trong danh sách). */
  focusCell?: { rowKey: string; letter: string; nonce: number } | null;
  /** Ẩn ô "Đi tới nhóm cột" (khi trang tự đặt). */
  hideGroupJump?: boolean;
}

interface Active {
  r: number;
  c: number;
}

interface Editing extends Active {
  text: string;
  /** "replace": bắt đầu bằng gõ phím — mũi tên ghi và di chuyển; "edit": F2/Enter/nhấp đúp — mũi tên trong ô. */
  mode: "replace" | "edit";
}

const ROLE_CLASS: Record<CellRole, string> = {
  fixed: "g-fixed",
  provincial: "g-prov",
  input: "g-input",
  open: "g-open",
  closed: "g-closed",
  formula: "g-formula",
  inactive: "g-inactive",
};

function fallbackCell(def: FormDefinition, row: GridRow, col: FormColumn, communes: readonly GridCommune[] | undefined): string {
  if (row.kind === "total") {
    const t = def.totals.find((x) => x.kind === row.totalKind);
    if (!t || t.label_letter.toUpperCase() !== col.letter) return "";
    const reg = communes?.find((c) => c.region === row.region);
    let label = t.label.replaceAll("{region}", reg?.regionName ?? row.region ?? "");
    const ind = row.indicator ? def.rows.indicators.find((i) => i.code === row.indicator) : undefined;
    if (ind) label += ` – ${ind.label}`;
    return label;
  }
  if (col.kind !== "fixed") return "";
  const c = communes?.find((x) => x.code === row.commune);
  switch (col.fixed_source) {
    case "commune_name":
      return c?.name ?? row.commune ?? "";
    case "region_name":
      return c?.regionName ?? row.region ?? "";
    case "indicator_label":
      return def.rows.indicators.find((i) => i.code === row.indicator)?.label ?? "";
  }
  return "";
}

interface RowViewProps {
  def: FormDefinition;
  row: GridRow;
  r: number;
  roles: CellRole[];
  values: Record<string, string> | undefined;
  computed: Record<string, string> | undefined;
  system: Record<string, string> | undefined;
  communes: readonly GridCommune[] | undefined;
  lefts: number[];
  activeC: number | null;
  editing: Editing | null;
  readOnly: boolean;
  issues: Map<string, FormCellIssue[]>;
  idBase: string;
  selectedColumn: number;
  onCellMouseDown: (r: number, c: number, e: MouseEvent) => void;
  onCellDoubleClick: (r: number, c: number) => void;
  setEditText: (t: string) => void;
  onEditorKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
  commitEdit: () => void;
}

const RowView = memo(function RowView(p: RowViewProps) {
  const { def, row } = p;
  const isTotal = row.kind === "total";
  const rowIssues = p.issues.get(`${row.key}|`);
  return (
    <tr className={isTotal ? `g-total g-total-${row.totalKind ?? ""}` : rowIssues?.length ? "g-row-issue" : undefined} aria-rowindex={p.r + 1}>
      {def.columns.map((col, c) => {
        const role = p.roles[c]!;
        const frozen = c < p.lefts.length;
        const editable = !isTotal && !p.readOnly && isEditableRole(role);
        const raw = isTotal
          ? p.computed?.[col.letter]
          : role === "input" || role === "open" || role === "closed"
            ? (p.values?.[col.letter] ?? p.computed?.[col.letter])
            : (p.computed?.[col.letter] ?? p.system?.[col.letter]);
        const shown = raw === undefined || raw === "" ? fallbackCell(def, row, col, p.communes) : displayCell(col, raw, isTotal || role === "formula");
        const iss = p.issues.get(`${row.key}|${col.letter}`);
        const sev = iss?.some((i) => i.severity === "error") ? "err" : iss?.length ? "warn" : "";
        const active = p.activeC === c;
        const isEditing = active && p.editing !== null;
        const numeric = col.dtype === "number" || col.dtype === "integer";
        const cls = [
          "g-cell",
          isTotal ? "g-tot" : ROLE_CLASS[role],
          numeric && !(col.kind === "fixed" && col.fixed_source !== "row_no") ? "g-num" : "",
          frozen ? "g-frz" : "",
          frozen && c === p.lefts.length - 1 ? "g-frz-last" : "",
          active ? "is-active" : "",
          sev ? `is-${sev}` : "",
          p.selectedColumn === c + 1 ? "is-colsel" : "",
        ]
          .filter(Boolean)
          .join(" ");
        return (
          <td
            key={col.letter}
            id={`${p.idBase}-r${p.r}-c${c}`}
            role="gridcell"
            aria-colindex={c + 1}
            aria-readonly={!editable || undefined}
            aria-selected={active || undefined}
            aria-invalid={sev === "err" || undefined}
            title={iss?.map((i) => i.message).join("\n") || undefined}
            className={cls}
            style={frozen ? { left: p.lefts[c] } : undefined}
            data-letter={col.letter}
            onMouseDown={(e) => p.onCellMouseDown(p.r, c, e)}
            onDoubleClick={() => p.onCellDoubleClick(p.r, c)}
          >
            {isEditing ? (
              <input
                className="g-edit"
                autoFocus
                aria-label={`Ô ${col.letter}`}
                value={p.editing!.text}
                inputMode={numeric ? "decimal" : undefined}
                onChange={(e) => p.setEditText(e.target.value)}
                onKeyDown={p.onEditorKeyDown}
                onBlur={p.commitEdit}
              />
            ) : (
              shown
            )}
          </td>
        );
      })}
    </tr>
  );
});

export function FormGrid({
  definition: def,
  rows,
  values = {},
  computed = {},
  system,
  communes,
  openSlots,
  readOnly = false,
  issues,
  onChange,
  onPasteResult,
  ariaLabel,
  showKindRow = false,
  selectedColumn,
  onSelectColumn,
  headerSelection,
  onHeaderCellClick,
  maxHeight = 520,
  focusCell,
  hideGroupJump = false,
}: FormGridProps) {
  const idBase = useId().replace(/:/g, "");
  const wrapRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<Active | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const cols = def.columns;
  const widths = useMemo(() => cols.map(columnWidth), [cols]);
  const baseFreeze = useMemo(() => freezeModel(def, widths), [def, widths]);
  // Khung hẹp (máy tính bảng): cột cố định không chừa nổi ~2 cột cuộn → bỏ cố định để vẫn cuộn xem được.
  const [wrapWidth, setWrapWidth] = useState(0);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => setWrapWidth(entries[0]?.contentRect.width ?? 0));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const freeze = useMemo(() => (wrapWidth > 0 && baseFreeze.width > wrapWidth - 160 ? { count: 0, lefts: [], width: 0 } : baseFreeze), [baseFreeze, wrapWidth]);
  const header = useMemo(() => buildHeader(def), [def]);
  const roles = useMemo(() => cols.map((c) => cellRole(c, openSlots)), [cols, openSlots]);
  const issueMap = useMemo(() => issueIndex(issues), [issues]);
  const lefts = useMemo(() => {
    const out: number[] = [];
    let x = 0;
    for (const w of widths) {
      out.push(x);
      x += w;
    }
    return out;
  }, [widths]);
  const totalWidth = widths.reduce((s, w) => s + w, 0);
  const selCol = selectedColumn ? columnIndex(selectedColumn) : 0;

  const canEdit = useCallback(
    (row: GridRow | undefined, c: number) => !!row && row.kind === "data" && !readOnly && !!onChange && isEditableRole(roles[c] ?? "fixed"),
    [readOnly, onChange, roles],
  );

  const ensureVisible = useCallback(
    (r: number, c: number) => {
      const wrap = wrapRef.current;
      const td = document.getElementById(`${idBase}-r${r}-c${c}`);
      if (!wrap || !td) return;
      if (c >= freeze.count) {
        const left = lefts[c] ?? td.offsetLeft;
        const w = widths[c] ?? td.offsetWidth;
        if (left < wrap.scrollLeft + freeze.width) wrap.scrollLeft = left - freeze.width;
        else if (left + w > wrap.scrollLeft + wrap.clientWidth) wrap.scrollLeft = left + w - wrap.clientWidth;
      }
      const head = wrap.querySelector("thead")?.getBoundingClientRect().height ?? 0;
      const top = td.offsetTop;
      if (top - head < wrap.scrollTop) wrap.scrollTop = Math.max(0, top - head);
      else if (top + td.offsetHeight > wrap.scrollTop + wrap.clientHeight) wrap.scrollTop = top + td.offsetHeight - wrap.clientHeight;
    },
    [idBase, freeze, lefts, widths],
  );

  const moveTo = useCallback(
    (r: number, c: number) => {
      if (!rows.length || !cols.length) return;
      const nr = Math.max(0, Math.min(rows.length - 1, r));
      const nc = Math.max(0, Math.min(cols.length - 1, c));
      setActive({ r: nr, c: nc });
      requestAnimationFrame(() => ensureVisible(nr, nc));
    },
    [rows.length, cols.length, ensureVisible],
  );

  // Ô được chọn từ ngoài (danh sách vấn đề): chỉnh state trong lúc render (không setState trong effect),
  // cuộn + đặt focus trong effect.
  const focusTarget = focusCell ? { r: rows.findIndex((x) => x.key === focusCell.rowKey), c: columnIndex(focusCell.letter) - 1 } : null;
  const [seenFocus, setSeenFocus] = useState<number | null>(null);
  if (focusCell && focusCell.nonce !== seenFocus) {
    setSeenFocus(focusCell.nonce);
    if (focusTarget && focusTarget.r >= 0 && focusTarget.c >= 0) setActive(focusTarget);
  }
  const focusR = focusTarget?.r ?? -1;
  const focusC = focusTarget?.c ?? -1;
  useEffect(() => {
    if (!focusCell || focusR < 0 || focusC < 0) return;
    const t = requestAnimationFrame(() => {
      ensureVisible(focusR, focusC);
      wrapRef.current?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(t);
    // chỉ khi có yêu cầu mới (nonce)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusCell?.nonce]);

  const commit = useCallback(
    (ed: Editing | null) => {
      if (!ed) return;
      const row = rows[ed.r];
      const col = cols[ed.c];
      setEditing(null);
      if (!row || !col || !canEdit(row, ed.c)) return;
      const value = normalizeCellInput(col, ed.text);
      const prev = values[row.key]?.[col.letter] ?? "";
      if (value !== prev) onChange?.([{ rowKey: row.key, letter: col.letter, value }]);
    },
    [rows, cols, canEdit, values, onChange],
  );

  const startEdit = useCallback(
    (r: number, c: number, mode: Editing["mode"], text?: string) => {
      const row = rows[r];
      const col = cols[c];
      if (!row || !col || !canEdit(row, c)) return false;
      setActive({ r, c });
      setEditing({ r, c, mode, text: text ?? editText(col, values[row.key]?.[col.letter]) });
      return true;
    },
    [rows, cols, canEdit, values],
  );

  const onWrapKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (editing) return; // ô sửa tự xử lý
    const a = active ?? { r: 0, c: 0 };
    const k = e.key;
    const move = (dr: number, dc: number) => {
      e.preventDefault();
      moveTo(a.r + dr, a.c + dc);
    };
    switch (k) {
      case "ArrowUp":
        return move(-1, 0);
      case "ArrowDown":
        return move(1, 0);
      case "ArrowLeft":
        return move(0, -1);
      case "ArrowRight":
        return move(0, 1);
      case "Tab":
        if (!active) return;
        return move(0, e.shiftKey ? -1 : 1);
      case "Home":
        return move(0, -a.c);
      case "End":
        return move(0, cols.length - 1 - a.c);
      case "Enter":
      case "F2":
        e.preventDefault();
        if (!active) return moveTo(0, 0);
        if (!startEdit(a.r, a.c, "edit") && k === "Enter") moveTo(a.r + 1, a.c);
        return;
      case "Delete":
      case "Backspace": {
        if (!active) return;
        const row = rows[a.r];
        const col = cols[a.c];
        if (row && col && canEdit(row, a.c) && (values[row.key]?.[col.letter] ?? "") !== "") {
          e.preventDefault();
          onChange?.([{ rowKey: row.key, letter: col.letter, value: "" }]);
        }
        return;
      }
    }
    if (k.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey && active) {
      if (startEdit(a.r, a.c, "replace", k)) e.preventDefault();
    }
  };

  const onEditorKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const ed = editing;
    if (!ed) return;
    const go = (dr: number, dc: number) => {
      e.preventDefault();
      commit(ed);
      moveTo(ed.r + dr, ed.c + dc);
      requestAnimationFrame(() => wrapRef.current?.focus());
    };
    switch (e.key) {
      case "Enter":
        return go(e.shiftKey ? -1 : 1, 0);
      case "Tab":
        return go(0, e.shiftKey ? -1 : 1);
      case "Escape":
        e.preventDefault();
        setEditing(null);
        requestAnimationFrame(() => wrapRef.current?.focus());
        return;
      case "ArrowUp":
      case "ArrowDown":
      case "ArrowLeft":
      case "ArrowRight":
        if (ed.mode === "replace") {
          const d: Record<string, [number, number]> = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] };
          const [dr, dc] = d[e.key]!;
          return go(dr, dc);
        }
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLDivElement>) => {
    const text = e.clipboardData?.getData("text/plain") ?? "";
    if (!text) return;
    const multi = /[\t\n]/.test(text.replace(/\r?\n$/, ""));
    if (editing && !multi) return; // dán một giá trị vào ô đang sửa: để ô xử lý
    if (!active || !onChange || readOnly) return;
    e.preventDefault();
    setEditing(null);
    const res = applyPaste(parseClipboard(text.replace(/\r?\n$/, "")), { row: active.r, col: active.c }, rows, cols, (row, col) => canEdit(row, cols.indexOf(col)));
    if (res.changes.length) onChange(res.changes);
    onPasteResult?.(res.changes.length, res.skipped);
  };

  const onCopy = (e: ClipboardEvent<HTMLDivElement>) => {
    if (editing || !active) return;
    const row = rows[active.r];
    const col = cols[active.c];
    if (!row || !col) return;
    const raw = values[row.key]?.[col.letter] ?? computed[row.key]?.[col.letter] ?? "";
    e.clipboardData?.setData("text/plain", editText(col, raw));
    e.preventDefault();
  };

  // Hàm gọi từ dòng đã memo: giữ tham chiếu ổn định, đọc trạng thái mới nhất qua ref (gõ một ô không vẽ lại cả lưới).
  const latest = useRef({ editing, commit, startEdit, onEditorKeyDown });
  useEffect(() => {
    latest.current = { editing, commit, startEdit, onEditorKeyDown };
  });
  const onCellMouseDown = useCallback((r: number, c: number, e: MouseEvent) => {
    if ((e.target as HTMLElement).tagName === "INPUT") return;
    const { editing: ed, commit: cm } = latest.current;
    if (ed) cm(ed);
    setActive({ r, c });
    wrapRef.current?.focus({ preventScroll: true });
  }, []);
  const onCellDoubleClick = useCallback((r: number, c: number) => void latest.current.startEdit(r, c, "edit"), []);
  const setEditText = useCallback((t: string) => setEditing((ed) => (ed ? { ...ed, text: t } : ed)), []);
  const commitEdit = useCallback(() => latest.current.commit(latest.current.editing), []);
  const onEditorKeyDownStable = useCallback((e: KeyboardEvent<HTMLInputElement>) => latest.current.onEditorKeyDown(e), []);

  const jumpToGroup = (id: string) => {
    const g = def.groups.find((x) => x.id === id);
    const wrap = wrapRef.current;
    if (!g || !wrap) return;
    const c = columnIndex(g.from) - 1;
    wrap.scrollLeft = Math.max(0, (lefts[c] ?? 0) - freeze.width);
  };

  const inSel = (row: number, c1: number, span: number, rspan: number) =>
    !!headerSelection && c1 <= headerSelection.c2 && c1 + span - 1 >= headerSelection.c1 && row <= headerSelection.r2 && row + rspan - 1 >= headerSelection.r1;

  const stickyTh = (c: number) => (c < freeze.count ? { left: freeze.lefts[c] } : undefined);
  const activeId = active ? `${idBase}-r${active.r}-c${active.c}` : undefined;

  return (
    <div className="fgrid">
      {def.groups.length && !hideGroupJump ? (
        <div className="fgrid-tools">
          <label className="fchip">
            <span>Đi tới</span>
            <select aria-label="Đi tới nhóm cột" defaultValue="" onChange={(e) => jumpToGroup(e.target.value)}>
              <option value="" disabled>
                Chọn nhóm cột
              </option>
              {def.groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.title} ({g.from}–{g.to})
                </option>
              ))}
            </select>
          </label>
        </div>
      ) : null}
      <div
        ref={wrapRef}
        className="fgrid-wrap"
        style={{ maxHeight }}
        tabIndex={0}
        role="group"
        aria-label={`${ariaLabel} — vùng cuộn`}
        aria-activedescendant={activeId}
        data-testid="form-grid"
        onKeyDown={onWrapKeyDown}
        onPaste={onPaste}
        onCopy={onCopy}
        onFocus={() => {
          if (rows.length) setActive((a) => a ?? { r: 0, c: Math.min(freeze.count, cols.length - 1) });
        }}
      >
        <table className={`fgrid-t${readOnly || !onChange ? " fgrid-ro" : ""}`} role="grid" aria-label={ariaLabel} aria-rowcount={rows.length} aria-colcount={cols.length} style={{ width: totalWidth }}>
          <colgroup>
            {widths.map((w, i) => (
              <col key={cols[i]!.letter} style={{ width: w }} />
            ))}
          </colgroup>
          <thead>
            <tr className="g-letters">
              {cols.map((col, c) => (
                <th
                  key={col.letter}
                  scope="col"
                  className={`${c < freeze.count ? "g-frz" : ""}${c === freeze.count - 1 ? " g-frz-last" : ""}${selCol === c + 1 ? " is-colsel" : ""}${active?.c === c ? " is-activecol" : ""}`}
                  style={stickyTh(c)}
                  onClick={onSelectColumn ? () => onSelectColumn(col.letter) : undefined}
                  aria-label={`Cột ${col.letter}`}
                >
                  {onSelectColumn ? (
                    <button type="button" className="g-letter-btn" aria-pressed={selCol === c + 1}>
                      {col.letter}
                    </button>
                  ) : (
                    col.letter
                  )}
                </th>
              ))}
            </tr>
            {header.map((hr, i) => (
              <tr key={`h${i}`} className="g-head">
                {hr.map((h) => (
                  <th
                    key={`${h.row}:${h.col}`}
                    scope="col"
                    colSpan={h.colSpan > 1 ? h.colSpan : undefined}
                    rowSpan={h.rowSpan > 1 ? h.rowSpan : undefined}
                    className={`${h.col - 1 < freeze.count && h.col + h.colSpan - 1 <= freeze.count ? "g-frz" : ""}${h.col + h.colSpan - 1 === freeze.count ? " g-frz-last" : ""}${inSel(h.row, h.col, h.colSpan, h.rowSpan) ? " is-hsel" : ""}${cols[h.col - 1]?.inactive ? " g-inactive" : ""}`}
                    style={h.col - 1 < freeze.count && h.col + h.colSpan - 1 <= freeze.count ? stickyTh(h.col - 1) : undefined}
                    data-cell={`${cols[h.col - 1]?.letter ?? ""}${h.row}`}
                    onClick={onHeaderCellClick ? (e) => onHeaderCellClick(h.row, h.col, e.shiftKey) : undefined}
                  >
                    {h.text}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {showKindRow ? (
              <tr className="g-kinds">
                {cols.map((col, c) => (
                  <td
                    key={col.letter}
                    className={`g-kind g-kind-${col.kind}${c < freeze.count ? " g-frz" : ""}${selCol === c + 1 ? " is-colsel" : ""}`}
                    style={c < freeze.count ? { left: freeze.lefts[c] } : undefined}
                    onClick={onSelectColumn ? () => onSelectColumn(col.letter) : undefined}
                    title={col.formula ? `= ${col.formula}` : undefined}
                  >
                    {KIND_SHORT[col.kind]}
                  </td>
                ))}
              </tr>
            ) : null}
            {rows.map((row, r) => (
              <RowView
                key={row.key}
                def={def}
                row={row}
                r={r}
                roles={roles}
                values={values[row.key]}
                computed={computed[row.key]}
                system={system?.[row.key] ?? (row.commune ? system?.[row.commune] : undefined)}
                communes={communes}
                lefts={freeze.lefts}
                activeC={active?.r === r ? active.c : null}
                editing={editing?.r === r ? editing : null}
                readOnly={readOnly || !onChange}
                issues={issueMap}
                idBase={idBase}
                selectedColumn={selCol}
                onCellMouseDown={onCellMouseDown}
                onCellDoubleClick={onCellDoubleClick}
                setEditText={setEditText}
                onEditorKeyDown={onEditorKeyDownStable}
                commitEdit={commitEdit}
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** Chú giải màu ô (Hình 14/15). */
export function GridLegend() {
  return (
    <ul className="g-legend" aria-label="Chú giải ô">
      <li>
        <i className="g-sw g-fixed" /> Cố định theo mẫu
      </li>
      <li>
        <i className="g-sw g-input" /> Ô nhập
      </li>
      <li>
        <i className="g-sw g-open" /> Cột của kỳ đang nhập
      </li>
      <li>
        <i className="g-sw g-closed" /> Kỳ khác (chỉ xem)
      </li>
      <li>
        <i className="g-sw g-formula" /> Công thức theo mẫu
      </li>
      <li>
        <i className="g-sw g-prov" /> Tỉnh giao
      </li>
    </ul>
  );
}
