"use client";

import { useEffect, useState } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem} from "@/api/problem";
import { parseProblemResponse } from "@/api/problem";
import type { CropFarmLogState } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { LocationChip, ReasonDialog, useCatalog } from "@/htx/common";
import { activityLabel } from "@/htx/rules";
import { CoopPicker, useCoopFields, useHtxScope } from "@/htx/scope";
import { useSeasonIndex } from "@/htx/seasons";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

/**
 * Ảnh minh chứng mở qua luồng tệp theo mục đích (GET /v1/files/{ref}/content, X-Purpose, R-42).
 * Bản rõ chỉ nằm trong bộ nhớ trình duyệt (object URL), thu hồi khi đóng.
 */
function EvidencePhoto({ fileRef }: { fileRef: string }) {
  const { api } = useAuth();
  const [url, setUrl] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => () => {
    if (url) URL.revokeObjectURL(url);
  }, [url]);
  const open = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const res = await api.GET("/v1/files/{ref}/content", { params: { path: { ref: fileRef }, header: { "X-Purpose": "extension.field_support" } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      setUrl(URL.createObjectURL(res.data as Blob));
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack" style={{ gap: 6 }}>
      <div className="row small">
        <code title="Mã tệp minh chứng">{fileRef.slice(0, 8)}…</code>
        {!url ? (
          <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void open()}>
            {busy ? "Đang mở…" : "Xem ảnh"}
          </button>
        ) : null}
      </div>
      {/* eslint-disable-next-line @next/next/no-img-element -- ảnh blob: giải mã tại chỗ, không qua next/image */}
      {url ? <img src={url} alt={`Ảnh minh chứng ${fileRef.slice(0, 8)}`} style={{ maxWidth: 280, borderRadius: 8 }} /> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
    </div>
  );
}

function LogDetail({ id }: { id: string }) {
  const { api } = useAuth();
  const machines = useCatalog("machine_type");
  const d = useAsync(() => unwrap(api.GET("/v1/cropcycle/farm-logs/{id}", { params: { path: { id } } })), [api, id]);
  if (d.error) return <ProblemAlert problem={d.error} />;
  if (!d.data) return <Loading />;
  const log = d.data.log;
  const machineName = (c: string) => machines.find((m) => m.code === c)?.name ?? c;
  return (
    <div className="stack" style={{ gap: 8 }}>
      <dl style={{ display: "grid", gridTemplateColumns: "160px 1fr", gap: "6px 12px" }} className="small">
        <dt className="mute">Ghi chú</dt>
        <dd>{log.note ?? "—"}</dd>
        <dt className="mute">Vật tư</dt>
        <dd>{log.inputs?.length ? log.inputs.map((i) => `${i.name} ${i.qty} ${i.unit}`).join("; ") : "—"}</dd>
        <dt className="mute">Máy móc</dt>
        <dd>{log.machine_codes?.length ? log.machine_codes.map(machineName).join(", ") : "—"}</dd>
        <dt className="mute">Nguồn vị trí</dt>
        <dd>
          {log.gps?.source ?? "—"}
          {log.distance_m ? ` · cách ranh giới thửa ${log.distance_m} m` : ""}
        </dd>
        <dt className="mute">Phiên bản</dt>
        <dd>
          {log.version}
          {log.correction_reason ? ` — sửa: ${log.correction_reason}` : ""}
        </dd>
      </dl>
      <div>
        <b className="small">Ảnh minh chứng ({log.file_refs?.length ?? 0})</b>
        <div className="row" style={{ alignItems: "flex-start", marginTop: 6 }}>
          {(log.file_refs ?? []).map((f) => (
            <EvidencePhoto key={f} fileRef={f} />
          ))}
          {(log.file_refs ?? []).length === 0 ? <span className="mute small">Không có ảnh.</span> : null}
        </div>
      </div>
    </div>
  );
}

/** Hàng chờ duyệt nhật ký (HTX US-LOG-05): phê duyệt / yêu cầu bổ sung (bắt buộc ghi chú); người ghi không tự duyệt. */
export default function ReviewPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const [version, setVersion] = useState(0);
  const [open, setOpen] = useState<string | null>(null);
  const [supplement, setSupplement] = useState<CropFarmLogState | null>(null);
  const [problems, setProblems] = useState<Record<string, ApiProblem>>({});
  const [done, setDone] = useState<string | null>(null);
  const index = useSeasonIndex(scope.coopRef);
  const fields = useCoopFields(scope.coopRef);
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cropcycle/farm-logs", { params: { query: { cooperative_ref: scope.coopRef || undefined, status: "pending", cursor, limit: 50 } } })),
    [api, scope.coopRef, version],
  );
  const reload = () => setVersion((v) => v + 1);

  const approve = async (l: CropFarmLogState) => {
    setProblems((p) => {
      const n = { ...p };
      delete n[l.id];
      return n;
    });
    try {
      await unwrap(
        api.POST("/v1/cropcycle/farm-logs/{id}/reviews", { params: { path: { id: l.id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { decision: "approved" } }),
      );
      setDone(`Đã phê duyệt nhật ký lô ${index.byRef.get(l.season_ref)?.plot_code ?? ""} — tính vào bảng điều hành từ bây giờ.`);
      reload();
    } catch (e) {
      setProblems((p) => ({ ...p, [l.id]: toApiProblem(e) }));
    }
  };

  return (
    <>
      <PageHead title="Phê duyệt nhật ký" subtitle="Chỉ nhật ký đã duyệt mới tính vào số liệu chính thức của HTX (tiến độ SOP trên bảng điều hành)" />
      <div className="card sec">
        <div className="row" style={{ marginBottom: 14, alignItems: "flex-end" }}>
          <CoopPicker scope={scope} />
        </div>
        {done ? (
          <p role="status" className="alert alert-info">
            <Icon name="check" /> {done}
          </p>
        ) : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <ul className="list-plain" aria-label="Nhật ký chờ duyệt">
          {list.items.map((l) => {
            const s = index.byRef.get(l.season_ref);
            const warn = l.location_status === "mismatch" || l.location_status === "missing";
            const label = `${s?.plot_code ?? "Lô"} — ${activityLabel(l.activity_code)}`;
            return (
              <li key={l.id} aria-label={label}>
                <div className="row" style={{ justifyContent: "space-between" }}>
                  <div>
                    <b style={{ display: "inline" }}>{s?.plot_code ?? "—"}</b> · {activityLabel(l.activity_code)}
                    {l.sop_step_seq ? ` (bước ${l.sop_step_seq})` : ""} · {formatDateTime(l.event_time)}
                    <div className="small mute">
                      {fields.householdLabel(l.household_ref)} · {l.on_behalf ? "Quản lý HTX ghi hộ" : "Nông dân tự ghi"}
                      {l.version > 1 ? ` · phiên bản ${l.version}` : ""}
                    </div>
                  </div>
                  <div className="row">
                    <LocationChip status={l.location_status} distance={l.distance_m} />
                    <button type="button" className="btn btn-o btn-sm" aria-expanded={open === l.id} onClick={() => setOpen((o) => (o === l.id ? null : l.id))}>
                      {open === l.id ? "Ẩn chi tiết" : "Chi tiết"}
                    </button>
                    <button type="button" className="btn btn-p btn-sm" onClick={() => void approve(l)}>
                      Phê duyệt
                    </button>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setSupplement(l)}>
                      Yêu cầu bổ sung
                    </button>
                  </div>
                </div>
                {warn ? (
                  <div className="alert alert-warn small" style={{ marginTop: 8 }} role="note">
                    <Icon name="alert" />
                    <span>
                      {l.location_status === "mismatch"
                        ? `Vị trí không khớp: điểm GPS lúc ghi cách ranh giới thửa ${l.distance_m ?? "?"} m — kiểm tra trước khi duyệt.`
                        : "Thiếu GPS: thiết bị không gửi vị trí (từ chối quyền hoặc ghi hộ trên web)."}
                    </span>
                  </div>
                ) : null}
                {problems[l.id] ? <ProblemAlert problem={problems[l.id]!} /> : null}
                {open === l.id ? (
                  <div className="panel" style={{ marginTop: 8 }}>
                    <LogDetail id={l.id} />
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không còn nhật ký chờ duyệt.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      {supplement ? (
        <ReasonDialog
          title={`Yêu cầu bổ sung — lô ${index.byRef.get(supplement.season_ref)?.plot_code ?? ""}`}
          label="Nội dung cần bổ sung (bắt buộc, gửi tới nông dân)"
          min={5}
          confirmText="Gửi yêu cầu bổ sung"
          onClose={() => setSupplement(null)}
          run={async (note, key) => {
            await unwrap(
              api.POST("/v1/cropcycle/farm-logs/{id}/reviews", {
                params: { path: { id: supplement.id }, header: { "Idempotency-Key": key } },
                body: { decision: "needs_supplement", note },
              }),
            );
            setDone(`Đã yêu cầu bổ sung nhật ký lô ${index.byRef.get(supplement.season_ref)?.plot_code ?? ""} — nông dân được thông báo để chỉnh sửa.`);
            setSupplement(null);
            reload();
          }}
        />
      ) : null}
    </>
  );
}
