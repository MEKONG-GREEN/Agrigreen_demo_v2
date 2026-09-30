"use client";

import { useMemo } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { groupDefinitions, unitLabel } from "@/metrics/series";

const GRAIN = { day: "Theo ngày", season: "Theo vụ" } as const;
const AGG: Record<string, string> = { sum: "Tổng", count: "Đếm", avg: "Bình quân", max: "Lớn nhất", last: "Giá trị cuối" };

/**
 * Định nghĩa chỉ số có phiên bản (GET /v1/metrics/definitions) — CHỈ XEM. Định nghĩa là tệp khai báo trong
 * kho mã (internal/platform/metrics/defs); đổi cách tính = thêm phiên bản mới, không sửa phiên bản cũ.
 */
export default function MetricDefinitionsPage() {
  const { api } = useAuth();
  const defs = useAsync(() => unwrap(api.GET("/v1/metrics/definitions")), [api]);
  const groups = useMemo(() => groupDefinitions(defs.data?.items ?? []), [defs.data]);

  return (
    <>
      <PageHead title="Định nghĩa chỉ số" subtitle="Chỉ số có phiên bản dùng cho bảng điều hành và lịch sử đã chốt — chỉ xem" />
      <div className="alert alert-info" role="note" style={{ marginBottom: 16 }}>
        Định nghĩa được khai báo trong kho mã và duyệt qua quy trình phát hành; phiên bản đã dùng không bị sửa tại chỗ. Người duyệt ghi “tạm, chờ
        khách” là định nghĩa chưa được khách hàng xác nhận.
      </div>
      {defs.error ? <ProblemAlert problem={defs.error} onRetry={defs.reload} /> : null}
      {defs.loading && !defs.data ? <Loading /> : null}
      <div className="stack">
        {groups.map((g) => (
          <section key={g.key} className="card sec" aria-labelledby={`def-${g.key}`} data-key={g.key}>
            <div className="sec-h" style={{ flexWrap: "wrap" }}>
              <h2 id={`def-${g.key}`}>
                {g.effective.title} <code className="small">{g.key}</code>
              </h2>
              <span className="chip c-ok">Đang hiệu lực: v{g.effective.version}</span>
            </div>
            <p className="small">{g.effective.description}</p>
            <div className="tbl-wrap" style={{ marginTop: 8 }}>
              <table className="tbl" aria-label={`Các phiên bản của ${g.key}`}>
                <thead>
                  <tr>
                    <th scope="col">Phiên bản</th>
                    <th scope="col">Hiệu lực từ</th>
                    <th scope="col">Đơn vị · mức</th>
                    <th scope="col">Nguồn</th>
                    <th scope="col">Quy tắc</th>
                    <th scope="col">Đề xuất / duyệt</th>
                    <th scope="col">Tệp khai báo</th>
                  </tr>
                </thead>
                <tbody>
                  {g.versions.map((v) => (
                    <tr key={v.version}>
                      <td>
                        v{v.version}
                        {v.version === g.effective.version ? <span className="chip c-ok" style={{ marginLeft: 6 }}>hiện hành</span> : null}
                      </td>
                      <td>{formatDate(v.effective_from)}</td>
                      <td className="small">
                        {unitLabel(v.unit)} · {GRAIN[v.grain]} · {AGG[v.aggregation] ?? v.aggregation}
                      </td>
                      <td className="small">
                        {v.source_module} — {v.source}
                      </td>
                      <td>
                        <code>{v.rule_version}</code>
                      </td>
                      <td className="small">
                        {v.proposed_by} / {v.approved_by}
                        {/tạm|chờ khách/.test(v.approved_by) ? <span className="chip c-warn" style={{ marginLeft: 6 }}>tạm, chờ khách</span> : null}
                      </td>
                      <td className="small">
                        <code>{v.file}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        ))}
      </div>
    </>
  );
}
