"use client";

// Báo cáo sản lượng HTX (HTX US-DASH-02): tạo việc xuất mẫu `htx.production` qua pf/reports
// (POST /v1/reports/exports → 202 + việc xuất nền → hỏi trạng thái → URL tải có hạn). Bộ lọc
// gửi đi khớp đúng bộ lọc đang xem trên bảng điều hành (AC-1).
import { useCallback, useState } from "react";
import type { ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { ReportExport, ReportParams } from "@/api/types";
import { toApiProblem } from "@/lib/useAsync";
import { useInterval } from "@/lib/useInterval";
import { EXPORT_POLL_MS, getExport, requestExport } from "@/reports/exports";

/** Mã mẫu hệ thống báo cáo sản lượng HTX (nguồn cropcycle.production). */
export const HTX_REPORT_KEY = "htx.production";

export interface ProductionFilter {
  coopRef?: string;
  seasonCode?: string;
  year?: string;
  variety?: string;
}

/** Bộ lọc của việc xuất từ bộ lọc bảng điều hành: trường trống không gửi (= tất cả). */
export function productionReportParams(f: ProductionFilter): ReportParams {
  const p: ReportParams = {};
  if (f.coopRef) p.cooperative_ref = f.coopRef;
  if (f.seasonCode) p.season_code = f.seasonCode;
  const y = (f.year ?? "").trim();
  if (y && /^\d+$/.test(y)) p.season_year = Number(y);
  if (f.variety) p.variety_code = f.variety;
  return p;
}

export interface ProductionExport {
  job: ReportExport | null;
  busy: boolean;
  problem: ApiProblem | null;
  start: (format: "xlsx" | "pdf", filter: ProductionFilter) => Promise<void>;
  reset: () => void;
}

/** Việc xuất báo cáo sản lượng của trang: tạo việc, tự hỏi trạng thái mỗi 1,5 giây tới khi xong/hỏng. */
export function useProductionExport(api: ApiClient): ProductionExport {
  const [job, setJob] = useState<ReportExport | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const start = useCallback(
    async (format: "xlsx" | "pdf", filter: ProductionFilter) => {
      setBusy(true);
      setProblem(null);
      setJob(null);
      try {
        setJob(await requestExport(api, { template_key: HTX_REPORT_KEY, format, params: productionReportParams(filter) }, newIdempotencyKey()));
      } catch (e) {
        setProblem(toApiProblem(e));
      } finally {
        setBusy(false);
      }
    },
    [api],
  );

  const queued = job?.status === "queued" ? job.id : null;
  useInterval(
    () => {
      if (!queued) return;
      getExport(api, queued).then(
        (j) => setJob((cur) => (cur?.id === j.id ? j : cur)),
        (e: unknown) => setProblem(toApiProblem(e)),
      );
    },
    queued ? EXPORT_POLL_MS : null,
  );

  const reset = useCallback(() => {
    setJob(null);
    setProblem(null);
  }, []);

  return { job, busy, problem, start, reset };
}
