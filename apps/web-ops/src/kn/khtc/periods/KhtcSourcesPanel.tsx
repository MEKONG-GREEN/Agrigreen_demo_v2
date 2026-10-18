"use client";

// Số liệu nguồn module của một kỳ KHTC (W11.64 — BRD KHTC v2.2 BR-DL-02…04, BR-DL-09): nút "Cập nhật số liệu" (chỉ cán bộ
// KHTC phụ trách kỳ — ⚑3), mốc dữ liệu từng nguồn, cảnh báo "Nguồn đã thay đổi" (chỉ cảnh báo — số đã chụp không tự đổi),
// số của các hạng mục Kế hoạch năm có nguồn module, lịch sử cũ → mới.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import {
  CHANGE_LABEL,
  DEPT_NAME,
  FIELD_LABEL,
  baseRefText,
  createKhtcSourceApi,
  valueBadges,
  valueText,
  vnNum,
  type KhtcSourceStatus,
} from "./sources";

export function KhtcSourcesPanel({ periodId }: { periodId: string }) {
  const { api } = useAuth();
  const sapi = useMemo(() => (api ? createKhtcSourceApi(api) : null), [api]);
  const status = useLoad(sapi ? () => sapi.status(periodId) : null, [sapi, periodId]);
  const [showHistory, setShowHistory] = useState(false);
  const history = useLoad(sapi && showHistory ? () => sapi.history(periodId) : null, [sapi, periodId, showHistory]);
  const act = useAction();

  return (
    <section aria-label="Số liệu nguồn module">
      <h3>Số liệu nguồn module</h3>
      {act.view}
      <LoadState s={status}>
        {(s: KhtcSourceStatus) => (
          <>
            <div className="row">
              {s.can_refresh ? (
                <button
                  className="btn btn-primary"
                  disabled={act.busy}
                  onClick={async () => {
                    const r = await act.run(() => sapi!.refresh(periodId), (x) => `Đã cập nhật số liệu (lần ${x.snapshot?.no ?? "—"}).`);
                    if (r) {
                      status.reload();
                      if (showHistory) history.reload();
                    }
                  }}
                >
                  Cập nhật số liệu
                </button>
              ) : null}
              <span className="mute">
                Cán bộ phụ trách: {s.owner_name || s.owner_ref.slice(0, 8)}
                {s.snapshot
                  ? ` · Lần cập nhật ${s.snapshot.no} lúc ${dt(s.snapshot.taken_at)}${s.snapshot.actor_name ? ` bởi ${s.snapshot.actor_name}` : ""}`
                  : " · Chưa cập nhật số liệu"}
                {s.terminal ? " · Kỳ đã đóng: giữ số đã chụp" : ""}
              </span>
            </div>
            {s.source_changed ? (
              <div className="alert alert-warn" role="alert" aria-label="Nguồn đã thay đổi">
                <span>
                  <strong>Nguồn đã thay đổi</strong> sau lần cập nhật {s.snapshot?.no}:{" "}
                  {s.changes
                    .map((c) => `${DEPT_NAME[c.department] ?? c.department} ${c.form_code} ${c.native_key} (${CHANGE_LABEL[c.kind]})`)
                    .join("; ")}
                  . Số trên báo cáo vẫn là số đã chụp —{" "}
                  {s.can_refresh ? "bấm “Cập nhật số liệu” để lấy số mới." : "cán bộ phụ trách kỳ cập nhật để lấy số mới."}
                </span>
              </div>
            ) : null}
            {s.live_error ? <p className="mute">Không đọc được số liệu nguồn hiện tại — chưa xác định nguồn có đổi.</p> : null}
            <h4>Mốc dữ liệu{s.live ? " (nguồn hiện tại — chưa cập nhật)" : ""}</h4>
            <table className="dt" aria-label="Mốc dữ liệu">
              <thead>
                <tr>
                  <th>Phòng</th>
                  <th>Biểu</th>
                  <th>Kỳ nguồn</th>
                  <th>Chốt lúc</th>
                  <th>Lần chốt</th>
                  <th>Trạng thái</th>
                </tr>
              </thead>
              <tbody>
                {s.as_of.map((a) => (
                  <tr key={`${a.department}:${a.form_code ?? ""}`}>
                    <td>{DEPT_NAME[a.department] ?? a.department}</td>
                    <td>{a.form_code ?? "—"}</td>
                    <td>{a.native_keys.join(", ") || "—"}</td>
                    <td>{dt(a.closed_at)}</td>
                    <td>{a.snapshot_no ?? "—"}</td>
                    <td>
                      <StatusPill tone={a.unavailable ? "warn" : a.closed ? "ok" : "info"}>
                        {a.unavailable ? "Chưa công bố" : a.missing ? "Không phát sinh" : a.closed ? "Đã chốt" : "Tạm tính"}
                      </StatusPill>
                      {a.fallback ? " ngày duyệt gần nhất" : ""}
                      {a.confirmation === "unconfirmed" ? " · có HTX chưa xác nhận" : ""}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {s.values.length ? (
              <>
                <h4>Hạng mục lấy từ module</h4>
                <table className="dt" aria-label="Hạng mục lấy từ module">
                  <thead>
                    <tr>
                      <th>Hạng mục</th>
                      <th>Khóa nguồn</th>
                      <th>Số</th>
                      <th>Gốc</th>
                      <th>Trạng thái</th>
                    </tr>
                  </thead>
                  <tbody>
                    {s.values.map((v) => (
                      <tr key={v.source_key}>
                        <td>{v.entries.map((e) => e.name || e.code).join("; ") || "—"}</td>
                        <td>
                          <code>{v.source_key}</code>
                        </td>
                        <td>
                          {valueText(v)}
                          {v.mode === "price" && v.price_days.length
                            ? ` (${v.price_days.map((d) => `${d.day}: ${vnNum(d.min)}–${vnNum(d.max)}`).join("; ")})`
                            : ""}
                        </td>
                        <td>{v.base != null ? `${vnNum(v.base)} — ${baseRefText(v.base_ref)}` : "—"}</td>
                        <td>{valueBadges(v).join(" · ")}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            ) : s.snapshot ? (
              <p className="mute">Kế hoạch năm chưa có hạng mục lấy từ module.</p>
            ) : null}
            <button className="btn" onClick={() => setShowHistory((x) => !x)}>
              {showHistory ? "Ẩn lịch sử cập nhật" : "Lịch sử cập nhật số liệu"}
            </button>
            {showHistory ? (
              <LoadState s={history}>
                {(items) =>
                  items.length ? (
                    <ul aria-label="Lịch sử cập nhật số liệu">
                      {items.map((h) => (
                        <li key={h.snapshot.id}>
                          Lần {h.snapshot.no} — {dt(h.snapshot.taken_at)} — {h.snapshot.actor_name || h.snapshot.actor_ref.slice(0, 8)}
                          {h.changes.length ? (
                            <ul>
                              {h.changes.map((c, i) => (
                                <li key={i}>
                                  {c.entries.map((e) => e.name || e.code).join("; ") || c.source_key} — {FIELD_LABEL[c.field] ?? c.field}:{" "}
                                  {vnNum(c.old)} → {vnNum(c.new)}
                                </li>
                              ))}
                            </ul>
                          ) : (
                            <span className="mute"> · không đổi số</span>
                          )}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="mute">Chưa có lần cập nhật nào.</p>
                  )
                }
              </LoadState>
            ) : null}
          </>
        )}
      </LoadState>
      <p className="mute">
        Số lấy từ module là số đã chốt của phòng (lũy kế của nguồn; giá min/max toàn tỉnh của bảng giá đã duyệt). Sai số liệu → phòng
        sửa và chốt lại ở module của phòng, rồi cán bộ phụ trách bấm “Cập nhật số liệu” (BR-DL-09).
      </p>
    </section>
  );
}
