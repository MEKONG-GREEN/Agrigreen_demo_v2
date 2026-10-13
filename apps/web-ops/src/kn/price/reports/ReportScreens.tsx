"use client";

// Màn Báo cáo giá (W11.77; BRD Báo giá nông sản v1.3): Tiến độ nhập giá (FN-TI-02.01, Cấp Tỉnh), Bảng giá tổng hợp
// (FN-TW-01.01, mọi cấp — chỉ xem), Báo cáo biến động giá (FN-TW-01.02), Dashboard theo vùng (FN-TW-01.03, Cấp TW / Tỉnh),
// Thống kê phiếu báo giá theo cán bộ Xã (FN-TI-02.08, Cấp Tỉnh; khóa tháng Q-085 chỉ QTNV — D11). Số liệu chỉ từ bảng giá
// đã duyệt; giá, % là chuỗi decimal (không float — thanh biểu đồ tính tỉ lệ bằng Decimal BigInt).
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import {
  DataTable,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { Decimal, formatDateVi } from "@/kn/grid/decimal";
import { saveBlob } from "@/lib/files";
import {
  money,
  TICKET_VIEW_LABEL,
  TICKET_VIEW_TONE,
  vnTime,
  vnToday,
} from "../tickets/api";
import {
  createPriceReportApi,
  signedPct,
  TREND_LABEL,
  TREND_TONE,
  type PriceDashboardLine,
  type PriceMovementLine,
  type PricePointPrice,
  type PriceProgressRow,
  type PriceReportApi,
  type PriceStatRow,
  type PriceSummaryLine,
} from "./api";

export function usePriceReportApi(): PriceReportApi {
  const { api } = useAuth();
  return useMemo(() => createPriceReportApi(api), [api]);
}

type Ref = { code: string; name: string };

/** Bảng không phân trang (báo cáo hiển thị đủ dòng). */
function Table<T>({
  caption,
  columns,
  rows,
  rowKey,
  empty,
}: {
  caption: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (r: T) => string;
  empty?: ReactNode;
}) {
  return (
    <DataTable
      caption={caption}
      columns={columns}
      rows={rows}
      rowKey={rowKey}
      page={1}
      pageSize={Math.max(rows.length, 1)}
      total={rows.length}
      onPage={() => undefined}
      empty={empty ?? "Không có dữ liệu phù hợp."}
    />
  );
}

function Select({
  id,
  label,
  value,
  onChange,
  options,
  all = "Tất cả",
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: Ref[];
  all?: string;
}) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <select
        id={id}
        className="input"
        style={{ width: 180 }}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">{all}</option>
        {options.map((o) => (
          <option key={o.code} value={o.code}>
            {o.name}
          </option>
        ))}
      </select>
    </>
  );
}

function DayInput({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <>
      <label htmlFor={id}>Ngày báo giá</label>
      <input
        id={id}
        type="date"
        className="input"
        style={{ width: 170 }}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </>
  );
}

function Card({
  label,
  value,
  tone = "neutral",
}: {
  label: string;
  value: ReactNode;
  tone?: "neutral" | "ok" | "warn" | "bad" | "info";
}) {
  return (
    <div className="card" style={{ padding: "8px 12px", minWidth: 130 }}>
      <div className="muted" style={{ fontSize: 12 }}>
        {label}
      </div>
      <StatusPill tone={tone}>{value}</StatusPill>
    </div>
  );
}

/** Thanh tỉ lệ value / max (Decimal — không float); rỗng → không vẽ. */
export function barPct(
  value: string | null | undefined,
  max: string | null | undefined,
): string {
  const v = value ? Decimal.parse(value) : null;
  const m = max ? Decimal.parse(max) : null;
  if (!v || !m || m.sign() <= 0 || v.sign() <= 0) return "0";
  return v.mul(Decimal.fromInt(100)).divRound(m, 1).toString();
}

function Bar({
  value,
  max,
  label,
}: {
  value: string | null | undefined;
  max: string | null | undefined;
  label: string;
}) {
  return (
    <div className="row" style={{ gap: 8, alignItems: "center" }}>
      <span style={{ minWidth: 120 }}>{label}</span>
      <span
        style={{
          flex: 1,
          background: "var(--surface-2, #eef2f0)",
          borderRadius: 4,
          height: 10,
          position: "relative",
        }}
      >
        <span
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            bottom: 0,
            width: `${barPct(value, max)}%`,
            background: "var(--brand, #2e7d32)",
            borderRadius: 4,
          }}
        />
      </span>
      <span style={{ minWidth: 90, textAlign: "right" }}>{money(value)}</span>
    </div>
  );
}

function maxOf(vs: (string | null | undefined)[]): string | null {
  let m: Decimal | null = null;
  for (const v of vs) {
    const d = v ? Decimal.parse(v) : null;
    if (d && (!m || d.gt(m))) m = d;
  }
  return m ? m.toString() : null;
}

function trendCell(r: {
  trend: keyof typeof TREND_LABEL;
  change_pct: string | null;
}) {
  return r.trend ? (
    <StatusPill
      tone={TREND_TONE[r.trend]}
    >{`${TREND_LABEL[r.trend]} ${signedPct(r.change_pct)}`}</StatusPill>
  ) : (
    "—"
  );
}

/** Nhãn ngày công bố của bảng đang hiển thị (luồng ngoại lệ FN-TW-01.01, 01.03). */
function PublishedBadge({
  d,
}: {
  d: {
    day: string | null;
    version: number;
    approved_at: string | null;
    not_today: boolean;
    updating: boolean;
  };
}) {
  if (!d.day) return <StatusPill tone="neutral">Chưa có dữ liệu</StatusPill>;
  return (
    <span className="row" style={{ gap: 6 }}>
      <StatusPill tone="ok">{`Đã công bố ${vnTime(d.approved_at).slice(0, 5)} · ${formatDateVi(d.day)} (v${d.version})`}</StatusPill>
      {d.not_today ? (
        <StatusPill tone="warn">Chưa công bố hôm nay</StatusPill>
      ) : null}
      {d.updating ? <StatusPill tone="warn">Đang cập nhật</StatusPill> : null}
    </span>
  );
}

function useDownload() {
  const act = useAction();
  const run = (f: () => Promise<{ blob: Blob; name: string }>) =>
    act.run(async () => {
      const x = await f();
      saveBlob(x.blob, x.name);
    });
  return { act, run };
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.01 — Tiến độ nhập giá
// ---------------------------------------------------------------------------------------------------------------------

const PROGRESS_STATUS: Ref[] = [
  { code: "incomplete", name: "Chưa hoàn thiện" },
  { code: "submitted", name: "Đã gửi" },
  { code: "late", name: "Nộp muộn" },
  { code: "missed", name: "Không nộp" },
  { code: "draft", name: "Bản nháp" },
  { code: "not_entered", name: "Chưa nhập" },
  { code: "unassigned", name: "Chưa phân công" },
  { code: "no_officer", name: "Chưa có cán bộ Xã" },
];

export function ProgressScreen() {
  const ra = usePriceReportApi();
  const [day, setDay] = useState(vnToday());
  const [region, setRegion] = useState("");
  const [status, setStatus] = useState("");
  const [mine, setMine] = useState(false);
  const q = { day, region, status, mine };
  const data = useLoad(() => ra.progress(q), [day, region, status, mine]);
  const dl = useDownload();
  const cols: Column<PriceProgressRow>[] = [
    { key: "rg", header: "Nhóm vùng", cell: (r) => r.point.region_name },
    {
      key: "pt",
      header: "Xã báo giá",
      cell: (r) => `${r.point.name} (${r.point.code})`,
    },
    {
      key: "xa",
      header: "Cán bộ Xã",
      cell: (r) =>
        r.point.officer_name ??
        (r.point.officer_ref ? (
          "—"
        ) : (
          <StatusPill tone="warn">Chưa có cán bộ Xã</StatusPill>
        )),
    },
    {
      key: "tinh",
      header: "Cán bộ Tỉnh phụ trách",
      cell: (r) =>
        r.point.owner_name ??
        (r.point.owner_ref ? (
          "—"
        ) : (
          <StatusPill tone="warn">Chưa phân công</StatusPill>
        )),
    },
    {
      key: "n",
      header: "Đã cập nhật / phải báo giá",
      cell: (r) => `${r.priced + r.no_goods} / ${r.items}`,
    },
    {
      key: "ab",
      header: "Bất thường ▲ / ▼",
      cell: (r) =>
        r.high + r.low ? (
          <span
            style={{ color: "var(--danger, #c62828)" }}
          >{`${r.high} / ${r.low}`}</span>
        ) : (
          "—"
        ),
    },
    {
      key: "st",
      header: "Trạng thái",
      cell: (r) => (
        <StatusPill tone={TICKET_VIEW_TONE[r.view]}>
          {TICKET_VIEW_LABEL[r.view]}
        </StatusPill>
      ),
    },
    { key: "at", header: "Cập nhật lúc", cell: (r) => vnTime(r.updated_at) },
    {
      key: "rem",
      header: "Hệ thống đã nhắc",
      cell: (r) =>
        r.reminders.length
          ? r.reminders.map((t) => vnTime(t).slice(0, 5)).join(", ")
          : "—",
    },
    {
      key: "act",
      header: "",
      cell: (r) =>
        r.view === "missed" ? (
          <Link
            className="btn"
            href={`/kn/bao-gia/kiem-duyet?day=${day}&point=${r.point.code}&proxy=1`}
          >
            Nhập thay
          </Link>
        ) : r.ticket ? (
          <Link
            className="btn"
            href={`/kn/bao-gia/kiem-duyet?day=${day}&point=${r.point.code}`}
          >
            Xem
          </Link>
        ) : null,
    },
  ];
  return (
    <>
      <PageHeader
        title="Tiến độ nhập giá"
        badge={
          data.data ? (
            <StatusPill tone={data.data.locked ? "info" : "warn"}>
              {data.data.locked
                ? "Đã khóa nhập"
                : `Khóa lúc ${vnTime(data.data.lock_at).slice(0, 5)}`}
            </StatusPill>
          ) : null
        }
        actions={
          <button
            type="button"
            className="btn"
            disabled={dl.act.busy}
            onClick={() => void dl.run(() => ra.progressExport(q))}
          >
            Xuất Excel
          </button>
        }
      >
        Các xã chưa hoàn thiện báo giá trong ngày (sau giờ khóa là Không nộp),
        số mặt hàng cao / thấp bất thường và các lần hệ thống đã nhắc cán bộ Xã.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <DayInput id="pg-day" value={day} onChange={(v) => v && setDay(v)} />
        <Select
          id="pg-region"
          label="Nhóm vùng"
          value={region}
          onChange={setRegion}
          options={data.data?.regions ?? []}
        />
        <Select
          id="pg-status"
          label="Trạng thái"
          value={status}
          onChange={setStatus}
          options={PROGRESS_STATUS}
        />
        <label className="row" style={{ gap: 6 }}>
          <input
            type="checkbox"
            checked={mine}
            onChange={(e) => setMine(e.target.checked)}
          />{" "}
          Chỉ xã được phân công
        </label>
      </div>
      {dl.act.view}
      <LoadState s={data}>
        {(d) => {
          const k = d.cards;
          return (
            <>
              {d.no_quote ? (
                <p role="status" className="muted">
                  Không có kỳ báo giá ngày {formatDateVi(d.requested)} — hiển
                  thị ngày báo giá gần nhất {formatDateVi(d.day)}.
                </p>
              ) : null}
              <div
                className="row"
                style={{ gap: 8, flexWrap: "wrap", marginBottom: 12 }}
                aria-label="Thẻ chỉ số"
              >
                <Card
                  label="Đã gửi / phải báo giá"
                  value={`${k.submitted} / ${k.points}`}
                  tone="ok"
                />
                <Card label="Bản nháp" value={k.draft} tone="warn" />
                <Card
                  label={d.locked ? "Không nộp" : "Chưa nhập"}
                  value={d.locked ? k.missed : k.not_entered}
                  tone="bad"
                />
                <Card label="Nộp muộn" value={k.late} tone="warn" />
                <Card
                  label="▲ Cao bất thường"
                  value={k.high}
                  tone={k.high ? "bad" : "neutral"}
                />
                <Card
                  label="▼ Thấp bất thường"
                  value={k.low}
                  tone={k.low ? "bad" : "neutral"}
                />
              </div>
              {k.high + k.low ? (
                <p>
                  <Link href={`/kn/bao-gia/kiem-duyet?day=${d.day}&tab=alerts`}>
                    Mở danh sách Cảnh báo biến động
                  </Link>
                </p>
              ) : null}
              <Table
                caption="Tiến độ nhập giá các xã"
                columns={cols}
                rows={d.rows}
                rowKey={(r) => r.point.code}
              />
            </>
          );
        }}
      </LoadState>
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.01 — Bảng giá tổng hợp
// ---------------------------------------------------------------------------------------------------------------------

const pointCols: Column<PricePointPrice>[] = [
  { key: "rg", header: "Nhóm vùng", cell: (r) => r.region_name },
  { key: "pt", header: "Xã báo giá", cell: (r) => r.point_name },
  { key: "grp", header: "Nhóm hàng", cell: (r) => r.group_name },
  { key: "it", header: "Mặt hàng", cell: (r) => r.item_name },
  { key: "u", header: "ĐVT", cell: (r) => r.unit_name },
  { key: "min", header: "Giá thấp nhất", cell: (r) => money(r.min) },
  { key: "max", header: "Giá cao nhất", cell: (r) => money(r.max) },
  {
    key: "late",
    header: "",
    cell: (r) =>
      r.late ? <StatusPill tone="warn">Nộp muộn</StatusPill> : null,
  },
];

export function SummaryScreen() {
  const ra = usePriceReportApi();
  const [day, setDay] = useState("");
  const [region, setRegion] = useState("");
  const [group, setGroup] = useState("");
  const [view, setView] = useState<"item" | "point">("item");
  const [item, setItem] = useState<string | null>(null);
  const q = { day, region, group, view };
  const data = useLoad(() => ra.summary(q), [day, region, group, view]);
  const detail = useLoad(
    item ? () => ra.summary({ ...q, view: "item", item }) : null,
    [item, day, region, group],
  );
  const dl = useDownload();
  const cols: Column<PriceSummaryLine>[] = [
    { key: "grp", header: "Nhóm hàng", cell: (r) => r.group_name },
    {
      key: "it",
      header: "Mặt hàng",
      cell: (r) => (
        <button
          type="button"
          className="link"
          onClick={() => setItem(r.item_code)}
        >
          {r.item_name}
        </button>
      ),
    },
    { key: "u", header: "ĐVT", cell: (r) => r.unit_name },
    { key: "min", header: "Giá thấp nhất", cell: (r) => money(r.min) },
    { key: "max", header: "Giá cao nhất", cell: (r) => money(r.max) },
    { key: "avg", header: "Giá trung bình", cell: (r) => money(r.avg) },
    { key: "tr", header: "Tăng/giảm so kỳ trước", cell: trendCell },
    { key: "n", header: "Số xã có giá", cell: (r) => r.points },
  ];
  return (
    <>
      <PageHeader
        title="Bảng giá tổng hợp"
        badge={data.data ? <PublishedBadge d={data.data} /> : null}
        actions={
          <button
            type="button"
            className="btn"
            disabled={dl.act.busy || !data.data?.day}
            onClick={() => void dl.run(() => ra.summaryExport(q))}
          >
            Xuất Excel
          </button>
        }
      >
        Bảng giá đã công bố (phiên bản đã duyệt mới nhất), tổng hợp theo mặt
        hàng, xã và nhóm vùng, kèm mức tăng/giảm so với bảng giá công bố liền
        trước.
      </PageHeader>
      <p role="note" className="muted">
        Chỉ xem — bảng tổng hợp không có thao tác sửa, xóa.
      </p>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <DayInput id="sb-day" value={day} onChange={setDay} />
        <Select
          id="sb-region"
          label="Nhóm vùng"
          value={region}
          onChange={setRegion}
          options={data.data?.regions ?? []}
          all="Toàn tỉnh"
        />
        <Select
          id="sb-group"
          label="Nhóm hàng"
          value={group}
          onChange={setGroup}
          options={data.data?.groups ?? []}
        />
        <label htmlFor="sb-view">Cách xem</label>
        <select
          id="sb-view"
          className="input"
          style={{ width: 150 }}
          value={view}
          onChange={(e) => setView(e.target.value as "item" | "point")}
        >
          <option value="item">Theo mặt hàng</option>
          <option value="point">Theo xã</option>
        </select>
      </div>
      {dl.act.view}
      <LoadState s={data}>
        {(d) =>
          !d.day ? (
            <p role="status">Chưa có dữ liệu.</p>
          ) : view === "point" ? (
            <Table
              caption="Bảng giá theo xã"
              columns={pointCols}
              rows={d.points}
              rowKey={(r) => `${r.point_code}:${r.item_code}`}
            />
          ) : (
            <Table
              caption="Bảng giá tổng hợp"
              columns={cols}
              rows={d.lines}
              rowKey={(r) => r.item_code}
            />
          )
        }
      </LoadState>
      {item ? (
        <Dialog title="Giá từng xã" onClose={() => setItem(null)}>
          <LoadState s={detail}>
            {(d) => (
              <Table
                caption="Giá từng xã"
                columns={pointCols}
                rows={d.points}
                rowKey={(r) => r.point_code}
              />
            )}
          </LoadState>
        </Dialog>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.02 — Báo cáo tăng giảm, biến động giá
// ---------------------------------------------------------------------------------------------------------------------

export function MovementScreen() {
  const ra = usePriceReportApi();
  const [period, setPeriod] = useState<"day" | "week" | "month">("day");
  const [day, setDay] = useState("");
  const [region, setRegion] = useState("");
  const [group, setGroup] = useState("");
  const [item, setItem] = useState("");
  const q = { period, day, region, group, item };
  const data = useLoad(
    () => ra.movement(q),
    [period, day, region, group, item],
  );
  const dl = useDownload();
  const cols: Column<PriceMovementLine>[] = [
    { key: "grp", header: "Nhóm hàng", cell: (r) => r.group_name },
    {
      key: "it",
      header: "Mặt hàng",
      cell: (r) => (
        <button
          type="button"
          className="link"
          onClick={() => setItem(r.item_code)}
        >
          {r.item_name}
        </button>
      ),
    },
    { key: "u", header: "ĐVT", cell: (r) => r.unit_name },
    { key: "cur", header: "Giá kỳ này", cell: (r) => money(r.cur) },
    { key: "prev", header: "Giá kỳ trước", cell: (r) => money(r.prev) },
    { key: "tr", header: "Biến động", cell: trendCell },
  ];
  return (
    <>
      <PageHeader
        title="Báo cáo biến động giá"
        actions={
          data.data?.can_export ? (
            <span className="row" style={{ gap: 6 }}>
              <button
                type="button"
                className="btn"
                disabled={dl.act.busy}
                onClick={() =>
                  void dl.run(() => ra.movementExport({ ...q, format: "xlsx" }))
                }
              >
                Xuất Excel
              </button>
              <button
                type="button"
                className="btn"
                disabled={dl.act.busy}
                onClick={() =>
                  void dl.run(() => ra.movementExport({ ...q, format: "pdf" }))
                }
              >
                Xuất PDF
              </button>
            </span>
          ) : null
        }
      >
        Mức tăng/giảm giá giữa các kỳ theo mặt hàng và xã, tổng hợp từ bảng giá
        đã công bố. Giá mỗi kỳ là trung bình Giá trung bình các ngày công bố
        trong kỳ.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <label htmlFor="mv-period">Kỳ so sánh</label>
        <select
          id="mv-period"
          className="input"
          style={{ width: 120 }}
          value={period}
          onChange={(e) => setPeriod(e.target.value as typeof period)}
        >
          <option value="day">Ngày</option>
          <option value="week">Tuần</option>
          <option value="month">Tháng</option>
        </select>
        <DayInput id="mv-day" value={day} onChange={setDay} />
        <Select
          id="mv-region"
          label="Nhóm vùng"
          value={region}
          onChange={setRegion}
          options={data.data?.regions ?? []}
          all="Toàn tỉnh"
        />
        <Select
          id="mv-group"
          label="Nhóm hàng"
          value={group}
          onChange={setGroup}
          options={data.data?.groups ?? []}
        />
      </div>
      {dl.act.view}
      <LoadState s={data}>
        {(d) => {
          if (d.insufficient && !d.lines.length)
            return <p role="status">Chưa đủ dữ liệu so sánh.</p>;
          const trendMax = maxOf(d.trend.map((t) => t.avg));
          const sel = d.lines.find((l) => l.item_code === d.item);
          return (
            <>
              <p className="muted">
                Kỳ này: {d.cur.label} ({d.cur.days} ngày công bố) · Kỳ trước:{" "}
                {d.prev.label || "—"} ({d.prev.days} ngày công bố)
                {d.insufficient ? " · Chưa đủ dữ liệu so sánh" : ""}
              </p>
              <div
                className="row"
                style={{ gap: 8, flexWrap: "wrap", marginBottom: 12 }}
                aria-label="Thẻ chỉ số"
              >
                <Card label="Số mặt hàng tăng" value={d.cards.up} tone="ok" />
                <Card
                  label="Số mặt hàng giảm"
                  value={d.cards.down}
                  tone="bad"
                />
                <Card label="Số mặt hàng đi ngang" value={d.cards.flat} />
                <Card
                  label="Số lần vượt ngưỡng"
                  value={d.cards.exceeded}
                  tone="warn"
                />
              </div>
              <Table
                caption="Biến động giá theo mặt hàng"
                columns={cols}
                rows={d.lines}
                rowKey={(r) => r.item_code}
              />
              {sel ? (
                <section
                  className="card"
                  style={{ padding: 12, marginTop: 12 }}
                  aria-label="Xu hướng"
                >
                  <strong>Xu hướng — {sel.item_name}</strong>
                  {d.trend.map((t) => (
                    <Bar
                      key={t.period.label}
                      label={t.period.label}
                      value={t.avg}
                      max={trendMax}
                    />
                  ))}
                  <h4>Chi tiết theo xã</h4>
                  <Table
                    caption="Biến động theo xã"
                    columns={[
                      {
                        key: "pt",
                        header: "Xã báo giá",
                        cell: (r) => r.point_name,
                      },
                      {
                        key: "rg",
                        header: "Nhóm vùng",
                        cell: (r) => r.region_name,
                      },
                      {
                        key: "cur",
                        header: "Kỳ này",
                        cell: (r) => money(r.cur),
                      },
                      {
                        key: "prev",
                        header: "Kỳ trước",
                        cell: (r) => money(r.prev),
                      },
                      { key: "tr", header: "Biến động", cell: trendCell },
                    ]}
                    rows={d.points}
                    rowKey={(r) => r.point_code}
                  />
                </section>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.03 — Dashboard theo vùng
// ---------------------------------------------------------------------------------------------------------------------

export function DashboardScreen() {
  const ra = usePriceReportApi();
  const [day, setDay] = useState("");
  const [group, setGroup] = useState("");
  const [item, setItem] = useState("");
  const [region, setRegion] = useState("");
  const [all, setAll] = useState(false);
  const q = { day, group, item, region, all };
  const data = useLoad(() => ra.dashboard(q), [day, group, item, region, all]);
  const dl = useDownload();
  return (
    <>
      <PageHeader
        title="Dashboard giá theo vùng"
        badge={data.data ? <PublishedBadge d={data.data} /> : null}
        actions={
          <button
            type="button"
            className="btn"
            disabled={dl.act.busy || !data.data?.day}
            onClick={() => void dl.run(() => ra.dashboardExport(q))}
          >
            Xuất Excel
          </button>
        }
      >
        Giá nông sản đã công bố theo nhóm vùng và từng xã trong vùng. Bấm tên
        vùng để xem giá theo xã.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <DayInput id="db-day" value={day} onChange={setDay} />
        <Select
          id="db-group"
          label="Nhóm hàng"
          value={group}
          onChange={setGroup}
          options={(data.data?.groups ?? []).map((g) => ({
            code: g.code,
            name: g.name,
          }))}
        />
        <label className="row" style={{ gap: 6 }}>
          <input
            type="checkbox"
            checked={all}
            onChange={(e) => setAll(e.target.checked)}
          />{" "}
          Mọi mặt hàng (mặc định: mặt hàng cơ bản)
        </label>
      </div>
      {dl.act.view}
      <LoadState s={data}>
        {(d) => {
          if (!d.day) return <p role="status">Chưa có dữ liệu.</p>;
          const regionName = (code: string) =>
            d.regions.find((r) => r.code === code)?.name ?? code;
          const sel = d.lines.find((l) => l.item.item_code === d.item);
          const trendMax = maxOf(
            d.trend.flatMap((t) => t.regions.map((r) => r.avg)),
          );
          const cols: Column<PriceDashboardLine>[] = [
            {
              key: "it",
              header: "Mặt hàng",
              cell: (r) => (
                <button
                  type="button"
                  className="link"
                  onClick={() => setItem(r.item.item_code)}
                >
                  {r.item.item_name}
                </button>
              ),
            },
            { key: "u", header: "ĐVT", cell: (r) => r.item.unit_name },
            { key: "avg", header: "Toàn tỉnh", cell: (r) => money(r.avg) },
            ...d.regions.map((rg) => ({
              key: `r-${rg.code}`,
              header: rg.name,
              cell: (r: PriceDashboardLine) =>
                money(r.regions.find((x) => x.region === rg.code)?.avg),
            })),
            { key: "tr", header: "So kỳ trước", cell: trendCell },
          ];
          return (
            <>
              <div
                className="row"
                style={{ gap: 8, flexWrap: "wrap", marginBottom: 12 }}
                aria-label="Thẻ chỉ số"
              >
                <Card label="Số mặt hàng tăng" value={d.cards.up} tone="ok" />
                <Card
                  label="Số mặt hàng giảm"
                  value={d.cards.down}
                  tone="bad"
                />
                <Card label="Số mặt hàng đi ngang" value={d.cards.flat} />
                <Card
                  label="Số xã có báo giá"
                  value={d.cards.points}
                  tone="info"
                />
              </div>
              <div
                className="row"
                style={{ gap: 8, marginBottom: 12, flexWrap: "wrap" }}
                aria-label="Nhóm vùng"
              >
                {d.regions.map((r) => (
                  <button
                    key={r.code}
                    type="button"
                    className="btn"
                    aria-pressed={region === r.code}
                    onClick={() => setRegion(region === r.code ? "" : r.code)}
                  >
                    {`${r.name} · ${r.points} xã`}
                  </button>
                ))}
              </div>
              <Table
                caption="Giá trung bình theo vùng"
                columns={cols}
                rows={d.lines}
                rowKey={(r) => r.item.item_code}
              />
              <section
                className="card"
                style={{ padding: 12, marginTop: 12 }}
                aria-label="Biến động theo nhóm hàng"
              >
                <strong>Biến động so kỳ trước theo nhóm hàng</strong>
                {d.groups.map((g) => (
                  <div key={g.code} className="row" style={{ gap: 8 }}>
                    <span style={{ minWidth: 160 }}>{g.name}</span>
                    <StatusPill tone="ok">{`▲ ${g.up}`}</StatusPill>
                    <StatusPill tone="bad">{`▼ ${g.down}`}</StatusPill>
                    <StatusPill tone="neutral">{`→ ${g.flat}`}</StatusPill>
                  </div>
                ))}
              </section>
              {sel ? (
                <section
                  className="card"
                  style={{ padding: 12, marginTop: 12 }}
                  aria-label="Xu hướng theo vùng"
                >
                  <strong>
                    Xu hướng giá trung bình theo vùng — {sel.item.item_name}
                  </strong>
                  {d.trend.map((t) =>
                    t.regions.map((r) => (
                      <Bar
                        key={`${t.day}:${r.region}`}
                        label={`${formatDateVi(t.day)} · ${regionName(r.region)}`}
                        value={r.avg}
                        max={trendMax}
                      />
                    )),
                  )}
                </section>
              ) : null}
              {region ? (
                <section
                  style={{ marginTop: 12 }}
                  aria-label="Giá theo xã trong vùng"
                >
                  <h3>Giá theo xã — {regionName(region)}</h3>
                  <Table
                    caption="Giá theo xã trong vùng"
                    columns={pointCols}
                    rows={d.drill}
                    rowKey={(r) => `${r.point_code}:${r.item_code}`}
                  />
                </section>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.08 — Thống kê phiếu báo giá (theo cán bộ Xã) + khóa tháng (Q-085)
// ---------------------------------------------------------------------------------------------------------------------

const RESULT_LABEL: Record<string, string> = {
  on_time: "Đúng hạn",
  late: "Nộp muộn",
  missed: "Không nộp",
};

export function StatsScreen() {
  const ra = usePriceReportApi();
  const [month, setMonth] = useState(vnToday().slice(0, 7));
  const [region, setRegion] = useState("");
  const [nonce, setNonce] = useState(0);
  const q = { month, region };
  const data = useLoad(() => ra.stats(q), [month, region, nonce]);
  const [who, setWho] = useState<PriceStatRow | null>(null);
  const detail = useLoad(
    who
      ? () =>
          ra.statsDetail({
            month,
            officer: who.officer_ref,
            point: who.point_code,
          })
      : null,
    [who, month],
  );
  const dl = useDownload();
  const act = useAction();
  const [reopen, setReopen] = useState(false);
  const [reason, setReason] = useState("");
  const cols: Column<PriceStatRow>[] = [
    {
      key: "who",
      header: "Cán bộ Xã",
      cell: (r) => (
        <button type="button" className="link" onClick={() => setWho(r)}>
          {r.officer_name ?? "Cán bộ Xã"}
        </button>
      ),
    },
    { key: "pt", header: "Xã", cell: (r) => r.point_name },
    { key: "rg", header: "Nhóm vùng", cell: (r) => r.region_name },
    { key: "req", header: "Phải nộp", cell: (r) => r.required },
    { key: "ok", header: "Đúng hạn", cell: (r) => r.on_time },
    { key: "late", header: "Nộp muộn", cell: (r) => r.late },
    { key: "miss", header: "Không nộp", cell: (r) => r.missed },
    { key: "del", header: "Bị xóa", cell: (r) => r.deleted },
    {
      key: "adj",
      header: "Số dòng bị hiệu chỉnh",
      cell: (r) => r.adjusted_lines,
    },
  ];
  return (
    <>
      <PageHeader
        title="Thống kê phiếu báo giá"
        badge={
          data.data?.snapshot ? (
            <StatusPill tone="info">Tháng đã khóa</StatusPill>
          ) : null
        }
        actions={
          <span className="row" style={{ gap: 6 }}>
            {data.data?.can_close ? (
              <button
                type="button"
                className="btn"
                disabled={act.busy}
                onClick={() =>
                  void act
                    .run(
                      () => ra.closeMonth(month, data.data!.month?.seq ?? 0),
                      "Đã khóa thống kê tháng",
                    )
                    .then(() => setNonce((n) => n + 1))
                }
              >
                Khóa tháng
              </button>
            ) : null}
            {data.data?.can_reopen ? (
              <button
                type="button"
                className="btn"
                onClick={() => setReopen(true)}
              >
                Mở lại tháng
              </button>
            ) : null}
            <button
              type="button"
              className="btn"
              disabled={dl.act.busy}
              onClick={() => void dl.run(() => ra.statsExport(q))}
            >
              Xuất Excel
            </button>
          </span>
        }
      >
        Tình hình gửi báo giá theo từng cán bộ Xã: phải nộp, đúng hạn, nộp muộn
        (gồm phiếu Cấp Tỉnh nhập thay), không nộp. Hệ thống chỉ thống kê, không
        tính tiền.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <label htmlFor="st-month">Tháng</label>
        <input
          id="st-month"
          type="month"
          className="input"
          style={{ width: 170 }}
          value={month}
          onChange={(e) => e.target.value && setMonth(e.target.value)}
        />
        <Select
          id="st-region"
          label="Nhóm vùng"
          value={region}
          onChange={setRegion}
          options={data.data?.regions ?? []}
        />
      </div>
      {act.view}
      {dl.act.view}
      <LoadState s={data}>
        {(d) => (
          <>
            <div
              className="row"
              style={{ gap: 8, flexWrap: "wrap", marginBottom: 12 }}
              aria-label="Thẻ chỉ số"
            >
              <Card
                label="Ngày báo giá"
                value={d.cards.quote_days}
                tone="info"
              />
              <Card
                label="Đúng hạn / phải nộp"
                value={`${d.cards.on_time} / ${d.cards.required}`}
                tone="ok"
              />
              <Card label="Nộp muộn" value={d.cards.late} tone="warn" />
              <Card label="Không nộp" value={d.cards.missed} tone="bad" />
            </div>
            {d.pending ? (
              <p className="muted">
                {d.pending} ngày báo giá chưa qua giờ khóa chưa được tính.
              </p>
            ) : null}
            <Table
              caption="Thống kê theo cán bộ Xã"
              columns={cols}
              rows={d.rows}
              rowKey={(r) => `${r.officer_ref}:${r.point_code}`}
            />
          </>
        )}
      </LoadState>
      {who ? (
        <Dialog
          title={`Phiếu theo ngày — ${who.officer_name ?? "Cán bộ Xã"}, ${who.point_name}`}
          onClose={() => setWho(null)}
        >
          <LoadState s={detail}>
            {(d) => (
              <Table
                caption="Phiếu theo ngày"
                columns={[
                  {
                    key: "d",
                    header: "Ngày",
                    cell: (r) => formatDateVi(r.day),
                  },
                  {
                    key: "r",
                    header: "Kết quả",
                    cell: (r) => RESULT_LABEL[r.result] ?? r.result,
                  },
                  {
                    key: "v",
                    header: "Trạng thái phiếu",
                    cell: (r) =>
                      TICKET_VIEW_LABEL[
                        r.view as keyof typeof TICKET_VIEW_LABEL
                      ] ?? r.view,
                  },
                ]}
                rows={d.days}
                rowKey={(r) => r.day}
              />
            )}
          </LoadState>
        </Dialog>
      ) : null}
      {reopen && data.data ? (
        <Dialog title="Mở lại thống kê tháng" onClose={() => setReopen(false)}>
          <label htmlFor="st-reason">Lý do (bắt buộc)</label>
          <textarea
            id="st-reason"
            className="input"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
          <button
            type="button"
            className="btn primary"
            disabled={!reason.trim() || act.busy}
            onClick={() =>
              void act
                .run(
                  () =>
                    ra.reopenMonth(
                      month,
                      data.data!.month?.seq ?? 0,
                      reason.trim(),
                    ),
                  "Đã mở lại thống kê tháng",
                )
                .then((r) => {
                  if (r) {
                    setReopen(false);
                    setReason("");
                    setNonce((n) => n + 1);
                  }
                })
            }
          >
            Mở lại
          </button>
        </Dialog>
      ) : null}
    </>
  );
}
