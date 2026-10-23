"use client";

// BM-18 Phụ lục 2 — Ước kết quả thực hiện chỉ tiêu thủy sản (W11.44, BRD CNTS v3.7 UR-TINH-15, FN-16): theo tuần CNTS
// (Thứ Ba – Thứ Hai, D8) — cột Tuần trước liền kề, Trong kỳ, Lũy kế từ đầu quý, Lũy kế từ đầu năm (theo ngày cuối tuần).
// Khai thác tự cộng từ BM-17 của các xã (không sửa tay, gom Khu vực); nuôi trồng nhập theo đối tượng; tổng sản lượng =
// khai thác + nuôi trồng. Chốt / mở lại tuần (số công bố cho KHTC), phiên bản tệp chia sẻ (sheet TỔNG + 4 sheet khai thác).
// BF-SWEEP-CNTS (#101): các bảng ở đây là biểu báo cáo (dòng nhóm, dòng khu vực) nên giữ bố cục biểu; ô nuôi trồng kiểm bằng
// parseNumberInput (không âm, ≤ 4 số lẻ) và gửi số đã chuẩn hóa; lý do mở lại nhập trong pop-up ngắn thay `window.prompt`.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, num, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { ReasonDialog } from "../ReasonDialog";
import { AGG_STATE, aquaValue, aquaValueError, createCntsAggApi, type Basis, type CntsBM18, type CntsCols } from "./api";
import { ShareSection } from "./CntsKsgmScreen";

function ColsCells({ c, d = 3 }: { c: CntsCols; d?: number }) {
  return (
    <>
      <td className="num">{num(c.prev, d)}</td>
      <td className="num">{num(c.cur, d)}</td>
      <td className="num">{num(c.qtd, d)}</td>
      <td className="num">{num(c.ytd, d)}</td>
    </>
  );
}

const COLS_HEAD = (
  <>
    <th className="num">Tuần trước liền kề</th>
    <th className="num">Trong kỳ báo cáo</th>
    <th className="num">Lũy kế từ đầu quý</th>
    <th className="num">Lũy kế từ đầu năm</th>
  </>
);

export function CntsPl2Screen() {
  const { api, kn } = useAuth();
  const writer = kn.context?.role === "kn_tinh";
  const capi = useMemo(() => (api ? createCntsAggApi(api) : null), [api]);
  const [week, setWeek] = useState("");
  const [basis, setBasis] = useState<Basis>("submitted");
  const data = useLoad(capi ? () => capi.bm18({ week: week || undefined, basis }) : null, [capi, week, basis]);
  const act = useAction();
  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader title="Thủy sản — Phụ lục 2 (BM-18)">
        <p className="mute">Phần III Khai thác và nuôi trồng thủy sản: khai thác tự cộng từ BM-17 của các xã, nuôi trồng nhập theo đối tượng.</p>
      </PageHeader>
      {act.view}
      <div className="toolbar" role="group" aria-label="Chọn tuần">
        <button className="btn btn-o btn-sm" disabled={!data.data?.prev_key} onClick={() => setWeek(data.data?.prev_key ?? "")}>‹ Tuần trước</button>
        <strong aria-label="Tuần đang xem">{data.data?.week.label ?? "…"}</strong>
        <button className="btn btn-o btn-sm" disabled={!data.data?.next_key} onClick={() => setWeek(data.data?.next_key ?? "")}>Tuần sau ›</button>
        <select className="select" aria-label="Số liệu" value={basis} onChange={(e) => setBasis(e.target.value as Basis)}>
          <option value="submitted">Số đã gửi</option>
          <option value="closed">Số đã chốt (kỳ nguồn)</option>
        </select>
      </div>
      <LoadState s={data}>{(v) => <Pl2Body key={v.week.key + v.basis} v={v} writer={writer} act={act} capi={capi} reload={data.reload} />}</LoadState>
    </div>
  );
}

function Pl2Body({ v, writer, act, capi, reload }: {
  v: CntsBM18;
  writer: boolean;
  act: ReturnType<typeof useAction>;
  capi: ReturnType<typeof createCntsAggApi>;
  reload: () => void;
}) {
  const closed = v.aggregate.state === "closed";
  const [edit, setEdit] = useState<Record<string, { area: string; output: string }>>(() =>
    Object.fromEntries(v.aqua.map((a) => [a.code, { area: a.entered_area ?? "", output: a.entered_output ?? "" }])),
  );
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [reopening, setReopening] = useState(false);
  const st = AGG_STATE[v.aggregate.state];
  const groupLabel = Object.fromEntries(v.aqua_groups.map((g) => [g.key, g.label]));
  const saveAqua = async () => {
    const e: Record<string, string> = {};
    const entries = v.aqua.flatMap((a) => {
      const x = edit[a.code] ?? { area: "", output: "" };
      const ea = aquaValueError(x.area);
      const eo = aquaValueError(x.output);
      if (ea) e[a.code + ".area"] = ea;
      if (eo) e[a.code + ".output"] = eo;
      const area = ea ? null : aquaValue(x.area);
      const output = eo ? null : aquaValue(x.output);
      const changed = area !== (a.entered_area ?? null) || output !== (a.entered_output ?? null);
      return changed ? [{ object: a.code, area, output }] : [];
    });
    setErrs(e);
    if (Object.keys(e).length || entries.length === 0) return;
    if (await act.run(() => capi.saveAqua(v.week.key, entries), "Đã lưu số nuôi trồng.")) reload();
  };
  return (
    <>
      <section className="card sec">
        <h2>Tình trạng</h2>
        <p>
          Kỳ BM-17 của tuần đã chốt: {v.sources.closed}/{v.sources.count} · Xã có báo cáo: {v.units_reported}/{v.units_total} · Lịch: {v.calendar} ·{" "}
          <StatusPill tone={st[1]}>{st[0]}</StatusPill>
          {v.aggregate.changed ? <StatusPill tone="warn">Nguồn đã thay đổi</StatusPill> : null}
        </p>
        {v.missing_units.length ? <p className="warn">Xã chưa nộp: {v.missing_units.join(", ")}</p> : null}
        {writer && !closed ? (
          <button className="btn btn-primary" disabled={act.busy || !v.sources.all_closed}
            title={v.sources.all_closed ? undefined : "Chốt mọi kỳ BM-17 của tuần trước"}
            onClick={async () => (await act.run(() => capi.close("CNTS-BM18", v.week.key), "Đã chốt Phụ lục 2 của tuần.")) && reload()}>
            Chốt tuần
          </button>
        ) : null}
        {writer && closed ? (
          <button className="btn btn-o" disabled={act.busy} onClick={() => setReopening(true)}>
            Mở lại
          </button>
        ) : null}
        {reopening ? (
          <ReasonDialog
            title={`Mở lại Phụ lục 2 — ${v.week.label}`}
            label="Lý do mở lại"
            confirmLabel="Mở lại"
            busy={act.busy}
            onClose={() => setReopening(false)}
            onConfirm={async (r) => {
              if (await act.run(() => capi.reopen("CNTS-BM18", v.week.key, r), "Đã mở lại tuần.")) {
                setReopening(false);
                reload();
              }
            }}
          />
        ) : null}
      </section>

      <section className="card sec">
        <h2>Phụ lục 2 — Phần III</h2>
        <table className="dt" aria-label="Phụ lục 2">
          <thead>
            <tr><th>Chỉ tiêu</th><th>ĐVT</th>{COLS_HEAD}</tr>
          </thead>
          <tbody>
            <tr><td><strong>Tổng sản lượng thủy sản</strong></td><td>tấn</td><ColsCells c={v.total} /></tr>
            <tr><td><strong>1. Khai thác</strong></td><td>tấn</td><ColsCells c={v.catch.tong!} /></tr>
            {v.groups.filter((g) => g.key !== "tong").map((g) => (
              <tr key={g.key}><td>&nbsp;&nbsp;{g.label}</td><td>tấn</td><ColsCells c={v.catch[g.key]!} /></tr>
            ))}
            <tr><td><strong>2. Nuôi trồng — tổng diện tích</strong></td><td>ha</td><ColsCells c={v.aqua_area} /></tr>
            <tr><td><strong>2. Nuôi trồng — tổng sản lượng</strong></td><td>tấn</td><ColsCells c={v.aqua_output} /></tr>
          </tbody>
        </table>
      </section>

      <section className="card sec">
        <h2>Nuôi trồng theo đối tượng</h2>
        <table className="dt" aria-label="Nuôi trồng">
          <thead>
            <tr>
              <th>Đối tượng</th><th>Diện tích trong tuần</th><th>Sản lượng trong tuần</th>
              <th className="num">Sản lượng — trong kỳ</th><th className="num">Lũy kế quý</th><th className="num">Lũy kế năm</th>
            </tr>
          </thead>
          <tbody>
            {v.aqua.map((a, i) => {
              const head = a.group && (i === 0 || v.aqua[i - 1]!.group !== a.group) ? (
                <tr key={a.group} className="row-sub"><td colSpan={6}><strong>{groupLabel[a.group] ?? a.group}</strong></td></tr>
              ) : null;
              const x = edit[a.code] ?? { area: "", output: "" };
              return [
                head,
                <tr key={a.code}>
                  <td>{a.group ? "  " : ""}{a.label}</td>
                  <td>
                    {writer && !closed ? (
                      <input className="input input-sm" inputMode="decimal" aria-label={`Diện tích ${a.label} (${a.area_unit})`} aria-invalid={errs[a.code + ".area"] ? true : undefined} value={x.area}
                        onChange={(e) => setEdit({ ...edit, [a.code]: { ...x, area: e.target.value } })} />
                    ) : num(a.area.cur, 3)}{" "}{a.area_unit}
                    {errs[a.code + ".area"] ? <span className="err" role="alert">{errs[a.code + ".area"]}</span> : null}
                  </td>
                  <td>
                    {writer && !closed ? (
                      <input className="input input-sm" inputMode="decimal" aria-label={`Sản lượng ${a.label} (${a.output_unit})`} aria-invalid={errs[a.code + ".output"] ? true : undefined} value={x.output}
                        onChange={(e) => setEdit({ ...edit, [a.code]: { ...x, output: e.target.value } })} />
                    ) : num(a.output.cur, 3)}{" "}{a.output_unit}
                    {errs[a.code + ".output"] ? <span className="err" role="alert">{errs[a.code + ".output"]}</span> : null}
                  </td>
                  <td className="num">{num(a.output.cur, 3)}</td>
                  <td className="num">{num(a.output.qtd, 3)}</td>
                  <td className="num">{num(a.output.ytd, 3)}</td>
                </tr>,
              ];
            })}
          </tbody>
        </table>
        {writer && !closed ? <button className="btn btn-primary" disabled={act.busy} onClick={() => void saveAqua()}>Lưu nuôi trồng</button> : null}
        {closed ? <p className="mute">Tuần đã chốt — mở lại để sửa số nuôi trồng.</p> : null}
      </section>

      <section className="card sec">
        <h2>Khai thác theo xã (tổng sản lượng, tấn)</h2>
        <table className="dt" aria-label="Khai thác theo xã">
          <thead>
            <tr><th>Khu vực</th><th>Xã/phường</th>{COLS_HEAD}<th>Ghi chú</th></tr>
          </thead>
          <tbody>
            {v.communes.map((c, i) => {
              const regionHead = i === 0 || v.communes[i - 1]!.region_code !== c.region_code;
              const reg = v.regions.find((r) => r.region_code === c.region_code);
              return [
                regionHead && reg ? (
                  <tr key={"r:" + c.region_code} className="row-sub">
                    <td colSpan={2}><strong>{reg.region_name}</strong></td>
                    <ColsCells c={reg.cells.tong!} />
                    <td />
                  </tr>
                ) : null,
                <tr key={c.commune_code}>
                  <td />
                  <td>{c.commune_name}</td>
                  <ColsCells c={c.cells.tong!} />
                  <td>
                    {c.expected && !c.reported ? <StatusPill tone="bad">Chưa nộp</StatusPill> : null}
                    {c.nil_report ? <StatusPill tone="neutral">Không phát sinh</StatusPill> : null}
                    {c.missing_weeks.length ? <StatusPill tone="warn">Thiếu kỳ: {c.missing_weeks.length}</StatusPill> : null}
                  </td>
                </tr>,
              ];
            })}
          </tbody>
        </table>
      </section>

      <ShareSection
        shares={v.shares}
        writer={writer}
        act={act}
        create={() => capi.share("CNTS-BM18", "week", v.week.key)}
        download={(id, ver) => capi.download(id).then((b) => saveBlob(b, `phu-luc-2-${v.week.key}-v${ver}.xlsx`))}
        reload={reload}
      />
    </>
  );
}
