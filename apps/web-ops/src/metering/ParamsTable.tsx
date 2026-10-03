"use client";

import { unwrap } from "@/api/client";
import { CHANNEL_LABEL } from "@/apiclient/rules";
import type { EgressChannel } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { useAsync } from "@/lib/useAsync";
import { formatVnd } from "./money";

const SUFFIX: Record<string, string> = {
  daily_records_quota: "Hạn mức bản ghi/ngày (0 = không giới hạn riêng)",
  anomaly_factor: "Hệ số bất thường (× trung vị 28 ngày)",
  anomaly_records: "Ngưỡng tuyệt đối khi < 7 ngày mẫu",
};

/** Mô tả tiếng Việt của khóa tham số pf/metering. */
export function describeParam(key: string): string {
  const eg = /^metering\.egress\.([a-z_]+)\.([a-z_]+)$/.exec(key);
  if (eg) return `${CHANNEL_LABEL[eg[1] as EgressChannel] ?? eg[1]} — ${SUFFIX[eg[2]!] ?? eg[2]}`;
  const unit = /^metering\.price\.([a-z_]+)\.([a-z_]+)\.per_unit_vnd$/.exec(key);
  if (unit) return `Đơn giá mỗi đơn vị — ${unit[2]} (gói ${unit[1]})`;
  const sub = /^metering\.price\.([a-z_]+)\.subscription_vnd_per_month$/.exec(key);
  if (sub) return `Thuê bao hằng tháng (gói ${sub[1]})`;
  return key;
}

/** Hiển thị giá trị: giá là tiền VND (chuỗi thập phân), còn lại là số nguyên/hệ số dạng chuỗi. */
export function paramValue(key: string, value: string | null): string {
  if (value === null) return "Chưa đặt";
  if (key.startsWith("metering.price.")) return formatVnd(value, { maxFraction: 4 });
  return formatVnd(value, { maxFraction: 4, currency: false });
}

const SOURCE: Record<string, string> = { default: "Mặc định (tạm, chờ khách)", unset: "Chưa đặt — chờ khách (Q5-4)" };

/**
 * Tham số hiệu lực của pf/metering (GET /v1/metering/params): hạn mức/ngưỡng DLP theo kênh và giá tùy chọn.
 * Chỉ xem — đề xuất/duyệt qua “Tham số & phê duyệt” (param_setter ≠ param_approver, R-56).
 */
export function ParamsTable() {
  const { adminApi } = useAuth();
  const params = useAsync(() => unwrap(adminApi.GET("/v1/metering/params")), [adminApi]);
  if (params.error) return <ProblemAlert problem={params.error} onRetry={params.reload} />;
  if (params.loading && !params.data) return <Loading />;
  return (
    <div className="stack" style={{ gap: 8 }}>
      <p className="small mute">Giá chưa đặt thì bảng kê chỉ có đơn vị; lập hóa đơn ngoài hệ thống. Đổi tham số qua màn hình “Tham số &amp; phê duyệt”.</p>
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Tham số đo lường">
          <thead>
            <tr>
              <th scope="col">Tham số</th>
              <th scope="col">Giá trị</th>
              <th scope="col">Nguồn</th>
            </tr>
          </thead>
          <tbody>
            {(params.data?.items ?? []).map((p) => (
              <tr key={p.key} data-param={p.key}>
                <td>
                  {describeParam(p.key)}
                  <div className="small mute">
                    <code>{p.key}</code>
                  </div>
                </td>
                <td>{paramValue(p.key, p.value)}</td>
                <td className="small">{SOURCE[p.source] ?? `Phiên bản đã duyệt ${p.source}`}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
