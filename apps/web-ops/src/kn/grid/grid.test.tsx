// KN P3b — bộ vẽ lưới mẫu báo cáo (FN-XA-03.02, FN-AD-04.06): gộp tiêu đề, chỉ khe kỳ mở được nhập, dán khối
// từ Excel, bàn phím, dòng tổng, vấn đề tại ô, số vi-VN không float. "Máy chủ" trong test là bản port giả lập.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState as useStateLike } from "react";
import { describe, expect, it, vi } from "vitest";
import { evaluateGrid } from "@/api/mock/forms/evaluate";
import seeds from "@/api/mock/forms/fixtures/seed.json";
import { applyOps } from "@/api/mock/forms/ops";
import { validate } from "@/api/mock/forms/compile";
import type { FormDefinition, FormEvalInput } from "@/kn/forms/types";
import { moveColumnOps, moveIndicatorOps, periodSlotChange, shiftRefsText } from "@/kn/forms/designer";
import { formatDecimalVi, normalizeNumberInput, normalizeDateInput, Decimal } from "./decimal";
import { FormEntry } from "./FormEntry";
import { FormGrid } from "./FormGrid";
import { allCommuneRows, applyPaste, buildEvalInput, buildHeader, cellRole, communeRows, computedCells, displayCell, parseClipboard, type GridCommune, type GridValues } from "./model";

const SEEDS = seeds as unknown as { code: string; definition: FormDefinition }[];
const def = (code: string) => structuredClone(SEEDS.find((s) => s.code === code)!.definition);
const COMMUNES: GridCommune[] = [
  { code: "91301", name: "Mỹ Hòa Hưng", region: "KV883", regionName: "Long Xuyên" },
  { code: "91302", name: "Bình Đức", region: "KV883", regionName: "Long Xuyên" },
  { code: "91310", name: "Vĩnh Tế", region: "KV884", regionName: "Châu Đốc" },
];
const server = (d: FormDefinition) => vi.fn(async (input: FormEvalInput) => evaluateGrid(d, input));

function cell(grid: HTMLElement, rowIdx: number, letter: string): HTMLElement {
  const tr = within(grid).getAllByRole("row").filter((r) => r.getAttribute("aria-rowindex") === String(rowIdx + 1))[0]!;
  return tr.querySelector<HTMLElement>(`[data-letter="${letter}"]`)!;
}

describe("KN P3b số decimal & định dạng vi-VN (không float)", () => {
  it("chuẩn hóa nhập/dán kiểu Việt Nam và hiển thị phân cách nghìn", () => {
    expect(normalizeNumberInput("38,5")).toBe("38.5");
    expect(normalizeNumberInput("1.234,50")).toBe("1234.5");
    expect(normalizeNumberInput(" 12 345,6 ")).toBe("12345.6");
    expect(normalizeNumberInput("0.1")).toBe("0.1");
    expect(normalizeNumberInput("abc")).toBe("abc");
    expect(normalizeDateInput("1/9/2026")).toBe("2026-09-01");
    expect(formatDecimalVi("1234567.891", { maxDecimals: 2 })).toBe("1.234.567,89");
    expect(formatDecimalVi("-0.005", { maxDecimals: 2 })).toBe("-0,01");
    // 0,1 + 0,2 = 0,3 đúng tuyệt đối (float sẽ ra 0,30000000000000004)
    expect(Decimal.parse("0.1")!.add(Decimal.parse("0.2")!).toString()).toBe("0.3");
    const pct = def("T04").columns.find((c) => c.letter === "F")!;
    expect(displayCell(pct, "55.5555555556", true)).toBe("55,56%");
  });
});

describe("KN P3b khung tiêu đề gộp nhiều tầng", () => {
  it("T01: A–E, M–O gộp dọc 2 dòng; F1:L1 gộp ngang; dòng 2 chỉ có 7 ô tuần", () => {
    const h = buildHeader(def("T01"));
    expect(h).toHaveLength(2);
    const a = h[0]!.find((c) => c.col === 1)!;
    expect(a).toMatchObject({ rowSpan: 2, colSpan: 1, text: "STT" });
    const f = h[0]!.find((c) => c.col === 6)!;
    expect(f.colSpan).toBe(7);
    expect(f.text).toContain("DIỆN TÍCH XUỐNG GIỐNG LÚA");
    expect(h[1]!.map((c) => c.text)).toEqual(["Tuần 1", "Tuần 2", "Tuần 3", "Tuần 4", "Tuần 5", "Tuần 6", "Tuần 7"]);
  });

  it("T04: 3 tầng, nhóm cây gộp 24 cột, cây gộp 4 cột, tới HR", () => {
    const d = def("T04");
    const h = buildHeader(d);
    expect(h).toHaveLength(3);
    expect(h[0]!.find((c) => c.col === 11)).toMatchObject({ colSpan: 24 });
    expect(h[1]!.find((c) => c.col === 11)).toMatchObject({ colSpan: 4, text: "Rau muống" });
    expect(h[2]!.filter((c) => c.col >= 11)).toHaveLength(216);
    render(<FormGrid definition={d} rows={communeRows(d, COMMUNES[0]!)} ariaLabel="T04" />);
    const grid = screen.getByRole("grid", { name: "T04" });
    expect(within(grid).getByRole("columnheader", { name: "Cột HR" })).toBeInTheDocument();
    // A–J cố định (sticky left), K không.
    expect(within(grid).getByRole("columnheader", { name: "Cột J" })).toHaveClass("g-frz", "g-frz-last");
    expect(within(grid).getByRole("columnheader", { name: "Cột K" })).not.toHaveClass("g-frz");
  });
});

describe("KN P3b vai trò ô và nhập liệu", () => {
  it("chỉ khe kỳ đang mở nhập được; cố định, Tỉnh giao, công thức chỉ xem", () => {
    const d = def("T01");
    const col = (l: string) => d.columns.find((c) => c.letter === l)!;
    expect(cellRole(col("H"), [3])).toBe("open");
    expect(cellRole(col("F"), [3])).toBe("closed");
    expect(cellRole(col("F"), null)).toBe("open");
    expect(cellRole(col("D"), [3])).toBe("provincial");
    expect(cellRole(col("M"), [3])).toBe("formula");
    const onChange = vi.fn();
    render(<FormGrid definition={d} rows={communeRows(d, COMMUNES[0]!)} communes={COMMUNES} openSlots={[3]} onChange={onChange} ariaLabel="T01" />);
    const grid = screen.getByRole("grid", { name: "T01" });
    expect(cell(grid, 0, "H")).not.toHaveAttribute("aria-readonly");
    expect(cell(grid, 0, "F")).toHaveAttribute("aria-readonly", "true");
    expect(cell(grid, 0, "M")).toHaveAttribute("aria-readonly", "true");
    expect(cell(grid, 0, "C")).toHaveTextContent("Mỹ Hòa Hưng");
    // Nhấp đúp ô kỳ đóng: không mở ô sửa.
    fireEvent.doubleClick(cell(grid, 0, "F"));
    expect(within(grid).queryByRole("textbox")).toBeNull();
    // Ô kỳ mở: gõ "38,5" + Enter → giá trị decimal "38.5".
    fireEvent.doubleClick(cell(grid, 0, "H"));
    const box = within(grid).getByRole("textbox", { name: "Ô H" });
    fireEvent.change(box, { target: { value: "38,5" } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onChange).toHaveBeenCalledWith([{ rowKey: "91301", letter: "H", value: "38.5" }]);
  });

  it("bàn phím: mũi tên, Tab, gõ để thay, Esc hủy, Delete xóa", () => {
    const d = def("T01");
    const values: GridValues = { "91301": { E: "126" } };
    const onChange = vi.fn();
    render(<FormGrid definition={d} rows={communeRows(d, COMMUNES[0]!)} communes={COMMUNES} values={values} openSlots={[3]} onChange={onChange} ariaLabel="T01" />);
    const wrap = screen.getByTestId("form-grid");
    fireEvent.mouseDown(cell(screen.getByRole("grid"), 0, "E"));
    expect(wrap.getAttribute("aria-activedescendant")).toMatch(/-r0-c4$/);
    fireEvent.keyDown(wrap, { key: "ArrowRight" });
    expect(wrap.getAttribute("aria-activedescendant")).toMatch(/-r0-c5$/);
    fireEvent.keyDown(wrap, { key: "Tab" });
    fireEvent.keyDown(wrap, { key: "Tab" });
    expect(wrap.getAttribute("aria-activedescendant")).toMatch(/-r0-c7$/); // H
    fireEvent.keyDown(wrap, { key: "5" });
    const box = within(wrap).getByRole("textbox");
    expect(box).toHaveValue("5");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(within(wrap).queryByRole("textbox")).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.keyDown(wrap, { key: "ArrowLeft" });
    fireEvent.keyDown(wrap, { key: "ArrowLeft" });
    fireEvent.keyDown(wrap, { key: "ArrowLeft" });
    expect(wrap.getAttribute("aria-activedescendant")).toMatch(/-r0-c4$/); // E
    fireEvent.keyDown(wrap, { key: "Delete" });
    expect(onChange).toHaveBeenCalledWith([{ rowKey: "91301", letter: "E", value: "" }]);
  });

  it("dán khối từ Excel: ô không nhập được bị bỏ qua và đếm", () => {
    expect(parseClipboard('1,5\t2\r\n"a\tb"\t4\r\n')).toEqual([["1,5", "2"], ["a\tb", "4"]]);
    const d = def("T01");
    const rows = allCommuneRows(d, COMMUNES).filter((r) => r.kind === "data");
    const editable = (_r: unknown, c: (typeof d.columns)[number]) => c.kind === "input" || (c.kind === "per_period" && c.period_slot === 1);
    const res = applyPaste(parseClipboard("10\t1.234,5\t7\n20\t3\t8"), { row: 0, col: 4 }, rows, d.columns, editable);
    expect(res.changes).toEqual([
      { rowKey: "91301", letter: "E", value: "10" },
      { rowKey: "91301", letter: "F", value: "1234.5" },
      { rowKey: "91302", letter: "E", value: "20" },
      { rowKey: "91302", letter: "F", value: "3" },
    ]);
    expect(res.skipped).toBe(2); // G (kỳ 2 đóng) ở hai dòng
    const onChange = vi.fn();
    const onPasteResult = vi.fn();
    render(<FormGrid definition={d} rows={rows} communes={COMMUNES} openSlots={[1]} onChange={onChange} onPasteResult={onPasteResult} ariaLabel="T01" />);
    const wrap = screen.getByTestId("form-grid");
    fireEvent.mouseDown(cell(screen.getByRole("grid"), 0, "E"));
    fireEvent.paste(wrap, { clipboardData: { getData: () => "10\t1.234,5\t7\n20\t3\t8\n" } });
    expect(onChange).toHaveBeenCalledWith(res.changes);
    expect(onPasteResult).toHaveBeenCalledWith(4, 2);
  });
});

describe("KN P3b dòng tổng và vấn đề tại ô (tính trên máy chủ)", () => {
  it("bảng tất cả các xã: Cộng khu vực theo tên, Toàn tỉnh; ô lỗi đánh dấu", () => {
    const d = def("T01");
    const values: GridValues = { "91301": { F: "28", G: "38", H: "38.5" }, "91302": { H: "12.125" }, "91310": { H: "abc" } };
    const input = buildEvalInput(d, COMMUNES, values, { mode: "draft", openSlots: null });
    expect(input.rows[0]!.system).toMatchObject({ B: "Long Xuyên", C: "Mỹ Hòa Hưng" });
    const r = evaluateGrid(d, input);
    const computed = computedCells(r.rows, r.totals, d, COMMUNES);
    const rows = allCommuneRows(d, COMMUNES);
    expect(rows.map((x) => x.key)).toEqual(["91301", "91302", "region:KV883", "91310", "region:KV884", "province"]);
    render(<FormGrid definition={d} rows={rows} values={values} computed={computed} communes={COMMUNES} issues={r.issues} readOnly ariaLabel="T01 tất cả" />);
    const grid = screen.getByRole("grid");
    expect(cell(grid, 0, "M")).toHaveTextContent("104,5");
    expect(cell(grid, 2, "B")).toHaveTextContent("Cộng Long Xuyên");
    expect(cell(grid, 2, "M")).toHaveTextContent("116,63"); // 104,5 + 12,125 → 2 chữ số
    expect(cell(grid, 5, "B")).toHaveTextContent("Toàn tỉnh");
    const bad = cell(grid, 3, "H");
    expect(bad).toHaveAttribute("aria-invalid", "true");
    expect(bad).toHaveAttribute("title", "phải là số (dùng dấu chấm thập phân)");
  });

  it("FormEntry: mỗi xã một tab, tính lại trên máy chủ sau khi gõ (giãn cách), tab tất cả chỉ xem", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const d = def("T01");
    const evaluate = server(d);
    function Host() {
      const [v, setV] = useStateLike<GridValues>({ "91301": { F: "28", G: "38" } });
      return <FormEntry definition={d} versionKey="v1" communes={COMMUNES} values={v} onValuesChange={(n) => setV(n)} evaluate={evaluate} openSlots={[3]} debounceMs={300} />;
    }
    render(<Host />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10);
    });
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(expect.arrayContaining([expect.stringContaining("Mỹ Hòa Hưng"), expect.stringContaining("Tất cả các xã")]));
    const grid = screen.getByRole("grid");
    await waitFor(() => expect(cell(grid, 0, "M")).toHaveTextContent("66"));
    fireEvent.doubleClick(cell(grid, 0, "H"));
    const box = within(grid).getByRole("textbox");
    fireEvent.change(box, { target: { value: "38,5" } });
    fireEvent.keyDown(box, { key: "Enter" });
    const calls = evaluate.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(evaluate.mock.calls.length).toBe(calls); // chưa hết giãn cách
    await act(async () => {
      await vi.advanceTimersByTimeAsync(400);
    });
    await waitFor(() => expect(cell(screen.getByRole("grid"), 0, "M")).toHaveTextContent("104,5"));
    fireEvent.click(screen.getByRole("tab", { name: /Tất cả các xã/ }));
    const all = screen.getByRole("grid");
    expect(cell(all, 0, "H")).toHaveAttribute("aria-readonly", "true");
    expect(within(all).getAllByRole("row").some((r) => r.textContent?.includes("Toàn tỉnh"))).toBe(true);
    vi.useRealTimers();
  });
});

describe("KN P3b soạn lô thao tác của trình thiết kế", () => {
  it("tăng số kỳ T01 7 → 8 hợp lệ trên engine; lũy kế SUM nở tới cột mới; tiêu đề khe mới", () => {
    const d = def("T01");
    const ch = periodSlotChange(d, "xg", 8, "Tuần {n}");
    const out = applyOps(d, ch.ops);
    expect(out.columns.find((c) => c.letter === "N")?.formula).toBe("SUM(F#:M#)");
    expect(out.columns.find((c) => c.letter === "M")).toMatchObject({ kind: "per_period", period_slot: 8, title: "Tuần 8" });
    const fixed = ch.fixHeader!(out)!;
    validate(fixed);
    expect(buildHeader(fixed)[1]!.map((c) => c.text)).toEqual(["Tuần 1", "Tuần 2", "Tuần 3", "Tuần 4", "Tuần 5", "Tuần 6", "Tuần 7", "Tuần 8"]);
  });

  it("chuyển cột: T04 F ↔ E đổi chỗ, công thức đổi tham chiếu; cột theo kỳ, biên dải bị từ chối", () => {
    const d = def("T04");
    const m = moveColumnOps(d, "F", -1);
    if (!("ops" in m)) throw new Error(m.error);
    const out = applyOps(d, m.ops);
    expect(out.columns[4]).toMatchObject({ key: "pct_khxg", formula: "IF(D#=0,0,F#/D#*100)" });
    expect(out.columns[5]?.key).toBe("dt_xg");
    expect(out.columns.find((c) => c.key === "pct_th")?.formula).toBe("IF(F#=0,0,G#/F#*100)");
    expect(buildHeader(out)[0]!.find((h) => h.col === 5)?.text).toBe("% KHXG");
    expect("error" in moveColumnOps(def("T01"), "H", 1)).toBe(true); // khối kỳ
    expect("error" in moveColumnOps(def("T01"), "E", 1)).toBe(true); // F thuộc khối kỳ
    expect("error" in moveColumnOps(def("V06"), "H", 1)).toBe(true); // I = SUM(D#:H#): H là đầu mút dải
    expect("ops" in moveColumnOps(def("V06"), "E", 1)).toBe(true); // E ↔ F nằm trong dải: dải giữ nguyên
  });

  it("chuyển dòng chỉ tiêu lên/xuống", () => {
    const d = def("T05");
    const out = applyOps(d, moveIndicatorOps(d, "SB02", -1));
    expect(out.rows.indicators.slice(0, 3).map((i) => i.code)).toEqual(["SB02", "SB01", "SB03"]);
    expect(moveIndicatorOps(d, "SB01", -1)).toEqual([]);
  });

  it("giảm số kỳ T01 7 → 6: dải lũy kế co lại; dời tham chiếu khi chèn", () => {
    const d = def("T01");
    const out = applyOps(d, periodSlotChange(d, "xg", 6, "Tuần {n}").ops);
    expect(out.columns.find((c) => c.letter === "L")?.formula).toBe("SUM(F#:K#)");
    expect(shiftRefsText("IF(D#=0,0,AA#/D#*100)", 20, 4)).toBe("IF(D#=0,0,AE#/D#*100)");
  });
});

// BF-TT-FORMS (#107, UP-04): khung "Yêu cầu biểu mẫu" luôn hiện; nhiều dòng yêu cầu → mỗi dòng một gạch đầu dòng.
describe("FormEntry — khung Yêu cầu biểu mẫu (UP-04)", () => {
  it("có yêu cầu → hiện từng dòng; chưa có → nói rõ mẫu chưa ghi yêu cầu", () => {
    const d = def("T01");
    d.requirements = "Nhập theo tuần\n- Không nhập số âm";
    const { unmount } = render(<FormEntry definition={d} versionKey="v1" communes={COMMUNES} values={{}} evaluate={server(d)} openSlots={[3]} readOnly />);
    const box = screen.getByRole("note", { name: "Yêu cầu biểu mẫu" });
    expect(within(box).getByText("- Nhập theo tuần")).toBeInTheDocument();
    expect(within(box).getByText("- Không nhập số âm")).toBeInTheDocument();
    unmount();
    const e = def("T01");
    delete (e as { requirements?: string }).requirements;
    render(<FormEntry definition={e} versionKey="v1" communes={COMMUNES} values={{}} evaluate={server(e)} openSlots={[3]} readOnly />);
    expect(within(screen.getByRole("note", { name: "Yêu cầu biểu mẫu" })).getByText("- Mẫu chưa ghi yêu cầu riêng.")).toBeInTheDocument();
  });
});
