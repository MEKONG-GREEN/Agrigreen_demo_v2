"use client";

import { unwrap } from "@/api/client";
import type { ScaleDevice } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Chip, Section, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { deviceHealth, HEALTH_LIMITS } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const LEVEL: Record<string, { status: string; label: string }> = {
  ok: { status: "within", label: "Bình thường" },
  warn: { status: "flag", label: "Cần chú ý" },
  bad: { status: "critical", label: "Sự cố" },
  unknown: { status: "not_applicable", label: "Chưa có số liệu" },
};

export default function DeviceHealthPage() {
  const { api } = useAuth();
  const facilities = useFacilities();
  const list = useAsync(async () => ((await unwrap(api.GET("/v1/measurement/devices", { params: { query: { limit: 200 } } }))).items ?? []) as ScaleDevice[], [api]);
  const active = (list.data ?? []).filter((d) => d.status === "active");
  return (
    <div className="stack">
      <PageHead
        title="Sức khoẻ thiết bị"
        subtitle={`Do cmd/ingest báo: lần cuối nhận lô số đo, độ đầy bộ đệm (≥ 72 giờ; đầy thì ngừng), độ lệch đồng hồ NTP (ghi nhận, không dùng làm thứ tự). Ngưỡng hiển thị: mất tín hiệu > ${HEALTH_LIMITS.staleMinutes} phút, đệm ≥ ${HEALTH_LIMITS.bufferWarn}/${HEALTH_LIMITS.bufferBad} %, lệch > ${HEALTH_LIMITS.skewWarnMs} ms.`}
      />
      <Section title="Thiết bị đang hiệu lực">
        {list.loading ? <Loading /> : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.data ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Sức khoẻ thiết bị cân">
              <thead>
                <tr>
                  <th>Thiết bị</th>
                  <th>Cơ sở · điểm</th>
                  <th>Lần cuối thấy</th>
                  <th className="num">Bộ đệm</th>
                  <th className="num">Lệch đồng hồ</th>
                  <th className="num">Bộ đếm cuối</th>
                  <th>Tình trạng</th>
                </tr>
              </thead>
              <tbody>
                {active.map((d) => {
                  const h = deviceHealth(d);
                  const lv = LEVEL[h.level]!;
                  return (
                    <tr key={`${d.device_id}#${d.epoch}`}>
                      <td className="mono">
                        {d.device_id}#{d.epoch}
                      </td>
                      <td>
                        {facilities.data?.find((f) => f.id === d.facility_ref)?.code ?? "—"} · {d.point}
                      </td>
                      <td>{formatDateTime(d.health?.last_seen_at)}</td>
                      <td className="num">{d.health?.buffer_pct != null ? `${d.health.buffer_pct} %` : "—"}</td>
                      <td className="num">{d.health?.clock_skew_ms != null ? `${d.health.clock_skew_ms} ms` : "—"}</td>
                      <td className="num">{d.health?.last_counter ?? "—"}</td>
                      <td>
                        <Chip status={lv.status} label={lv.label} />
                        {h.issues.length ? <div className="small">{h.issues.join(" · ")}</div> : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
      </Section>
    </div>
  );
}
