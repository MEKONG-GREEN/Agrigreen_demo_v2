"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { unitLabel } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { MrvCompleteness, MrvEstimate } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { useUnits } from "@/lib/useUnits";
import { completenessSummary, estimateLabel, ITEM_STATUS_CHIP, ITEM_STATUS_LABEL, SOURCE_LABEL } from "@/mrv/rules";

const TABS = [
  { id: "ho-so", label: "Đủ hồ sơ theo thửa/vụ" },
  { id: "bat-thuong", label: "Bất thường (FR-08)" },
  { id: "uoc-tinh", label: "Ước tính CO2 (tạm)" },
];

const SEASON_STATUS: Record<string, string> = { open: "Đang mở", completed: "Đã kết thúc", cancelled: "Đã hủy" };
const short = (ref: string) => `#${ref.slice(-6)}`;

/** MRV (pf/mrv G3): tình trạng bằng chứng theo thửa/vụ, cảnh báo không ghi nhận (KN FR-08), ước tính CO2 TẠM. */
export default function MrvPage() {
  const { api } = useAuth();
  const [tab, setTab] = useState("ho-so");
  const list = useAsync(
    () => collectPages<MrvCompleteness>((cursor) => unwrap(api.GET("/v1/mrv/completeness", { params: { query: { cursor, limit: 200, anomaly: tab === "bat-thuong" ? true : undefined } } })), 2000),
    [api, tab === "bat-thuong"],
  );
  const units = useUnits(api, useMemo(() => (list.data ?? []).map((c) => c.season.commune_code), [list.data]));

  return (
    <>
      <PageHead title="MRV — hồ sơ, đối chiếu, ước tính" subtitle="Minh chứng giảm phát thải theo thửa/vụ trong phạm vi được phân quyền" />
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Nội dung MRV">
        <div style={{ marginTop: 12 }}>
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {tab === "uoc-tinh" ? (
            <EstimatesTab units={units} />
          ) : list.loading && !list.data ? (
            <Loading />
          ) : tab === "ho-so" ? (
            <CompletenessTable rows={list.data ?? []} units={units} />
          ) : (
            <AnomalyTable rows={list.data ?? []} units={units} />
          )}
        </div>
      </Tabs>
    </>
  );
}

type Units = ReturnType<typeof useUnits>;

function CompletenessTable({ rows, units }: { rows: MrvCompleteness[]; units: Units }) {
  if (rows.length === 0) return <p className="mute small">Chưa có thửa/vụ nào được liên kết vào MRV trong phạm vi.</p>;
  return (
    <div className="card sec">
      <p className="small mute" style={{ marginBottom: 8 }}>
        Chỉ minh chứng cán bộ <b style={{ display: "inline" }}>đã duyệt “Đạt chuẩn”</b> mới được tính vào ước tính giảm phát thải (KN US-MRV-05).
      </p>
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Tình trạng hồ sơ MRV theo thửa/vụ">
          <thead>
            <tr>
              <th scope="col">Thửa · vụ</th>
              <th scope="col">Xã</th>
              <th scope="col">Hồ sơ</th>
              <th scope="col">Chi tiết</th>
              <th scope="col">Tính vào ước tính</th>
              <th scope="col">Đối chiếu</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((c) => {
              const sum = completenessSummary(c);
              return (
                <tr key={c.season.season_ref} data-season={c.season.season_ref}>
                  <td>
                    <b>Thửa {short(c.season.plot_ref)}</b>
                    <div className="small mute">
                      {c.season.season_code} {c.season.season_year} · {SEASON_STATUS[c.season.status] ?? c.season.status} · sạ {formatDate(c.season.sowing_date)}
                    </div>
                  </td>
                  <td>{unitLabel(units, c.season.commune_code, false)}</td>
                  <td>
                    <span className={`chip ${sum.missing.length === 0 ? "c-ok" : "c-warn"}`}>
                      {formatInt(sum.done)}/{formatInt(sum.total)} mục
                    </span>
                    {c.anomaly.no_record ? <span className="chip c-bad" style={{ marginLeft: 4 }}>Không ghi nhận ≥ {c.anomaly.threshold_days} ngày</span> : null}
                  </td>
                  <td>
                    <ul className="list-plain small">
                      {c.items.map((i) => (
                        <li key={i.key}>
                          {SOURCE_LABEL[i.source]} <code>{i.key}</code>
                          {i.catalog_version ? ` (danh mục v${i.catalog_version})` : ""}: <span className={`chip ${ITEM_STATUS_CHIP[i.status]}`}>{ITEM_STATUS_LABEL[i.status]}</span>{" "}
                          {i.count > 0 ? `${formatInt(i.approved_count)}/${formatInt(i.count)} đã duyệt` : ""}
                        </li>
                      ))}
                    </ul>
                  </td>
                  <td>{c.counts_toward_estimate ? <span className="chip c-ok">Có</span> : <span className="chip c-warn">Chưa (thiếu minh chứng đạt chuẩn)</span>}</td>
                  <td>
                    <Link className="btn btn-o btn-sm" href={`/mrv/doi-chieu?season=${c.season.season_ref}`}>
                      Đối chiếu HTX ↔ KN
                    </Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function AnomalyTable({ rows, units }: { rows: MrvCompleteness[]; units: Units }) {
  return (
    <div className="card sec">
      <p className="small" style={{ marginBottom: 8 }}>
        KN FR-08: vụ đang mở không có bản ghi mới (nhật ký HTX hoặc minh chứng cán bộ) từ {formatInt(rows[0]?.anomaly.threshold_days ?? 14)} ngày trở lên. Ngưỡng là tham số{" "}
        <code>mrv.anomaly_no_record_days</code>.
      </p>
      {rows.length === 0 ? (
        <p className="small mute" role="status">
          Không có vụ nào bất thường trong phạm vi.
        </p>
      ) : (
        <table className="tbl" aria-label="Vụ không ghi nhận (FR-08)">
          <thead>
            <tr>
              <th scope="col">Thửa · vụ</th>
              <th scope="col">Xã</th>
              <th scope="col">Bản ghi gần nhất</th>
              <th scope="col" className="num">Số ngày</th>
              <th scope="col">Việc cần làm</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((c) => (
              <tr key={c.season.season_ref}>
                <td>
                  <b>Thửa {short(c.season.plot_ref)}</b> · {c.season.season_code} {c.season.season_year}
                </td>
                <td>{unitLabel(units, c.season.commune_code, false)}</td>
                <td>{formatDateTime(c.anomaly.last_record_at)}</td>
                <td className="num">
                  <span className="chip c-bad">{formatInt(c.anomaly.days_since)} ngày</span>
                </td>
                <td>
                  <Link href={`/mrv/doi-chieu?season=${c.season.season_ref}`}>Xem đối chiếu</Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function EstimatesTab({ units }: { units: Units }) {
  const { api, principal } = useAuth();
  const canRecompute = hasRole(principal, ROLES.mrvReviewer);
  const est = useAsync(() => collectPages<MrvEstimate>((cursor) => unwrap(api.GET("/v1/mrv/estimates", { params: { query: { cursor, limit: 200 } } })), 2000), [api]);
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const extra = useUnits(api, useMemo(() => (est.data ?? []).map((e) => e.commune_code), [est.data]));
  const names = extra.size ? extra : units;

  const recompute = async (ref: string) => {
    setBusy(ref);
    setProblem(null);
    try {
      await unwrap(api.POST("/v1/mrv/seasons/{id}/estimate/recompute", { params: { path: { id: ref }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      setDone(ref);
      est.reload();
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(null);
    }
  };

  const rows = est.data ?? [];
  const anyProvisional = rows.some((e) => !estimateLabel(e).official);
  return (
    <div className="card sec">
      {anyProvisional ? (
        <div className="alert alert-warn" role="note" data-testid="estimate-provisional" style={{ marginBottom: 10 }}>
          <b>TẠM — chờ khách ký phương pháp.</b>&nbsp;Số liệu dưới đây là ước tính theo quy tắc <code>mrv.co2.estimate.v1</code> với hệ số mặc định; chỉ chính thức khi
          phương pháp đã được khách ký và duyệt (người duyệt khác người đề xuất). Không dùng để báo cáo tín chỉ.
        </div>
      ) : null}
      {est.error ? <ProblemAlert problem={est.error} onRetry={est.reload} /> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {est.loading && !est.data ? <Loading /> : null}
      {est.data && rows.length === 0 ? <p className="small mute">Chưa có ước tính (chưa có bằng chứng thu gom).</p> : null}
      {rows.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Ước tính CO2 theo thửa/vụ">
            <thead>
              <tr>
                <th scope="col">Thửa · vụ</th>
                <th scope="col">Xã</th>
                <th scope="col" className="num">Rơm tính (tấn)</th>
                <th scope="col" className="num">Tránh đốt (kgCO2e)</th>
                <th scope="col">Tham số</th>
                <th scope="col">Trạng thái</th>
                {canRecompute ? <th scope="col">Tính lại</th> : null}
              </tr>
            </thead>
            <tbody>
              {rows.map((e) => {
                const lbl = estimateLabel(e);
                const excluded = e.inputs.find((i) => !i.counted);
                return (
                  <tr key={e.id} data-estimate={e.season_ref}>
                    <td>
                      <b>Thửa {short(e.plot_ref)}</b> · {e.season_code}
                      <div className="small mute">
                        {e.rule_version} · tính {formatDateTime(e.computed_at)}
                      </div>
                    </td>
                    <td>{unitLabel(names, e.commune_code, false)}</td>
                    <td className="num">{formatDecimal(e.counted_tonnes, 0)}</td>
                    <td className="num">
                      {formatDecimal(e.avoided_burning_kgco2e, 0)}
                      {excluded ? <div className="small mute">Chưa tính: chưa có minh chứng đạt chuẩn</div> : null}
                    </td>
                    <td className="small">
                      {e.params.map((p) => (
                        <div key={p.key}>
                          <code>{p.key}</code> = {p.value} {p.unit} {p.source === "default" ? "(mặc định, chưa duyệt)" : `(bản duyệt v${p.version})`}
                        </div>
                      ))}
                    </td>
                    <td>
                      <span className={`chip ${lbl.official ? "c-ok" : "c-warn"}`} data-testid="estimate-label">
                        {lbl.text}
                      </span>
                    </td>
                    {canRecompute ? (
                      <td>
                        <button type="button" className="btn btn-o btn-sm" disabled={busy === e.season_ref} onClick={() => void recompute(e.season_ref)}>
                          {busy === e.season_ref ? "Đang tính…" : "Tính lại"}
                        </button>
                        {done === e.season_ref ? <div className="small mute" role="status">Không đổi đầu vào — giữ ước tính hiện hành.</div> : null}
                      </td>
                    ) : null}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
