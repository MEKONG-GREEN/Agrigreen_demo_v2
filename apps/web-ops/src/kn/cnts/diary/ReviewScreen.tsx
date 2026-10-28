"use client";

// Rà soát, xác thực nhật ký (W11.48 — BRD CNTS v3.7 UR-TINH-12, FN-13; prototype A menu Tỉnh "Rà soát nhật ký", resources
// "diaries" Xác thực / Yêu cầu bổ sung): danh sách lần ghi chờ rà soát của chương trình mình (chủ nhiệm) hoặc của phòng
// (lãnh đạo phòng); chi tiết: số liệu so lần trước, kết quả kiểm tra tự động, vị trí so với điểm, biểu đồ tăng trọng, ảnh;
// Xác thực (người xác thực khác người ghi) hoặc Yêu cầu bổ sung có lý do. Lần ghi đã xác thực không sửa.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { ReasonBox } from "@/kn/cnts/model/parts";
import { createCntsDiaryApi, dmy, FORM_LABEL, KIND_LABEL, type CntsDiaryApi, type EntryStatus } from "./api";
import { Checks, flagged, LocationInfo, MetricsTable, Photos, StatusBadge, TablesView, WeightChart } from "./parts";

export function ReviewScreen() {
  const { api } = useAuth();
  const dapi = useMemo(() => (api ? createCntsDiaryApi(api) : null), [api]);
  const [mode, setMode] = useState<"review" | EntryStatus | "">("review");
  const [onlyFlagged, setOnlyFlagged] = useState(false);
  const list = useLoad(
    dapi ? () => dapi.entries(mode === "review" ? { review: true, flagged: onlyFlagged } : { ...(mode ? { status: mode } : {}), flagged: onlyFlagged }) : null,
    [dapi, mode, onlyFlagged],
  );
  const communeName = useCommuneNames((list.data ?? []).map((e) => e.commune_code));
  const [open, setOpen] = useState<string | null>(null);
  if (!dapi) return null;
  return (
    <div className="page">
      <PageHeader title="Rà soát nhật ký">
        <p className="mute">
          Lần ghi nhật ký BM-08, BM-09, BM-19 của hộ mô hình. Chủ nhiệm rà soát chương trình mình phụ trách; lần ghi được xác thực mới dùng cho báo cáo và biên
          bản nghiệm thu (Mẫu 05).
        </p>
      </PageHeader>
      <div className="row">
        <label>
          Hiển thị{" "}
          <select value={mode} onChange={(e) => (setMode(e.target.value as typeof mode), setOpen(null))}>
            <option value="review">Chờ tôi rà soát</option>
            <option value="">Tất cả</option>
            <option value="submitted">Đã gửi</option>
            <option value="returned">Yêu cầu bổ sung</option>
            <option value="verified">Đã xác thực</option>
          </select>
        </label>
        <label>
          <input type="checkbox" checked={onlyFlagged} onChange={(e) => setOnlyFlagged(e.target.checked)} /> Chỉ lần ghi có cảnh báo
        </label>
      </div>
      <LoadState s={list}>
        {(items) => (
          <table className="table">
            <thead>
              <tr>
                <th>Ngày ghi</th>
                <th>Mẫu</th>
                <th>Điểm / hộ</th>
                <th>Xã</th>
                <th>Người ghi</th>
                <th>Cảnh báo</th>
                <th>Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {items.length ? (
                items.map((e) => (
                  <tr key={e.ref} className={open === e.ref ? "selected" : undefined}>
                    <td>
                      <button className="link" onClick={() => setOpen(open === e.ref ? null : e.ref)}>
                        {dmy(e.entry_date)}
                      </button>
                    </td>
                    <td>
                      {e.form} · {KIND_LABEL[e.entry_kind]}
                    </td>
                    <td>
                      {e.site_name}
                      {e.owner_label ? ` · ${e.owner_label}` : ""}
                    </td>
                    <td>{communeName(e.commune_code)}</td>
                    <td>{e.author_display ?? "—"}</td>
                    <td>{flagged(e) ? `${e.checks.filter((c) => c.level === "warn").length} cảnh báo` : "—"}</td>
                    <td>
                      <StatusBadge status={e.status} />
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={7} className="empty">
                    {mode === "review" ? "Không có lần ghi chờ rà soát." : "Không có lần ghi."}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </LoadState>
      {open ? <EntryReview key={open} dapi={dapi} entryRef={open} onChanged={list.reload} /> : null}
    </div>
  );
}

function EntryReview({ dapi, entryRef, onChanged }: { dapi: CntsDiaryApi; entryRef: string; onChanged: () => void }) {
  const d = useLoad(() => dapi.entry(entryRef), [dapi, entryRef]);
  const act = useAction();
  const [giving, setGiving] = useState(false);
  return (
    <LoadState s={d}>
      {(e) => (
        <section className="card sec" aria-label="Chi tiết lần ghi">
          <h3>
            {FORM_LABEL[e.form]} · {KIND_LABEL[e.entry_kind]} · {dmy(e.entry_date)} <StatusBadge status={e.status} />
          </h3>
          <p className="mute">
            {e.project_code} · {e.site_name}
            {e.owner_label ? ` · ${e.owner_label}` : ""}
            {e.breed ? ` · ${e.breed}` : ""} · ghi bởi {e.author_display ?? "—"} lúc {new Date(e.captured_at).toLocaleString("vi-VN")}
          </p>
          {e.status_reason ? <p>Lý do: {e.status_reason}</p> : null}
          {act.view}
          <h4>Số liệu</h4>
          <MetricsTable form={e.form} entry={e} previous={e.previous?.metrics} />
          <TablesView form={e.form} tables={e.tables} />
          <h4>Kiểm tra tự động</h4>
          <Checks checks={e.checks} />
          <h4>Vị trí</h4>
          <LocationInfo e={e} />
          <h4>Tăng trọng theo lần cân</h4>
          <WeightChart points={e.weights} />
          <h4>Ảnh</h4>
          <Photos dapi={dapi} photos={e.photos} />
          {e.can_review ? (
            giving ? (
              <ReasonBox
                label="Gửi yêu cầu bổ sung"
                busy={act.busy}
                onCancel={() => setGiving(false)}
                onConfirm={async (reason) => {
                  if (await act.run(() => dapi.giveBack(e.ref, e.version, reason), "Đã yêu cầu bổ sung")) {
                    setGiving(false);
                    d.reload();
                    onChanged();
                  }
                }}
              />
            ) : (
              <div className="row">
                <button
                  className="btn btn-primary"
                  disabled={act.busy}
                  onClick={async () => {
                    if (await act.run(() => dapi.verify(e.ref, e.version), "Đã xác thực lần ghi")) {
                      d.reload();
                      onChanged();
                    }
                  }}
                >
                  Xác thực
                </button>
                <button className="btn" onClick={() => setGiving(true)}>
                  Yêu cầu bổ sung
                </button>
              </div>
            )
          ) : e.status === "submitted" ? (
            <p className="mute">Chỉ chủ nhiệm của dự án hoặc lãnh đạo phòng xác thực; người ghi không tự xác thực.</p>
          ) : null}
          <details>
            <summary>Lịch sử ({e.versions.length} phiên bản)</summary>
            <ul className="list-plain">
              {e.versions.map((v) => (
                <li key={v.version}>
                  #{v.version} · {STATUS_EVENT[v.event]} · {v.updated_by_display ?? v.updated_by} · {new Date(v.updated_at).toLocaleString("vi-VN")}
                  {v.status_reason ? ` · ${v.status_reason}` : ""}
                </li>
              ))}
            </ul>
          </details>
        </section>
      )}
    </LoadState>
  );
}

const STATUS_EVENT: Record<string, string> = {
  created: "Tạo",
  updated: "Sửa",
  submitted: "Gửi",
  returned: "Yêu cầu bổ sung",
  verified: "Xác thực",
  corrected: "Điều chỉnh",
};
