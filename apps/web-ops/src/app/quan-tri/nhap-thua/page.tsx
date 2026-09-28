"use client";

import { useState, type ChangeEvent, type FormEvent } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { PlotImportResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatBytes, readUpload, type UploadFile } from "@/lib/files";
import { useAsync } from "@/lib/useAsync";

const STATUS = {
  accepted: { text: "Đã tạo thửa", cls: "c-ok" },
  flagged: { text: "Đã tạo — chồng lấn chờ xem xét", cls: "c-warn" },
  duplicate: { text: "Trùng (đã nhập trước)", cls: "c-mute" },
  rejected: { text: "Bị loại", cls: "c-bad" },
} as const;

const FORMAT: Record<string, "geojson" | "kml" | "shapefile" | undefined> = { geojson: "geojson", kml: "kml", zip: "shapefile" };

/** Nhập ranh giới thửa GeoJSON/KML/Shapefile (.zip) — Admin API POST /v1/geo/plots/import (geo_admin, MFA). */
export default function PlotImportPage() {
  const { adminApi, api } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [province, setProvince] = useState("");
  const [commune, setCommune] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<PlotImportResult | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const provinces = useAsync(() => listChildUnits(api, null), [api]);
  const communes = useAsync(() => (province ? listChildUnits(api, province) : Promise.resolve([])), [api, province]);
  const format = file ? FORMAT[file.kind] : undefined;

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setIdemKey(newIdempotencyKey());
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !format) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/geo/plots/import", {
          params: {
            header: { "Idempotency-Key": idemKey },
            query: { format, file_name: file.name, commune_code: commune || undefined, confirm_overlap: confirm || undefined },
          },
          // Gửi nguyên văn nội dung tệp (JSON/KML là chuỗi, Shapefile .zip là byte).
          body: file.body as unknown as Record<string, never>,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setResult(res);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHead title="Nhập ranh giới thửa" subtitle="GeoJSON, KML hoặc Shapefile (.zip gồm .shp, .dbf, .prj) — tối đa 200 đối tượng, 10 MB, hệ toạ độ WGS84" />
      <MfaNotice />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Mỗi vùng thành một thửa, cùng quy tắc như vẽ tay: chồng lấn HTX khác luôn bị loại; chồng lấn cùng HTX/thửa nháp chỉ nhận khi chọn “Nhận cả
          đối tượng chồng lấn” (mở xung đột để xem xét). Hệ thống <b style={{ display: "inline" }}>không tự chuyển VN-2000</b> — chuyển sang EPSG:4326
          trước khi tải lên. Nhập lại cùng tệp không tạo trùng.
        </span>
      </div>
      <form className="card sec stack" onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor="pl-file">Tệp ranh giới</label>
          <input id="pl-file" type="file" accept=".geojson,.json,.kml,.zip,application/geo+json,application/vnd.google-earth.kml+xml,application/zip" onChange={(e) => void onFile(e)} />
          {file ? (
            <span className="small">
              {file.name} · {formatBytes(file.size)} · {format ? `định dạng ${format === "shapefile" ? "Shapefile (.zip)" : format.toUpperCase()}` : "không nhận ra định dạng"}
            </span>
          ) : null}
          {file && !format ? <span className="err">Chỉ nhận GeoJSON, KML hoặc Shapefile đóng gói .zip (không tải .shp rời).</span> : null}
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="pl-prov">Tỉnh (để chọn xã mặc định)</label>
            <select
              id="pl-prov"
              className="select"
              value={province}
              onChange={(e) => {
                setProvince(e.target.value);
                setCommune("");
                setIdemKey(newIdempotencyKey());
              }}
            >
              <option value="">—</option>
              {(provinces.data ?? []).map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="pl-commune">Xã mặc định (khi đối tượng không có MA_XA)</label>
            <select
              id="pl-commune"
              className="select"
              value={commune}
              onChange={(e) => {
                setCommune(e.target.value);
                setIdemKey(newIdempotencyKey());
              }}
            >
              <option value="">—</option>
              {(communes.data ?? []).map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <label className="row small">
          <input
            type="checkbox"
            checked={confirm}
            onChange={(e) => {
              setConfirm(e.target.checked);
              setIdemKey(newIdempotencyKey());
            }}
          />
          Nhận cả đối tượng chồng lấn cùng HTX/thửa nháp (mở xung đột để xem xét)
        </label>
        <div>
          <button type="submit" className="btn btn-p" disabled={!file || !format || busy}>
            {busy ? "Đang nhập…" : "Nhập ranh giới"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </form>
      {result ? (
        <section className="card sec" style={{ marginTop: 16 }} aria-labelledby="pl-res">
          <div className="sec-h">
            <h2 id="pl-res">Kết quả theo đối tượng ({result.format})</h2>
            <span className="small">
              <span className="chip c-ok">{formatInt(result.accepted)} đã tạo</span> <span className="chip c-warn">{formatInt(result.flagged)} chờ xem xét</span>{" "}
              <span className="chip c-mute">{formatInt(result.duplicate)} trùng</span> <span className="chip c-bad">{formatInt(result.rejected)} bị loại</span>
            </span>
          </div>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Kết quả nhập ranh giới</caption>
              <thead>
                <tr>
                  <th scope="col" className="num">#</th>
                  <th scope="col">Tên</th>
                  <th scope="col">Kết quả</th>
                  <th scope="col">Mã thửa</th>
                  <th scope="col" className="num">Diện tích (ha)</th>
                  <th scope="col">Lý do</th>
                </tr>
              </thead>
              <tbody>
                {result.features.map((f) => (
                  <tr key={f.index}>
                    <td className="num">{f.index + 1}</td>
                    <td>{f.name ?? "—"}</td>
                    <td>
                      <span className={`chip ${STATUS[f.status].cls}`}>{STATUS[f.status].text}</span>
                    </td>
                    <td>{f.plot_code ?? "—"}</td>
                    <td className="num">{f.area_ha ? formatDecimal(f.area_ha, 2) : "—"}</td>
                    <td className="small">
                      {f.reason ?? "—"}
                      {f.code ? (
                        <div className="mute">
                          <code>{f.code}</code>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
    </>
  );
}
