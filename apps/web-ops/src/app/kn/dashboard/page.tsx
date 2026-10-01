"use client";

import { useState } from "react";
import { AlertList, HotMetric, PeriodCompare, ProgressList, SectionTitle } from "@/components/patterns/dashboard";
import { FilterBar, FilterChip } from "@/components/patterns/list";
import { useAuth } from "@/auth/AuthProvider";
import { DEMO_ALERTS, DEMO_COMPARE, DEMO_HOT, DEMO_PROGRESS } from "@/kn/demo";
import { DemoNotice, Greeting, RequiresMock } from "@/kn/ui";

const DEFAULTS = { time: "week", region: "", unit: "", crop: "" };

/** Mẫu Dashboard (FN-TW-01.01 / FN-TI-02.01, Hình 3d/3e) — dữ liệu minh họa ở đợt 1. */
export default function KnDashboardPage() {
  const { kn } = useAuth();
  const [f, setF] = useState(DEFAULTS);
  const isProvince = kn.context?.role === "kn_tinh";
  return (
    <RequiresMock title="Dashboard">
      <Greeting />
      <DemoNotice />
      <FilterBar onReset={() => setF(DEFAULTS)}>
        <FilterChip
          label="Mốc thời gian"
          value={f.time}
          defaultValue="week"
          onChange={(time) => setF({ ...f, time })}
          options={[
            { value: "week", label: "Tuần" },
            { value: "month", label: "Tháng" },
            { value: "season", label: "Vụ" },
          ]}
        />
        {isProvince ? (
          <FilterChip
            label="Khu vực"
            value={f.region}
            onChange={(region) => setF({ ...f, region })}
            options={[{ value: "", label: "Tất cả" }, ...DEMO_PROGRESS.map((p) => ({ value: p.label, label: p.label }))]}
          />
        ) : (
          <FilterChip label="Tỉnh" value="91" defaultValue="" onChange={() => undefined} options={[{ value: "91", label: "An Giang" }]} />
        )}
        <FilterChip
          label="Loại cây"
          value={f.crop}
          onChange={(crop) => setF({ ...f, crop })}
          options={[
            { value: "", label: "Tất cả" },
            { value: "lua", label: "Lúa" },
            { value: "rau-mau", label: "Rau màu" },
            { value: "cay-an-trai", label: "Cây ăn trái" },
          ]}
        />
      </FilterBar>
      <SectionTitle>Chỉ số nóng</SectionTitle>
      <div className="hot-grid">
        {DEMO_HOT.map((h) => (
          <HotMetric key={h.label} label={h.label} value={h.value} delta={h.delta} trend={h.trend} series={h.series} tone={h.tone} />
        ))}
      </div>
      <SectionTitle>Chỉ số so sánh</SectionTitle>
      <div className="cmp-grid">
        <ProgressList title="Tiến độ xuống giống theo khu vực" items={DEMO_PROGRESS} />
        <PeriodCompare title="So sánh kỳ" items={DEMO_COMPARE} />
      </div>
      <AlertList alerts={DEMO_ALERTS} allHref="/thong-bao" />
    </RequiresMock>
  );
}
