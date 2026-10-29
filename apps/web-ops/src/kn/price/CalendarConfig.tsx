"use client";

// BF-SWEEP-PRICE: danh sách cấu hình FN-AD-04.04–06 chỉ xem; thao tác nhập trên route riêng.
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useMemo, useState } from "react";
import type { ApiClient } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { RelatedTabs } from "@/components/patterns/detail";
import {
  DataTable,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import {
  createPriceConfigApi,
  DAY_KIND_LABEL,
  type PriceCalendar,
  type PriceCalendarDay,
  type PriceThreshold,
} from "./api";
import { configBase, thresholdPath } from "./calendar-routes";
import {
  dmy,
  hm,
  nextDay,
  RunsTable,
  ScheduleSummary,
  ThresholdSummary,
  WD_LABEL,
} from "./calendar-view";
export { dmy } from "./calendar-view";

export function PriceCalendarScreen() {
  const auth = useAuth();
  const base = configBase(usePathname());
  const session = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}`;
  return <CalendarList key={session} api={auth.api} base={base} />;
}

type Retire =
  | { kind: "exception"; day: string }
  | { kind: "threshold"; scope: "group" | "item"; code: string };

function CalendarList({ api, base }: { api: ApiClient; base: string }) {
  const pa = useMemo(() => createPriceConfigApi(api), [api]);
  const act = useAction();
  const cal = useLoad(() => pa.calendar(), [pa, act.note]);
  const th = useLoad(() => pa.thresholds(), [pa, act.note]);
  const runs = useLoad(() => pa.runs(), [pa, act.note]);
  const [retire, setRetire] = useState<Retire | null>(null);
  const canRetire =
    retire?.kind === "exception" ? cal.data?.can_manage : th.data?.can_manage;
  const confirm = async () => {
    if (!retire || !canRetire || act.busy) return;
    const result = await act.run(
      () =>
        retire.kind === "exception"
          ? pa.retireException(retire.day)
          : pa.retireThreshold(retire.scope, retire.code),
      "Đã xóa cấu hình được chọn.",
    );
    if (result !== null) setRetire(null);
  };
  return (
    <>
      <PageHeader title="Lịch báo giá, giờ khóa, nhắc nhập giá, ngưỡng cảnh báo">
        Báo giá nông sản — Phòng Thông tin – Truyền thông
      </PageHeader>
      {act.view}
      <LoadState s={cal}>
        {(c) => (
          <RelatedTabs
            label="Cấu hình báo giá"
            tabs={[
              {
                id: "lich",
                label: "Kỳ báo giá & giờ khóa",
                content: <ScheduleTab c={c} base={base} />,
              },
              {
                id: "ngoai-le",
                label: "Lịch ngoại lệ",
                content: (
                  <>
                    <div className="card sec">
                      <h2>Lịch ngoại lệ</h2>
                      <p className="mute">
                        Ngày nghỉ không sinh kỳ báo giá, không gửi nhắc, không
                        tính phiếu phải nộp. Chỉ thêm/xóa từ ngày mai.
                      </p>
                      {c.can_manage ? (
                        <Link
                          className="btn btn-primary"
                          href={`${base}/ngoai-le/them-moi`}
                        >
                          Thêm ngoại lệ
                        </Link>
                      ) : null}
                    </div>
                    <DataTable
                      caption="Lịch ngoại lệ"
                      rows={c.exceptions}
                      rowKey={(e) => e.day}
                      empty="Chưa có ngày ngoại lệ."
                      columns={[
                        {
                          key: "day",
                          header: "Ngày",
                          cell: (e) => (
                            <Link href={`${base}/ngoai-le/${e.day}`}>
                              {dmy(e.day)}
                            </Link>
                          ),
                        },
                        {
                          key: "kind",
                          header: "Loại",
                          cell: (e) =>
                            e.kind === "holiday"
                              ? "Ngày nghỉ"
                              : "Ngày báo giá bổ sung",
                        },
                        {
                          key: "reason",
                          header: "Lý do",
                          cell: (e) => e.reason,
                        },
                        {
                          key: "action",
                          header: "Thao tác",
                          cell: (e) =>
                            c.can_manage ? (
                              <button
                                className="btn btn-sm"
                                type="button"
                                disabled={
                                  act.busy || e.day < nextDay(c.today.day)
                                }
                                onClick={() =>
                                  setRetire({ kind: "exception", day: e.day })
                                }
                              >
                                Xóa
                              </button>
                            ) : (
                              "Chỉ xem"
                            ),
                        },
                      ]}
                    />
                  </>
                ),
              },
              {
                id: "nhac",
                label: "Nhắc nhập giá",
                content: (
                  <>
                    <div className="card sec">
                      <h2>Nhắc nhập giá</h2>
                      <p className="mute">
                        Lần 1 buổi sáng ngày báo giá; lần 2 trước giờ khóa, chỉ
                        xã chưa gửi. Thay đổi áp dụng từ ngày báo giá tiếp theo.
                      </p>
                      <div className="row">
                        <Link className="btn btn-o" href={`${base}/nhac`}>
                          Xem cấu hình nhắc
                        </Link>
                        {c.can_manage ? (
                          <Link
                            className="btn btn-primary"
                            href={`${base}/nhac/sua`}
                          >
                            Sửa cấu hình nhắc
                          </Link>
                        ) : null}
                      </div>
                      <ScheduleSummary value={c.upcoming.at(-1) ?? c.current} />
                    </div>
                    <h2>Lịch sử gửi nhắc</h2>
                    <LoadState s={runs}>
                      {(r) => (
                        <RunsTable
                          runs={r.items.filter((x) => x.kind !== "lock")}
                        />
                      )}
                    </LoadState>
                  </>
                ),
              },
              {
                id: "nguong",
                label: "Ngưỡng cảnh báo",
                content: (
                  <LoadState s={th}>
                    {(t) => {
                      const columns: Column<PriceThreshold>[] = [
                        {
                          key: "scope",
                          header: "Áp dụng cho",
                          cell: (x) => (
                            <Link
                              href={`${base}/${thresholdPath(x.scope, x.code)}`}
                            >
                              {x.scope === "group" ? "Nhóm hàng" : "Mặt hàng"}{" "}
                              {x.code}
                            </Link>
                          ),
                        },
                        { key: "pct", header: "Ngưỡng ±%", cell: (x) => x.pct },
                        {
                          key: "at",
                          header: "Cập nhật",
                          cell: (x) => dt(x.recorded_at),
                        },
                        {
                          key: "actor",
                          header: "Người cập nhật",
                          cell: (x) =>
                            x.actor_ref
                              ? `Mã cán bộ ${x.actor_ref}`
                              : "Chưa có thông tin trong API",
                        },
                        {
                          key: "action",
                          header: "Thao tác",
                          cell: (x) =>
                            t.can_manage && x.scope !== "default" ? (
                              <div className="row">
                                <Link
                                  href={`${base}/${thresholdPath(x.scope, x.code)}/sua`}
                                  className="btn btn-sm"
                                >
                                  Sửa
                                </Link>
                                <button
                                  type="button"
                                  className="btn btn-sm"
                                  disabled={act.busy}
                                  onClick={() =>
                                    setRetire({
                                      kind: "threshold",
                                      scope: x.scope as "group" | "item",
                                      code: x.code,
                                    })
                                  }
                                >
                                  Xóa
                                </button>
                              </div>
                            ) : (
                              "Chỉ xem"
                            ),
                        },
                      ];
                      return (
                        <>
                          <div className="card sec">
                            <h2>Ngưỡng mặc định và tham số chung</h2>
                            <p className="mute">
                              Ưu tiên: mặt hàng → nhóm hàng → mặc định. Ngưỡng
                              mới áp dụng từ lần gửi tiếp theo; cảnh báo đã sinh
                              giữ nguyên.
                            </p>
                            <div className="row">
                              <Link
                                className="btn btn-o"
                                href={`${base}/nguong/mac-dinh`}
                              >
                                Xem tham số chung
                              </Link>
                              {t.can_manage ? (
                                <Link
                                  className="btn btn-primary"
                                  href={`${base}/nguong/mac-dinh/sua`}
                                >
                                  Sửa tham số chung
                                </Link>
                              ) : null}
                            </div>
                            <ThresholdSummary value={t.default} />
                          </div>
                          {t.can_manage ? (
                            <p>
                              <Link
                                className="btn btn-primary"
                                href={`${base}/nguong/them-moi`}
                              >
                                Thêm ngưỡng
                              </Link>
                            </p>
                          ) : null}
                          <DataTable
                            caption="Ngưỡng theo nhóm hàng / mặt hàng"
                            columns={columns}
                            rows={t.items}
                            rowKey={(x) => `${x.scope}:${x.code}`}
                            empty="Chưa có ngưỡng riêng — áp ngưỡng mặc định."
                          />
                        </>
                      );
                    }}
                  </LoadState>
                ),
              },
              {
                id: "lich-su",
                label: "Lịch sử thay đổi",
                content: (
                  <>
                    <DataTable
                      caption="Lịch sử thay đổi lịch báo giá"
                      rows={c.history}
                      rowKey={(h) => String(h.version)}
                      empty="Chưa đổi — đang dùng cấu hình mặc định."
                      columns={[
                        {
                          key: "version",
                          header: "Phiên bản",
                          cell: (h) => h.version,
                        },
                        {
                          key: "from",
                          header: "Áp dụng từ",
                          cell: (h) => (h.valid_from ? dmy(h.valid_from) : "—"),
                        },
                        {
                          key: "days",
                          header: "Ngày báo giá",
                          cell: (h) =>
                            h.weekdays.map((w) => WD_LABEL[w]).join(", "),
                        },
                        {
                          key: "times",
                          header: "Khóa / phản hồi",
                          cell: (h) => `${h.lock_at} / ${h.respond_by}`,
                        },
                        {
                          key: "reminders",
                          header: "Nhắc",
                          cell: (h) => `${h.remind1_at}, ${h.remind2_at}`,
                        },
                        {
                          key: "at",
                          header: "Thời điểm ghi",
                          cell: (h) => dt(h.recorded_at),
                        },
                        {
                          key: "actor",
                          header: "Người ghi",
                          cell: (h) => `Mã cán bộ ${h.actor_ref}`,
                        },
                      ]}
                    />
                    <h2>Mốc khóa nhập thực tế</h2>
                    <LoadState s={runs}>
                      {(r) => (
                        <RunsTable
                          runs={r.items.filter((x) => x.kind === "lock")}
                        />
                      )}
                    </LoadState>
                  </>
                ),
              },
            ]}
          />
        )}
      </LoadState>
      {retire ? (
        <Dialog
          title="Xác nhận xóa cấu hình"
          onClose={() => !act.busy && setRetire(null)}
          actions={
            <button
              type="button"
              className="btn btn-primary"
              disabled={act.busy || !canRetire}
              onClick={() => void confirm()}
            >
              Xác nhận xóa
            </button>
          }
        >
          <p>
            {retire.kind === "exception"
              ? `Xóa ngoại lệ ngày ${dmy(retire.day)}?`
              : `Xóa ngưỡng riêng ${retire.code} để dùng ngưỡng ưu tiên còn lại?`}
          </p>
        </Dialog>
      ) : null}
    </>
  );
}

function ScheduleTab({ c, base }: { c: PriceCalendar; base: string }) {
  const [page, setPage] = useState(1);
  const columns: Column<PriceCalendarDay>[] = [
    {
      key: "day",
      header: "Ngày",
      cell: (d) => `${WD_LABEL[d.weekday]} ${dmy(d.day)}`,
    },
    {
      key: "kind",
      header: "Loại",
      cell: (d) => (
        <StatusPill
          tone={d.kind === "holiday" ? "warn" : d.quote ? "ok" : "neutral"}
        >
          {DAY_KIND_LABEL[d.kind]}
        </StatusPill>
      ),
    },
    { key: "lock", header: "Giờ khóa", cell: (d) => hm(d.lock_at) },
    { key: "respond", header: "Hạn phản hồi", cell: (d) => hm(d.respond_by) },
    { key: "reason", header: "Lý do", cell: (d) => d.reason || "—" },
  ];
  return (
    <div className="sec">
      <div className="card sec">
        <h2>Lịch báo giá hằng ngày</h2>
        <p className="mute">
          Hôm nay: {DAY_KIND_LABEL[c.today.kind]}
          {c.today.quote
            ? c.locked
              ? " — đã khóa nhập."
              : ` — khóa lúc ${hm(c.today.lock_at)}.`
            : "."}
        </p>
        <div className="row">
          <Link className="btn btn-o" href={`${base}/lich`}>
            Xem kỳ báo giá & giờ khóa
          </Link>
          {c.can_manage ? (
            <Link className="btn btn-primary" href={`${base}/lich/sua`}>
              Sửa kỳ báo giá
            </Link>
          ) : null}
        </div>
        <ScheduleSummary value={c.current} />
        {c.upcoming.map((u) => (
          <p key={u.version} className="mute">
            Đã lên lịch: phiên bản {u.version} từ{" "}
            {u.valid_from ? dmy(u.valid_from) : "—"} — khóa {u.lock_at}, hạn
            phản hồi {u.respond_by}.
          </p>
        ))}
        {!c.can_manage ? (
          <p className="mute">
            Chỉ xem — cấu hình do Quản trị nghiệp vụ báo giá thực hiện.
          </p>
        ) : null}
      </div>
      <DataTable
        caption="Lịch khóa nhập 30 ngày tới"
        columns={columns}
        rows={c.days.slice((page - 1) * 10, page * 10)}
        rowKey={(d) => d.day}
        page={page}
        pageSize={10}
        total={c.days.length}
        onPage={setPage}
      />
    </div>
  );
}
