"use client";

// pf/reports — xuất báo cáo bất đồng bộ (R-52): POST /v1/reports/exports → 202 + việc xuất → hỏi trạng thái
// GET /v1/reports/exports/{id} → khi `completed` lấy URL tải có hạn GET /v1/reports/exports/{id}/download.
// Hợp đồng không có route liệt kê việc xuất của người dùng, nên "lịch sử" là các việc xuất trong phiên
// làm việc này: chỉ giữ MÃ việc xuất trong sessionStorage (không PII), trạng thái luôn đọc lại từ máy chủ.
import { useCallback, useEffect, useRef, useState } from "react";
import type { ApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { ReportDownload, ReportExport, ReportParams } from "@/api/types";
import { triggerDownload } from "@/lib/files";
import { useInterval } from "@/lib/useInterval";

export const EXPORT_POLL_MS = 1500;
const HISTORY_KEY = "ag.reports.history";
const HISTORY_MAX = 30;

export function requestExport(
  api: ApiClient,
  input: { template_key: string; format: "xlsx" | "pdf"; params: ReportParams },
  idemKey: string,
): Promise<ReportExport> {
  return unwrap(api.POST("/v1/reports/exports", { params: { header: { "Idempotency-Key": idemKey } }, body: input }));
}

export function getExport(api: ApiClient, id: string): Promise<ReportExport> {
  return unwrap(api.GET("/v1/reports/exports/{id}", { params: { path: { id } } }));
}

export async function downloadExport(api: ApiClient, id: string): Promise<ReportDownload> {
  const d = await unwrap(api.GET("/v1/reports/exports/{id}/download", { params: { path: { id } } }));
  triggerDownload(d.url, d.file_name);
  return d;
}

/**
 * Hỏi trạng thái tới khi xong/hỏng hoặc hết `timeoutMs`. Dùng trong kiểm thử và nơi cần chờ một việc;
 * giao diện dùng `useExportJobs` (hỏi định kỳ nhiều việc).
 */
export async function waitExport(api: ApiClient, id: string, opts: { intervalMs?: number; timeoutMs?: number; sleep?: (ms: number) => Promise<void> } = {}): Promise<ReportExport> {
  const interval = opts.intervalMs ?? EXPORT_POLL_MS;
  const deadline = Date.now() + (opts.timeoutMs ?? 120_000);
  const sleep = opts.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  for (;;) {
    const e = await getExport(api, id);
    if (e.status !== "queued" || Date.now() >= deadline) return e;
    await sleep(interval);
  }
}

function readHistory(): string[] {
  try {
    const raw = window.sessionStorage.getItem(HISTORY_KEY);
    const v: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string").slice(0, HISTORY_MAX) : [];
  } catch {
    return [];
  }
}

function writeHistory(ids: string[]): void {
  try {
    window.sessionStorage.setItem(HISTORY_KEY, JSON.stringify(ids.slice(0, HISTORY_MAX)));
  } catch {
    // bỏ qua: chỉ mất lịch sử khi tải lại trang
  }
}

export interface ExportJobs {
  jobs: ReportExport[];
  add: (e: ReportExport) => void;
  refresh: () => void;
}

/** Danh sách việc xuất của phiên + tự hỏi lại việc đang `queued` mỗi 1,5 giây. */
export function useExportJobs(api: ApiClient): ExportJobs {
  const [jobs, setJobs] = useState<ReportExport[]>([]);
  const loaded = useRef(false);

  const refreshIds = useCallback(
    (ids: string[]) =>
      Promise.allSettled(ids.map((id) => getExport(api, id))).then((settled) => {
        const fresh = new Map<string, ReportExport>();
        settled.forEach((r) => {
          if (r.status === "fulfilled") fresh.set(r.value.id, r.value);
        });
        setJobs((cur) => {
          const known = new Map(cur.map((j) => [j.id, j]));
          for (const [id, j] of fresh) known.set(id, j);
          return Array.from(known.values()).sort((a, b) => Date.parse(b.requested_at) - Date.parse(a.requested_at));
        });
      }),
    [api],
  );

  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    const ids = readHistory();
    if (ids.length === 0) return;
    // Nạp lại trạng thái các việc xuất của phiên (setState chỉ trong callback của promise).
    Promise.allSettled(ids.map((id) => getExport(api, id))).then((settled) => {
      const list = settled.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
      setJobs((cur) => (cur.length ? cur : list.sort((a, b) => Date.parse(b.requested_at) - Date.parse(a.requested_at))));
    });
  }, [api]);

  const pending = jobs.filter((j) => j.status === "queued").map((j) => j.id);
  useInterval(
    () => {
      if (pending.length) void refreshIds(pending);
    },
    pending.length ? EXPORT_POLL_MS : null,
  );

  const add = useCallback((e: ReportExport) => {
    setJobs((cur) => [e, ...cur.filter((j) => j.id !== e.id)]);
    writeHistory([e.id, ...readHistory().filter((id) => id !== e.id)]);
  }, []);

  return { jobs, add, refresh: () => void refreshIds(jobs.map((j) => j.id)) };
}

/** Kỳ báo cáo KN (FR-05): tháng YYYY-MM, quý YYYY-Qn, năm YYYY. */
export function buildPeriod(kind: "month" | "quarter" | "year", year: string, month: string, quarter: string): string {
  if (kind === "year") return year;
  if (kind === "quarter") return `${year}-Q${quarter}`;
  return `${year}-${month.padStart(2, "0")}`;
}
