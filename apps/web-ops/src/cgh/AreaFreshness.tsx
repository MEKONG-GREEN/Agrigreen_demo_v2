"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { AppGet } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

type SyncLog = AppGet<"/v1/mechanization/area-sync-log">["items"][number];

/** Kết quả áp QT-03 của một lần nhận diện tích vụ (CGH FN-07 B5). */
export const AREA_SYNC_OUTCOME: Record<SyncLog["outcome"], { label: string; cls: string }> = {
  applied: { label: "Đã áp", cls: "c-ok" },
  kept_locked: { label: "Giữ bản nhập tay đã khóa", cls: "c-info" },
  stale_ignored: { label: "Bỏ qua (cũ hơn)", cls: "c-mute" },
};

/** Thông điệp banner khi feed quá hạn: ưu tiên `message` máy chủ trả. */
export function staleMessage(s: { stale: boolean; message: string | null; threshold_hours: string }): string | null {
  if (!s.stale) return null;
  return s.message ?? `Dữ liệu App HTX chưa cập nhật quá ${s.threshold_hours} giờ — đang dùng dữ liệu nhập tay dự phòng.`;
}

/**
 * Độ tươi dữ liệu diện tích vụ từ App HTX (CGH FN-07 B5, BR-04, AC-02/AC-03) cho vụ đang xem: chip "cập nhật lúc …",
 * banner khi quá ngưỡng (đang dùng dữ liệu nhập tay dự phòng); Admin CGH xem thêm nhật ký đồng bộ.
 */
export function AreaFreshness({
  season,
  coopNames,
}: {
  season: { season_code: string; season_year: number } | null;
  coopNames: ReadonlyMap<string, { name: string; code: string | null }>;
}) {
  const { api, principal } = useAuth();
  const isAdmin = hasRole(principal, ROLES.cghAdmin, ROLES.platformAdmin);
  const code = season?.season_code ?? "";
  const year = season?.season_year ?? 0;
  const status = useAsync(
    () => (code ? unwrap(api.GET("/v1/mechanization/area-sync-status", { params: { query: { season_code: code, season_year: year } } })) : Promise.resolve(null)),
    [api, code, year],
  );
  const s = status.data;
  const msg = s ? staleMessage(s) : null;
  if (!season) return null;
  return (
    <section aria-label="Độ tươi dữ liệu App HTX" style={{ marginBottom: 16 }} data-testid="area-freshness">
      {status.error ? <ProblemAlert problem={status.error} onRetry={status.reload} /> : null}
      {s ? (
        <div className="row" style={{ gap: 8, flexWrap: "wrap", marginBottom: msg ? 8 : 0 }}>
          <span className={`chip ${s.stale ? "c-warn" : "c-ok"}`} data-testid="area-fresh-chip">
            <Icon name="clock" size={13} />
            {s.last_app_htx_at ? `Dữ liệu App HTX cập nhật lúc ${formatDateTime(s.last_app_htx_at)}` : "Chưa nhận dữ liệu App HTX cho vụ này"}
          </span>
          <span className="mute small">Ngưỡng độ tươi {s.threshold_hours} giờ</span>
        </div>
      ) : null}
      {msg ? (
        <div className="alert alert-warn" role="alert" data-testid="area-stale-banner">
          <Icon name="alert" />
          <span>{msg}</span>
        </div>
      ) : null}
      {isAdmin ? <SyncLogPanel code={code} year={year} coopNames={coopNames} /> : null}
    </section>
  );
}

function SyncLogPanel({ code, year, coopNames }: { code: string; year: number; coopNames: ReadonlyMap<string, { name: string; code: string | null }> }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState<"" | "app_htx" | "manual">("");
  const log = usePagedList(
    (cursor) =>
      open
        ? unwrap(api.GET("/v1/mechanization/area-sync-log", { params: { query: { season_code: code, season_year: year, source: source || undefined, cursor, limit: 20 } } }))
        : Promise.resolve({ items: [], next_cursor: null }),
    [api, code, year, source, open],
  );
  return (
    <details className="card sec" style={{ marginTop: 8 }} onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}>
      <summary style={{ cursor: "pointer", fontWeight: 700 }}>Nhật ký đồng bộ</summary>
      {open ? (
        <div style={{ marginTop: 10 }}>
          <div className="field" style={{ maxWidth: 260, marginBottom: 10 }}>
            <label htmlFor="area-log-source">Nguồn diện tích</label>
            <select id="area-log-source" className="select" value={source} onChange={(e) => setSource(e.target.value as "" | "app_htx" | "manual")}>
              <option value="">Tất cả</option>
              <option value="app_htx">App HTX</option>
              <option value="manual">Nhập tay</option>
            </select>
          </div>
          {log.error ? <ProblemAlert problem={log.error} onRetry={log.reload} /> : null}
          {log.loading && log.items.length === 0 ? <Loading /> : null}
          {!log.loading && !log.error && log.items.length === 0 ? <Empty>Chưa có lần đồng bộ diện tích nào cho vụ này.</Empty> : null}
          {log.items.length > 0 ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Nhật ký đồng bộ diện tích vụ">
                <thead>
                  <tr>
                    <th scope="col">Thời điểm</th>
                    <th scope="col">HTX</th>
                    <th scope="col">Nguồn</th>
                    <th scope="col">Kết quả</th>
                    <th scope="col" className="num">Diện tích (ha)</th>
                    <th scope="col">Thời điểm dữ liệu nguồn</th>
                  </tr>
                </thead>
                <tbody>
                  {log.items.map((l) => {
                    const c = coopNames.get(l.cooperative_ref);
                    return (
                      <tr key={l.id}>
                        <td className="small">{formatDateTime(l.recorded_at)}</td>
                        <td>
                          {c ? <b>{c.name}</b> : <code>{l.cooperative_ref}</code>}
                          {c?.code ? <div className="mute small">{c.code}</div> : null}
                        </td>
                        <td>{l.source_label}</td>
                        <td>
                          <span className={`chip ${AREA_SYNC_OUTCOME[l.outcome].cls}`}>{AREA_SYNC_OUTCOME[l.outcome].label}</span>
                        </td>
                        <td className="num">{formatDecimal(l.area_ha, 2)}</td>
                        <td className="small">{formatDateTime(l.source_at)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : null}
          <LoadMore hasMore={log.hasMore} loading={log.loading} onClick={log.loadMore} />
        </div>
      ) : null}
    </details>
  );
}
