// Bản port TS của bộ tính mẫu báo cáo (chỉ cho backend giả lập) phải trùng engine Go: ca vàng do
// internal/platform/reports/form/webfixture_test.go sinh (AG_UPDATE_WEB_FIXTURES=1).
import { describe, expect, it } from "vitest";
import type { FormDefinition, FormEvalInput, FormEvalResult, FormOp, FormTotalRow } from "@/kn/forms/types";
import { Decimal } from "@/kn/grid/decimal";
import { ValidationError } from "./compile";
import { evaluateGrid } from "./evaluate";
import evalGolden from "./fixtures/eval-golden.json";
import opsGolden from "./fixtures/ops-golden.json";
import seeds from "./fixtures/seed.json";
import { parse, format, evaluate, BLANK } from "./formula";
import { layout } from "./layout";
import { applyOps, OpError } from "./ops";

interface Seed {
  code: string;
  definition: FormDefinition;
}
const SEEDS = seeds as unknown as Seed[];
const def = (code: string) => SEEDS.find((s) => s.code === code)!.definition;

/** Bỏ trường rỗng như omitempty của Go (null, "", false, 0, []) để so hai phía. */
function norm(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(norm);
  if (v && typeof v === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v)) {
      if (x === null || x === undefined || x === "" || x === false || x === 0 || (Array.isArray(x) && x.length === 0)) continue;
      out[k] = norm(x);
    }
    return out;
  }
  return v;
}

describe("KN P3b bộ tính giả lập = engine Go (FN-AD-04.06)", () => {
  it("14 mẫu khởi tạo, T04 rộng tới cột HR, T01 tới O", () => {
    expect(SEEDS.map((s) => s.code)).toEqual(["T01", "T02", "T03", "T04", "T05", "T06", "T07", "M01", "V01", "V02", "V03", "V04", "V05", "V06"]);
    expect(def("T04").columns.at(-1)?.letter).toBe("HR");
    expect(def("T01").columns.at(-1)?.letter).toBe("O");
  });

  for (const c of evalGolden as unknown as { name: string; code: string; input: FormEvalInput; result: FormEvalResult; totals: FormTotalRow[] }[]) {
    it(`tính lưới: ${c.name}`, () => {
      const got = evaluateGrid(def(c.code), c.input);
      expect(norm({ valid: got.valid, rows: got.rows, issues: got.issues })).toEqual(norm(c.result));
      expect(norm(got.totals)).toEqual(norm(c.totals));
    });
  }

  for (const c of opsGolden as unknown as { name: string; code: string; ops: FormOp[]; error_code?: string; error_at?: number; result?: FormDefinition }[]) {
    it(`thao tác: ${c.name}`, () => {
      if (c.error_code) {
        let code = "";
        let at: number | undefined;
        try {
          applyOps(def(c.code), c.ops);
        } catch (e) {
          if (e instanceof OpError) {
            code = e.code;
            at = e.index;
          } else if (e instanceof ValidationError) code = e.code;
          else throw e;
        }
        expect(code).toBe(c.error_code);
        expect(at).toBe(c.error_at);
        return;
      }
      expect(norm(applyOps(def(c.code), c.ops))).toEqual(norm(c.result));
    });
  }

  it("định dạng công thức như máy chủ; chia giữ 20 chữ số rồi làm tròn kết quả 10 chữ số", () => {
    expect(format(parse("=sum( F# : L# )+ 0.50"))).toBe("SUM(F#:L#)+0.5");
    expect(format(parse("A#-(B#-C#)"))).toBe("A#-(B#-C#)");
    expect(format(parse("-2^2"))).toBe("-2^2");
    const v = evaluate(parse("1/3"), { cell: () => BLANK, xref: () => BLANK });
    expect(v.k === "num" && v.n.toString()).toBe("0.33333333333333333333");
    expect(Decimal.parse("2.5")!.round(0).toString()).toBe("3");
    expect(Decimal.parse("-2.5")!.round(0).toString()).toBe("-3");
  });

  it("layout: nhãn kỳ theo đợt, khe vượt số kỳ inactive, {season}", () => {
    const d = layout(def("T01"), { periodLabels: ["Tuần 36", "Tuần 37"], season: "THU ĐÔNG 2026" });
    expect(d.columns.find((c) => c.letter === "F")?.title).toBe("Tuần 36");
    expect(d.columns.find((c) => c.letter === "H")?.inactive).toBe(true);
    expect(d.title).toBe("TIẾN ĐỘ XUỐNG GIỐNG LÚA THU ĐÔNG 2026");
  });
});
