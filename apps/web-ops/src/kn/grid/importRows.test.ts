import { describe, expect, it } from "vitest";
import type { FormDefinition } from "@/kn/forms/types";
import { mergeGridImport, normName } from "./importRows";

const col = (letter: string, kind: string, extra: Record<string, unknown> = {}) => ({ letter, key: letter.toLowerCase(), title: letter, kind, dtype: "number", ...extra });
const base = {
  schema: "form-template.v1",
  code: "T01",
  title: "T01",
  sheet_name: "PL 01",
  totals: [],
  header: { rows: 1, cells: [], merges: [] },
  period_blocks: [],
  groups: [],
  rules: [],
  pending: [],
} as unknown as FormDefinition;
const COMMUNES = [
  { code: "91301", name: "Mỹ Hòa Hưng", region: "LX" },
  { code: "91302", name: "Bình Đức", region: "LX" },
];

describe("Nhập từ Excel — ghép dòng tệp vào lưới (UP-04)", () => {
  it("ghép theo tên xã (không dấu, có tiền tố 'Xã'), chỉ điền ô nhập được, chuẩn hóa số kiểu VN", () => {
    const def = { ...base, rows: { kind: "per_commune", indicators: [] }, columns: [col("A", "fixed", { fixed_source: "row_no" }), col("C", "fixed", { fixed_source: "commune_name", dtype: "text" }), col("D", "provincial"), col("E", "input"), col("F", "formula")] } as unknown as FormDefinition;
    const r = mergeGridImport(
      def,
      COMMUNES,
      { "91302": { E: "9" } },
      [
        { line: 5, cells: { A: "1", C: "Xã My Hoa Hung", D: "100", E: "1.234,5", F: "999" } },
        { line: 6, cells: { C: "Bình Đức", E: "3" } },
        { line: 7, cells: { C: "Xã Lạ", E: "1" } },
        { line: 8, cells: { E: "1" } },
        { line: 9, cells: { C: "Bình Đức", D: "5" } },
      ],
      (l) => l === "E",
    );
    expect(r.values).toEqual({ "91301": { E: "1234.5" }, "91302": { E: "3" } });
    expect(r.applied).toBe(2);
    expect(r.skipped).toEqual([
      { line: 7, reason: "xã không thuộc công việc này" },
      { line: 8, reason: "không có tên xã" },
      { line: 9, reason: "không có ô nhập được" },
    ]);
  });

  it("biểu có chỉ tiêu: ghép theo nhãn chỉ tiêu; tên xã gộp dọc dùng cho các dòng dưới", () => {
    const def = {
      ...base,
      rows: { kind: "per_commune", indicators: [{ code: "ray_nau", label: "Rầy nâu" }, { code: "dao_on", label: "Bệnh đạo ôn" }] },
      columns: [col("C", "fixed", { fixed_source: "commune_name", dtype: "text" }), col("D", "fixed", { fixed_source: "indicator_label", dtype: "text" }), col("E", "input")],
    } as unknown as FormDefinition;
    const r = mergeGridImport(def, COMMUNES, {}, [
      { line: 9, cells: { C: "Mỹ Hòa Hưng", D: "Rầy nâu", E: "10" } },
      { line: 10, cells: { D: "Bệnh đạo ôn", E: "2" } },
      { line: 11, cells: { D: "Sâu lạ", E: "2" } },
    ], () => true);
    expect(r.values).toEqual({ "91301/ray_nau": { E: "10" }, "91301/dao_on": { E: "2" } });
    expect(r.skipped).toEqual([{ line: 11, reason: "chỉ tiêu không có trong biểu" }]);
  });

  it("normName bỏ dấu và tiền tố đơn vị", () => {
    expect(normName("Phường Long Xuyên")).toBe("long xuyen");
    expect(normName("Đặc khu Phú Quốc")).toBe("phu quoc");
  });
});
