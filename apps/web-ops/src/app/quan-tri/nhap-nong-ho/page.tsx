"use client";

import { useState, type ChangeEvent, type FormEvent } from "react";
import { importHouseholds, IMPORT_MAX_ROWS, precheckCsv, type CsvPrecheck } from "@/admin/households";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { HouseholdImportResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";

const STATUS = {
  accepted: { text: "Đã nhận", cls: "c-ok" },
  duplicate: { text: "Trùng — dùng lại mã cũ", cls: "c-warn" },
  rejected: { text: "Bị từ chối", cls: "c-bad" },
} as const;

/** Nhập nông hộ từ CSV — Admin API POST /v1/households/import (MFA bắt buộc). */
export default function HouseholdImportPage() {
  const { adminApi, principal } = useAuth();
  const [csv, setCsv] = useState<string | null>(null);
  const [fileName, setFileName] = useState("");
  const [check, setCheck] = useState<CsvPrecheck | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<HouseholdImportResult | null>(null);
  // Một tệp = một thao tác: gửi lại cùng tệp (vd sau lỗi mạng) dùng lại khóa → không nhập trùng.
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    if (!f) {
      setCsv(null);
      setCheck(null);
      return;
    }
    const text = await f.text();
    setFileName(f.name);
    setCsv(text);
    setCheck(precheckCsv(text));
    setIdemKey(newIdempotencyKey());
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!csv) return;
    setBusy(true);
    setProblem(null);
    try {
      setResult(await importHouseholds(adminApi, csv, idemKey));
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  const blocked = !csv || !check || check.missingColumns.length > 0 || check.tooMany || check.rows === 0;

  return (
    <>
      <PageHead title="Nhập nông hộ từ tệp" subtitle="Tệp CSV UTF-8, dòng đầu là tiêu đề, tối đa 200 dòng — kết quả theo từng dòng" />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Cột: <code>full_name|ho_ten</code>, <code>phone|so_dien_thoai</code>, <code>national_id|cccd</code>, <code>address|dia_chi</code>,{" "}
          <code>cooperative_code|ma_htx</code>, <code>commune_code|ma_xa</code>, <code>cultivated_area_ha|dien_tich_ha</code>,{" "}
          <code>member_since|ngay_vao_htx</code>. Bắt buộc: họ tên, SĐT, Mã HTX. Định danh chủ hộ đi thẳng vào kho định danh mã hoá; trang
          này không lưu và không hiển thị lại nội dung tệp.
        </span>
      </div>
      {principal && !principal.mfa ? (
        <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status">
          <Icon name="alert" />
          <span>Phiên hiện tại chưa qua xác thực hai yếu tố — Admin API sẽ từ chối (mfa_required).</span>
        </div>
      ) : null}
      <form className="card sec stack" onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor="hh-file">Chọn tệp CSV</label>
          <input id="hh-file" type="file" accept=".csv,text/csv" onChange={(e) => void onFile(e)} />
        </div>
        {check ? (
          <div className="small">
            {fileName}: {formatInt(check.rows)} dòng dữ liệu.
            {check.missingColumns.length > 0 ? (
              <p className="err" role="alert">
                Thiếu cột bắt buộc: {check.missingColumns.join("; ")}.
              </p>
            ) : null}
            {check.tooMany ? (
              <p className="err" role="alert">
                Quá {IMPORT_MAX_ROWS} dòng — chia thành nhiều tệp.
              </p>
            ) : null}
          </div>
        ) : null}
        <div>
          <button type="submit" className="btn btn-p" disabled={blocked || busy}>
            {busy ? "Đang nhập…" : "Nhập nông hộ"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </form>

      {result ? (
        <section className="card sec" style={{ marginTop: 16 }} aria-labelledby="imp-title">
          <div className="sec-h">
            <h2 id="imp-title">Kết quả nhập</h2>
            <span className="small">
              <span className="chip c-ok">{formatInt(result.accepted)} đã nhận</span>{" "}
              <span className="chip c-warn">{formatInt(result.duplicate)} trùng</span>{" "}
              <span className="chip c-bad">{formatInt(result.rejected)} bị từ chối</span>
            </span>
          </div>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Kết quả theo dòng</caption>
              <thead>
                <tr>
                  <th scope="col" className="num">Dòng</th>
                  <th scope="col">Kết quả</th>
                  <th scope="col">Lý do</th>
                  <th scope="col">Mã tham chiếu nông hộ</th>
                </tr>
              </thead>
              <tbody>
                {result.rows.map((r) => (
                  <tr key={r.row}>
                    <td className="num">{r.row}</td>
                    <td>
                      <span className={`chip ${STATUS[r.status].cls}`}>{STATUS[r.status].text}</span>
                    </td>
                    <td>{r.reason ?? "—"}</td>
                    <td>
                      <code className="small">{r.household_ref ?? "—"}</code>
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
