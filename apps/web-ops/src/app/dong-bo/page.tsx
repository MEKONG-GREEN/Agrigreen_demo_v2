"use client";

import { useState, type FormEvent } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { NudgeInput, NudgeResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useNow } from "@/components/SlaCountdown";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const keyOf = (t: NudgeInput) => (t.device_id ? `device:${t.device_id}` : `unit:${t.unit ?? ""}`);

/**
 * "Đồng bộ ngay" (US-SYNC-02) — POST /v1/sync/nudge gửi thông điệp dữ liệu {kind: sync_now} tới máy.
 * Giới hạn 1 lần/5 phút mỗi đích, 30 lần/giờ mỗi người: 429 notify.nudge_rate_limited kèm Retry-After →
 * khoá nút của đích đó và đếm ngược.
 */
function useNudge() {
  const { api } = useAuth();
  const [busy, setBusy] = useState<string | null>(null);
  const [retryUntil, setRetryUntil] = useState<Record<string, number>>({});
  const [results, setResults] = useState<Record<string, NudgeResult>>({});
  const [problems, setProblems] = useState<Record<string, ApiProblem>>({});

  const send = async (target: NudgeInput) => {
    const k = keyOf(target);
    setBusy(k);
    setProblems((s) => {
      const next = { ...s };
      delete next[k];
      return next;
    });
    try {
      const r = await unwrap(api.POST("/v1/sync/nudge", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: target }));
      setResults((s) => ({ ...s, [k]: r }));
    } catch (e) {
      const p = toApiProblem(e);
      setProblems((s) => ({ ...s, [k]: p }));
      if (p.status === 429) setRetryUntil((s) => ({ ...s, [k]: Date.now() + (p.retryAfterS ?? 300) * 1000 }));
    } finally {
      setBusy(null);
    }
  };
  return { send, busy, retryUntil, results, problems };
}

function NudgeButton({ target, n, now }: { target: NudgeInput; n: ReturnType<typeof useNudge>; now: number }) {
  const k = keyOf(target);
  const wait = Math.max(0, Math.ceil(((n.retryUntil[k] ?? 0) - now) / 1000));
  const r = n.results[k];
  const p = n.problems[k];
  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className="btn btn-p btn-sm" disabled={n.busy === k || wait > 0} onClick={() => void n.send(target)}>
        <Icon name="sync" size={15} />
        {n.busy === k ? "Đang gửi…" : wait > 0 ? `Thử lại sau ${Math.floor(wait / 60)}:${String(wait % 60).padStart(2, "0")}` : "Đồng bộ ngay"}
      </button>
      {r && !p ? (
        <span className="small" role="status" style={{ color: "var(--g800)" }}>
          Đã xếp lượt gửi tới {r.devices} thiết bị.
        </span>
      ) : null}
      {p ? <ProblemAlert problem={p} /> : null}
    </div>
  );
}

function DeviceList({ n, now }: { n: ReturnType<typeof useNudge>; now: number }) {
  const { adminApi } = useAuth();
  const devices = useAsync(() => collectPages((cursor) => unwrap(adminApi.GET("/v1/devices", { params: { query: { cursor, limit: 200 } } })), 1000), [adminApi]);
  return (
    <>
      {devices.error ? <ProblemAlert problem={devices.error} onRetry={devices.reload} /> : null}
      {devices.loading && !devices.data ? <Loading /> : null}
      {devices.data ? (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Thiết bị đã đăng ký</caption>
            <thead>
              <tr>
                <th scope="col">Thiết bị</th>
                <th scope="col">Nền tảng</th>
                <th scope="col">Lần cuối kết nối</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {devices.data.map((d) => (
                <tr key={d.device_id}>
                  <td>
                    <code className="small">{d.device_id}</code>
                  </td>
                  <td>
                    {d.platform === "ios" ? "iOS" : "Android"} · app {d.app_version}
                  </td>
                  <td className="small">{formatDateTime(d.last_seen_at)}</td>
                  <td>{d.revoked ? <span className="chip c-mute">Đã thu hồi</span> : <span className="chip c-ok">Đang dùng</span>}</td>
                  <td style={{ minWidth: 220 }}>{d.revoked ? null : <NudgeButton target={{ device_id: d.device_id }} n={n} now={now} />}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </>
  );
}

export default function SyncNudgePage() {
  const { api, principal } = useAuth();
  const now = useNow(1000);
  const n = useNudge();
  const province = principal?.level === "tinh" ? (principal.scopeUnits.find((u) => u.length === 2) ?? null) : null;
  const units = useAsync(async () => {
    const list = (await listChildUnits(api, province)).map((u) => ({ code: u.code, name: u.name }));
    return province ? [{ code: province, name: `Toàn tỉnh ${principal?.unitName ?? province}` }, ...list] : list;
  }, [api, province]);
  const [unit, setUnit] = useState("");
  const [deviceId, setDeviceId] = useState("");
  const [manual, setManual] = useState<string | null>(null);
  const deviceAdmin = hasRole(principal, ROLES.deviceAdmin, ROLES.platformAdmin);
  const chosenUnit = unit || units.data?.[0]?.code || "";

  const submitDevice = (e: FormEvent) => {
    e.preventDefault();
    setManual(deviceId.trim() || null);
  };

  return (
    <>
      <PageHead title="Đồng bộ thiết bị" subtitle="Yêu cầu app hiện trường đồng bộ ngay khi có chỉ đạo, cảnh báo hoặc dữ liệu cần cập nhật gấp" />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Máy nhận thông điệp đẩy chỉ chứa lệnh <code>sync_now</code> (không có nội dung, ADR-0015) rồi tự đồng bộ qua máy chủ trong nước. Mỗi
          thiết bị/đơn vị chỉ được yêu cầu 1 lần trong 5 phút; mỗi người tối đa 30 lần/giờ.
        </span>
      </div>
      <section className="card sec" style={{ marginBottom: 16 }} aria-labelledby="nd-unit">
        <div className="sec-h">
          <h2 id="nd-unit">Theo đơn vị</h2>
        </div>
        <div className="row" style={{ alignItems: "flex-end" }}>
          <div className="field">
            <label htmlFor="nd-unit-pick">Đơn vị (mọi thiết bị trong đơn vị)</label>
            <select id="nd-unit-pick" className="select" value={chosenUnit} onChange={(e) => setUnit(e.target.value)}>
              {(units.data ?? []).map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
            </select>
          </div>
          {chosenUnit ? <NudgeButton target={{ unit: chosenUnit }} n={n} now={now} /> : null}
        </div>
      </section>
      <section className="card sec" aria-labelledby="nd-dev">
        <div className="sec-h">
          <h2 id="nd-dev">Theo thiết bị</h2>
        </div>
        {deviceAdmin ? (
          <DeviceList n={n} now={now} />
        ) : (
          <>
            <form className="row" style={{ alignItems: "flex-end", marginBottom: 10 }} onSubmit={submitDevice}>
              <div className="field" style={{ flex: "1 1 320px" }}>
                <label htmlFor="nd-device">Mã thiết bị (device_id)</label>
                <input id="nd-device" className="input" value={deviceId} onChange={(e) => setDeviceId(e.target.value)} placeholder="01927b3e-…" />
              </div>
              <button type="submit" className="btn btn-o btn-sm">
                Chọn thiết bị
              </button>
            </form>
            {manual ? <NudgeButton target={{ device_id: manual }} n={n} now={now} /> : null}
          </>
        )}
      </section>
    </>
  );
}
