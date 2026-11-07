"use client";

// Khối "Check-in, check-out tại địa điểm" của chi tiết lịch (BRD CNTS v4.0 FN-12 CV-07/CV-08, UR-TINH-18): thời điểm,
// khoảng cách, sai số GPS của check-in và check-out, thời gian ở địa điểm (BR-07), "Đồng bộ trễ" (BR-08), ảnh tại chỗ.
// Check-in cũ trước v4.0 không ảnh: ghi "Dữ liệu cũ — không ảnh", không bắt nhập bù.
// CNTS-V4-CHECKIN-MONITOR (#250, FN-12 BR-07/BR-08, Hình 49): nhãn "Chưa check-out" từ 23:59 ngày công tác (máy chủ tính
// theo giờ Việt Nam), màn theo dõi check-in, check-out của cán bộ xã (<VisitMonitor>).
import { useEffect, useMemo, useState } from "react";
import type { ApiProblem } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import { formatDateTime } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";
import { SESSION_LABEL, stayLabel, type WsCheckin, type WsListItem } from "./model";
import { dayColumnLabel } from "./week";

type Kind = "checkin" | "checkout";

const dist = (d: number | null, far: boolean) => (d === null ? "Chưa có tọa độ địa điểm" : `${Math.round(d)} m${far ? " · xa" : ""}`);
const acc = (a: number | null, weak: boolean) => (a === null ? "—" : `${Math.round(a)} m${weak ? " · yếu" : ""}`);

export function CheckinVisits({
  checkins,
  itemLabel,
  loadPhoto,
  missed,
}: {
  checkins: WsCheckin[];
  itemLabel: (itemId: string) => string;
  loadPhoto: (itemId: string, kind: Kind) => Promise<Blob>;
  missed?: number;
}) {
  const [preview, setPreview] = useState<{ title: string; url: string } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  useEffect(() => () => (preview ? URL.revokeObjectURL(preview.url) : undefined), [preview]);

  const show = async (c: WsCheckin, kind: Kind) => {
    setProblem(null);
    try {
      const blob = await loadPhoto(c.item_id, kind);
      setPreview({ title: `Ảnh ${kind === "checkin" ? "check-in" : "check-out"} — ${itemLabel(c.item_id)}`, url: URL.createObjectURL(blob) });
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  return (
    <section className="lv-block" aria-label="Check-in, check-out">
      <h2>Check-in, check-out tại địa điểm</h2>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {checkins.length === 0 ? (
        <p className="mute">Chưa có check-in.</p>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Mục</th>
              <th>Check-in</th>
              <th>Cách địa điểm</th>
              <th>Sai số GPS</th>
              <th>Check-out</th>
              <th>Thời gian ở địa điểm</th>
              <th>Ảnh</th>
              <th>Ghi chú</th>
            </tr>
          </thead>
          <tbody>
            {checkins.map((c) => {
              const o = c.checkout;
              const notes = [
                c.mock_location || o?.mock_location ? "vị trí giả lập" : "",
                (c.point_stored && !c.location) || (o?.point_stored && !o.location) ? "tọa độ đã hết hạn lưu" : "",
              ].filter(Boolean);
              return (
                <tr key={c.id}>
                  <td>{itemLabel(c.item_id)}</td>
                  <td>{formatDateTime(c.captured_at)}</td>
                  <td>
                    {dist(c.distance_m, c.far)}
                    {o ? <div>Check-out: {dist(o.distance_m, o.far)}</div> : null}
                  </td>
                  <td>
                    {acc(c.accuracy_m, c.weak_accuracy)}
                    {o ? <div>Check-out: {acc(o.accuracy_m, o.weak_accuracy)}</div> : null}
                  </td>
                  <td>
                    {o ? (
                      <>
                        {formatDateTime(o.captured_at)}
                        {o.synced_late ? <div className="ws-flag f-conflict">Đồng bộ trễ · {formatDateTime(o.synced_at)}</div> : null}
                      </>
                    ) : c.no_checkout ? (
                      <NoCheckoutFlag />
                    ) : (
                      <span className="mute">Chưa check-out</span>
                    )}
                  </td>
                  <td>{o ? stayLabel(o.stay_minutes) : "—"}</td>
                  <td>
                    {c.photo ? (
                      <button type="button" className="link" onClick={() => show(c, "checkin")}>
                        Ảnh check-in
                      </button>
                    ) : c.legacy_no_photo ? (
                      <span className="mute">Dữ liệu cũ — không ảnh</span>
                    ) : null}
                    {o ? (
                      <div>
                        <button type="button" className="link" onClick={() => show(c, "checkout")}>
                          Ảnh check-out
                        </button>
                      </div>
                    ) : null}
                  </td>
                  <td>{notes.join(", ") || "—"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {missed ? <p className="mute">{missed} mục công tác ngoài cơ quan đã qua ngày chưa check-in.</p> : null}
      {preview ? (
        <figure>
          {/* eslint-disable-next-line @next/next/no-img-element -- ảnh blob đã tải có xác thực, next/image không áp dụng */}
          <img src={preview.url} alt={preview.title} style={{ maxWidth: "100%", maxHeight: 360 }} />
          <figcaption>
            {preview.title}{" "}
            <button type="button" className="link" onClick={() => setPreview(null)}>
              Đóng ảnh
            </button>
          </figcaption>
        </figure>
      ) : null}
    </section>
  );
}

/** Nhãn "Chưa check-out" (BR-07): đã check-in, tới 23:59 ngày công tác chưa check-out. */
function NoCheckoutFlag() {
  return (
    <span className="ws-flag f-conflict" title="Đã check-in, đến 23:59 cùng ngày chưa check-out">
      Chưa check-out
    </span>
  );
}

/** Một mục công tác của cán bộ xã trên màn theo dõi. */
export interface VisitRow {
  scheduleId: string;
  owner: string;
  communeCode: string;
  itemId: string;
  day: string;
  label: string;
  checkin: WsCheckin | null;
}

/** Dòng màn theo dõi: mọi mục của lịch Cấp Xã (phòng ban dùng check-in), sắp theo ngày rồi cán bộ. */
export function visitRows(schedules: WsListItem[]): VisitRow[] {
  const rows: VisitRow[] = [];
  for (const s of schedules) {
    if (s.owner_level !== "commune" || !s.checkin_enabled) continue;
    const byItem = new Map((s.checkins ?? []).map((c) => [c.item_id, c]));
    for (const it of s.items) {
      rows.push({
        scheduleId: s.id,
        owner: s.owner_display ?? "—",
        communeCode: it.commune_code ?? s.commune_codes[0] ?? "",
        itemId: it.id,
        day: it.day,
        label: `${SESSION_LABEL[it.session]} · ${it.content}`,
        checkin: byItem.get(it.id) ?? null,
      });
    }
  }
  return rows.sort((a, b) => a.day.localeCompare(b.day) || a.owner.localeCompare(b.owner, "vi"));
}

/**
 * Màn "Theo dõi check-in, check-out của cán bộ xã" (BRD CNTS v4.0 Hình 49, UR-TINH-18): check-in, check-out, thời gian ở
 * địa điểm, ảnh; mục chưa check-out hiện nhãn "Chưa check-out"; lọc nhanh các mục Chưa check-out.
 */
export function VisitMonitor({
  schedules,
  communeName,
  loadPhoto,
}: {
  schedules: WsListItem[];
  communeName: (code: string) => string;
  loadPhoto: (scheduleId: string, itemId: string, kind: Kind) => Promise<Blob>;
}) {
  const [onlyOpen, setOnlyOpen] = useState(false);
  const [preview, setPreview] = useState<{ title: string; url: string } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  useEffect(() => () => (preview ? URL.revokeObjectURL(preview.url) : undefined), [preview]);
  const all = useMemo(() => visitRows(schedules), [schedules]);
  const open = all.filter((r) => r.checkin?.no_checkout).length;
  const rows = onlyOpen ? all.filter((r) => r.checkin?.no_checkout) : all;

  const show = async (r: VisitRow, kind: Kind) => {
    setProblem(null);
    try {
      const blob = await loadPhoto(r.scheduleId, r.itemId, kind);
      setPreview({ title: `Ảnh ${kind === "checkin" ? "check-in" : "check-out"} — ${r.owner}, ${r.label}`, url: URL.createObjectURL(blob) });
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  return (
    <section aria-label="Theo dõi check-in, check-out">
      <p className="small" style={{ marginBottom: 8 }}>
        <label>
          <input type="checkbox" checked={onlyOpen} onChange={(e) => setOnlyOpen(e.target.checked)} /> Chỉ mục Chưa check-out ({open})
        </label>
      </p>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div className="card" style={{ padding: 0, overflow: "auto" }}>
        <table className="table">
          <caption className="sr-only">Check-in, check-out của cán bộ xã</caption>
          <thead>
            <tr>
              <th>Ngày</th>
              <th>Cán bộ</th>
              <th>Xã/phường</th>
              <th>Mục công tác</th>
              <th>Check-in</th>
              <th>Check-out</th>
              <th>Thời gian ở địa điểm</th>
              <th>Ảnh</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={8} className="empty">
                  {onlyOpen ? "Không có mục Chưa check-out." : "Không có mục công tác của cán bộ xã trong tuần này."}
                </td>
              </tr>
            ) : (
              rows.map((r) => {
                const c = r.checkin;
                const o = c?.checkout ?? null;
                return (
                  <tr key={r.itemId} data-testid="visit-row">
                    <td>{dayColumnLabel(r.day)}</td>
                    <td>{r.owner}</td>
                    <td>{r.communeCode ? communeName(r.communeCode) : "—"}</td>
                    <td>{r.label}</td>
                    <td>
                      {c ? (
                        <>
                          {formatDateTime(c.captured_at)}
                          {c.far ? <div className="ws-flag f-conflict">Xa địa điểm</div> : null}
                        </>
                      ) : (
                        <span className="mute">Chưa check-in</span>
                      )}
                    </td>
                    <td>
                      {o ? (
                        <>
                          {formatDateTime(o.captured_at)}
                          {o.far ? <div className="ws-flag f-conflict">Xa địa điểm</div> : null}
                          {o.synced_late ? <div className="ws-flag f-conflict">Đồng bộ trễ · {formatDateTime(o.synced_at)}</div> : null}
                        </>
                      ) : c?.no_checkout ? (
                        <NoCheckoutFlag />
                      ) : c ? (
                        <span className="mute">Chưa check-out</span>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td>{o ? stayLabel(o.stay_minutes) : "—"}</td>
                    <td>
                      {c?.photo ? (
                        <button type="button" className="link" onClick={() => void show(r, "checkin")}>
                          Ảnh check-in
                        </button>
                      ) : c?.legacy_no_photo ? (
                        <span className="mute">Dữ liệu cũ — không ảnh</span>
                      ) : null}
                      {o ? (
                        <div>
                          <button type="button" className="link" onClick={() => void show(r, "checkout")}>
                            Ảnh check-out
                          </button>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
      {preview ? (
        <figure>
          {/* eslint-disable-next-line @next/next/no-img-element -- ảnh blob đã tải có xác thực, next/image không áp dụng */}
          <img src={preview.url} alt={preview.title} style={{ maxWidth: "100%", maxHeight: 360 }} />
          <figcaption>
            {preview.title}{" "}
            <button type="button" className="link" onClick={() => setPreview(null)}>
              Đóng ảnh
            </button>
          </figcaption>
        </figure>
      ) : null}
    </section>
  );
}
