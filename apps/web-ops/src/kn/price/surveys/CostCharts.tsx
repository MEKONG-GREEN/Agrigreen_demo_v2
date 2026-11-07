import { ColumnChart } from "@/components/BarChart";
import { COST_KEYS, COST_LABEL, type SurveyCostBlock } from "./api";

const COLORS = ["#1b8a54", "#347c9d", "#9064ad", "#b76d21", "#637846", "#a34a61"];
const costSeries = COST_KEYS.map((key, i) => ({ key, label: COST_LABEL[key]!, color: COLORS[i]! }));
const priceSeries = [
  { key: "cost", label: "Giá thành bình quân", color: "#347c9d" },
  { key: "price", label: "Giá bán bình quân", color: "#1b8a54" },
];
const sample = (n: number) => `${n} phiếu${n < 3 ? " · Mẫu nhỏ" : ""}`;

/** Dùng nguyên aggregate của bảng; không tải phiếu nông hộ hay bảng giá hằng ngày. */
export function CostCharts({ block }: { block: SurveyCostBlock }) {
  return <>
    <ColumnChart title={`Cơ cấu chi phí bình quân — ${block.round.name}`} unit="đồng/phiếu" stacked series={costSeries}
      data={block.rows.map((r) => ({ key: r.item, label: r.item_name, note: sample(r.n), values: r.avg_costs }))} />
    <ColumnChart title={`Giá thành – giá bán theo vùng — ${block.round.name}`} unit="đồng/kg" series={priceSeries}
      data={block.regions.map((r) => ({ key: `${r.item}:${r.region}`, label: `${r.item_name} · ${r.region_name}`, note: sample(r.n), values: { cost: r.avg_cost_per_kg, price: r.avg_price } }))} />
  </>;
}
