"use client";

// Tính lưới trên MÁY CHỦ (POST /v1/reports/form-versions/{id}/evaluate) có giãn cách (debounce): công thức,
// dòng tổng và vấn đề tại ô luôn trùng máy chủ — web không tự tính công thức (tránh lệch). Kết quả cũ (về
// muộn hơn lần gọi sau) bị bỏ qua.
import { useEffect, useMemo, useRef, useState } from "react";
import type { ApiProblem } from "@/api/problem";
import type { FormDefinition, FormEvalInput, FormEvalResult } from "@/kn/forms/types";
import { buildEvalInput, computedCells, type EvalInputOptions, type GridCommune, type GridValues } from "./model";

export type FormEvaluator = (input: FormEvalInput) => Promise<FormEvalResult>;

export interface FormEvaluationState {
  result: FormEvalResult | null;
  /** Ô hiển thị theo dòng (dữ liệu + dòng tổng). */
  computed: GridValues;
  pending: boolean;
  problem: ApiProblem | null;
}

export interface UseFormEvaluationOptions extends Partial<EvalInputOptions> {
  definition: FormDefinition | null;
  communes: readonly GridCommune[];
  values: GridValues;
  evaluate: FormEvaluator | null;
  /** ms, mặc định 400. */
  debounceMs?: number;
  /** Khóa đổi khi đổi phiên bản mẫu (gọi lại ngay). */
  versionKey?: string;
}

export function useFormEvaluation(o: UseFormEvaluationOptions): FormEvaluationState {
  const [state, setState] = useState<{ result: FormEvalResult | null; pending: boolean; problem: ApiProblem | null }>({ result: null, pending: false, problem: null });
  const seq = useRef(0);
  const input = useMemo(
    () =>
      o.definition
        ? buildEvalInput(o.definition, o.communes, o.values, { mode: o.mode ?? "draft", openSlots: o.openSlots ?? null, system: o.system, xrefs: o.xrefs })
        : null,
    [o.definition, o.communes, o.values, o.mode, o.openSlots, o.system, o.xrefs],
  );
  const key = useMemo(() => (input ? JSON.stringify(input) : ""), [input]);
  const firstFor = useRef<string | undefined>(undefined);
  const evaluate = o.evaluate;
  const delay = o.debounceMs ?? 400;
  useEffect(() => {
    if (!input || !evaluate) return;
    const my = ++seq.current;
    // Lần đầu của một phiên bản: gọi ngay; sau đó giãn cách theo gõ phím.
    const immediate = firstFor.current !== o.versionKey;
    firstFor.current = o.versionKey;
    const t = setTimeout(
      () => {
        setState((s) => ({ ...s, pending: true }));
        evaluate(input).then(
          (result) => {
            if (my === seq.current) setState({ result, pending: false, problem: null });
          },
          (e: unknown) => {
            if (my === seq.current) setState((s) => ({ ...s, pending: false, problem: e as ApiProblem }));
          },
        );
      },
      immediate ? 0 : delay,
    );
    return () => clearTimeout(t);
    // key thay cho input (so theo nội dung, không theo tham chiếu)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, evaluate, delay, o.versionKey]);
  const computed = useMemo(
    () => (state.result && o.definition ? computedCells(state.result.rows, state.result.totals, o.definition, o.communes) : {}),
    [state.result, o.definition, o.communes],
  );
  return { ...state, computed };
}
