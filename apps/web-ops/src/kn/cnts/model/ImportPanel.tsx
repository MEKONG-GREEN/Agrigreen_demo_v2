"use client";

// Nạp Excel điểm trình diễn, hộ tham gia của dự án (W11.53c — BRD CNTS v3.7 FN-19 luồng phụ): tải tệp mẫu → chọn tệp →
// Xem trước (máy chủ kiểm, không ghi) → Ghi (cả tệp hoặc không ghi gì). Lỗi theo sheet/dòng/cột. Họ tên, SĐT chủ hộ chỉ
// hiện dạng che bớt (máy chủ không trả lại). Tệp được chụp một lần lúc chọn: xem trước và ghi gửi đúng cùng byte; bấm Ghi
// lại sau mất phản hồi giữ cùng Idempotency-Key (useAction) — máy chủ phát lại, không tạo bản sao.
import { useRef, useState } from "react";
import { DataTable, StatusPill } from "@/components/patterns/list";
import { useAction } from "@/kn/cycle/common";
import { saveBlob } from "@/kn/cnts/knqg/api";
import { ImportRejected, dmy, importFileError, issueWhere, snapshotFile, type CntsModelApi, type ImportResult } from "./api";

export function ImportPanel({ mapi, projectRef, projectCode, onDone }: { mapi: CntsModelApi; projectRef: string; projectCode: string; onDone: (committed: boolean) => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [fileErr, setFileErr] = useState<string | null>(null);
  // #176 review P2: kết quả xem trước gắn đúng snapshot đã gửi xem trước; đổi tệp → bỏ. Phản hồi của snapshot cũ trả muộn
  // (sau khi đã chọn tệp khác) bị bỏ qua, và nút Ghi chỉ gửi đúng snapshot đã xem trước hợp lệ.
  const [preview, setPreview] = useState<{ file: File; result: ImportResult } | null>(null);
  const current = useRef<File | null>(null);
  const [picking, setPicking] = useState(false);
  const act = useAction();
  const pick = async (f: File | undefined) => {
    setPreview(null);
    setFile(null);
    current.current = null;
    const err = importFileError(f ?? null);
    setFileErr(err);
    if (!f || err) return;
    setPicking(true);
    try {
      const snap = await snapshotFile(f);
      current.current = snap;
      setFile(snap);
    } finally {
      setPicking(false);
    }
  };
  const template = async () => {
    const b = await act.run(() => mapi.importTemplate(projectRef));
    if (b) saveBlob(b, `mau-nap-diem-ho-${projectCode.toLowerCase()}.xlsx`);
  };
  const check = async () => {
    const snap = file;
    if (!snap) return setFileErr(importFileError(null));
    const r = await act.run(() => mapi.previewImport(projectRef, snap));
    if (r && current.current === snap) setPreview({ file: snap, result: r });
  };
  const ready = !!preview && preview.file === file && preview.result.valid && !preview.result.committed;
  const commit = async () => {
    const snap = preview?.file;
    if (!ready || !snap) return;
    const r = await act.run(
      () => mapi.commitImport(projectRef, snap),
      (x) => `Đã nạp ${x.site_count} điểm, ${x.household_count} hộ${x.replayed ? " (lần ghi trước — không tạo bản sao)" : ""}.`,
    );
    if (r) {
      if (current.current === snap) setPreview({ file: snap, result: r });
      onDone(true);
    }
  };
  const shown = preview?.file === file ? preview?.result : undefined;
  const issues = act.problem instanceof ImportRejected ? act.problem.issues : (shown?.issues ?? []);
  return (
    <section className="card sec" aria-label="Nạp Excel điểm, hộ">
      <h3>Nạp Excel điểm trình diễn, hộ tham gia</h3>
      <p className="mute">
        Dùng tệp mẫu của dự án (sheet &quot;Điểm trình diễn&quot;, &quot;Hộ tham gia&quot;, hướng dẫn và mã tra cứu). Tối đa 2 MB, 200 điểm, 2000 hộ. Hệ thống chỉ ghi khi tệp
        không còn lỗi — ghi cả tệp hoặc không ghi gì. Họ tên, SĐT chủ hộ lưu ở kho định danh.
      </p>
      {act.view}
      <div className="row" style={{ gap: 8, flexWrap: "wrap", alignItems: "end" }}>
        <button className="btn" disabled={act.busy} onClick={() => void template()}>
          Tải tệp mẫu
        </button>
        <div className="field">
          <label>
            Tệp Excel nạp{" "}
            <input
              type="file"
              accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              aria-invalid={fileErr ? true : undefined}
              disabled={act.busy || picking}
              onChange={(e) => {
                void pick(e.target.files?.[0]);
                e.target.value = "";
              }}
            />
          </label>
          {fileErr ? (
            <span className="err" role="alert">
              {fileErr}
            </span>
          ) : file ? (
            <small className="mute">{file.name}</small>
          ) : null}
        </div>
        <button className="btn" disabled={act.busy || picking || !file} onClick={() => void check()}>
          Xem trước
        </button>
        <button className="btn btn-primary" disabled={act.busy || picking || !ready} onClick={() => void commit()}>
          Ghi vào dự án
        </button>
        <button className="btn" onClick={() => onDone(false)}>
          Đóng
        </button>
      </div>
      {issues.length ? (
        <>
          <h4>
            <StatusPill tone="bad">{issues.length} lỗi</StatusPill> Sửa trong tệp rồi chọn lại tệp
          </h4>
          <DataTable
            caption="Lỗi của tệp nạp"
            rows={issues}
            rowKey={(i) => `${i.sheet}-${i.row}-${i.column}-${i.message}`}
            columns={[
              { key: "where", header: "Vị trí", cell: (i) => issueWhere(i) },
              { key: "msg", header: "Lý do", cell: (i) => i.message },
            ]}
          />
        </>
      ) : null}
      {shown ? (
        <>
          <p>
            {shown.committed ? <StatusPill tone="ok">Đã ghi</StatusPill> : shown.valid ? <StatusPill tone="info">Ghi được</StatusPill> : null} {shown.site_count} điểm mới,{" "}
            {shown.household_count} hộ.
          </p>
          {shown.sites.length ? (
            <DataTable
              caption="Điểm trình diễn trong tệp"
              rows={shown.sites}
              rowKey={(x) => String(x.row)}
              columns={[
                { key: "row", header: "Dòng", cell: (x) => x.row },
                { key: "code", header: "Mã điểm", cell: (x) => x.code },
                { key: "name", header: "Tên điểm", cell: (x) => x.name },
                { key: "commune", header: "Mã xã", cell: (x) => x.commune_code },
                { key: "model", header: "Loại mô hình", cell: (x) => x.model_type_name ?? x.model_type_code },
                { key: "scale", header: "Quy mô", cell: (x) => (x.scale ? `${x.scale} ${x.scale_unit ?? ""}` : "—") },
                { key: "officer", header: "Cán bộ theo dõi", cell: (x) => x.officer_display ?? "—" },
              ]}
            />
          ) : null}
          {shown.households.length ? (
            <DataTable
              caption="Hộ tham gia trong tệp"
              rows={shown.households}
              rowKey={(x) => String(x.row)}
              columns={[
                { key: "row", header: "Dòng", cell: (x) => x.row },
                { key: "site", header: "Điểm", cell: (x) => x.site_name },
                { key: "owner", header: "Chủ hộ", cell: (x) => `${x.owner_label} · ${x.owner_phone_masked}` },
                { key: "scale", header: "Quy mô", cell: (x) => (x.scale ? `${x.scale} ${x.scale_unit ?? ""}` : "—") },
                { key: "keeper", header: "Ghi nhật ký", cell: (x) => (x.keeper_kind === "self" ? "Hộ tự ghi" : "Cán bộ theo dõi") },
                { key: "joined", header: "Ngày tham gia", cell: (x) => dmy(x.joined_on) },
              ]}
            />
          ) : null}
        </>
      ) : null}
    </section>
  );
}
