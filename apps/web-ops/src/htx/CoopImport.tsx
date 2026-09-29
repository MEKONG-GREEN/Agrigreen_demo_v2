"use client";

// Nhập hồ sơ HTX hàng loạt từ tệp Excel mẫu (HTX US-HTX-02, Admin API — registry_admin, kn_province_admin,
// platform_admin; MFA). Người đại diện + SĐT trong tệp đi thẳng vào kho định danh ở máy chủ; web chỉ đọc
// tệp để gửi, không lưu, không hiển thị lại (R-04).
import { useState, type ChangeEvent, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import type { CoopProfileImportResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { filenameFromDisposition, readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { toApiProblem } from "@/lib/useAsync";
import { ResultList } from "./common";

export function CoopImportPanel({ onDone }: { onDone: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CoopProfileImportResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);

  const template = async () => {
    setProblem(null);
    try {
      const res = await adminApi.GET("/v1/cooperatives/import-template.xlsx", { parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      saveBlob(res.data, filenameFromDisposition(res.response.headers.get("Content-Disposition"), "mau-nhap-htx.xlsx"));
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const ok = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const r = await unwrap(
        adminApi.POST("/v1/cooperatives/import", {
          params: { header: { "Idempotency-Key": key } },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setResult(r);
      if (r.created > 0) onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 8, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Nhập HTX hàng loạt">
      <b>Nhập hồ sơ HTX từ tệp Excel mẫu (tối đa 200 dòng)</b>
      <span className="small mute">
        Cột bắt buộc: Tên HTX, Địa chỉ, Người đại diện, SĐT, Mã xã. Mã HTX do hệ thống sinh. Người đại diện và SĐT được lưu mã hoá trong kho định danh.
      </span>
      <div className="row" style={{ alignItems: "flex-end" }}>
        <button type="button" className="btn btn-o btn-sm" onClick={() => void template()}>
          <Icon name="download" size={15} /> Tải tệp mẫu
        </button>
        <input type="file" aria-label="Tệp danh sách HTX" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="submit" className="btn btn-p btn-sm" disabled={!ok || busy}>
          {busy ? "Đang nhập…" : "Nhập"}
        </button>
      </div>
      {file && !ok ? <span className="err small">Sai định dạng file: chỉ nhận .xlsx hoặc CSV UTF-8 (không nhận .xls cũ).</span> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <div className="stack" style={{ gap: 6 }}>
          <p role="status">
            <b style={{ display: "inline" }}>{result.created} dòng thành công</b> · {result.rejected} dòng lỗi
          </p>
          <ResultList
            caption="Kết quả nhập HTX theo dòng"
            rows={result.rows.map((r) => ({
              key: String(r.row),
              label: `Dòng ${r.row}${r.name ? `: ${r.name}` : ""}${r.code ? ` (${r.code})` : ""}`,
              ok: r.status === "created",
              status: r.status === "created" ? "Đã tạo" : "Bị loại",
              reason: r.reason ?? undefined,
            }))}
          />
        </div>
      ) : null}
    </form>
  );
}
