"use client";

// Khối "Check-in, check-out tại địa điểm" của chi tiết lịch (BRD CNTS v4.0 FN-12 CV-07/CV-08, UR-TINH-18): thời điểm,
// khoảng cách, sai số GPS của check-in và check-out, thời gian ở địa điểm (BR-07), "Đồng bộ trễ" (BR-08), ảnh tại chỗ.
// Check-in cũ trước v4.0 không ảnh: ghi "Dữ liệu cũ — không ảnh", không bắt nhập bù.
import { useEffect, useState } from "react";
import type { ApiProblem } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import { formatDateTime } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";
import { stayLabel, type WsCheckin } from "./model";

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
