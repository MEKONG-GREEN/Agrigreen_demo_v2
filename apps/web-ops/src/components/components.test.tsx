import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { coverageStatus } from "@/cgh/coverage";
import { CoverageChip, CoverageLegend } from "./CoverageLegend";
import { ProblemAlert } from "./ProblemAlert";
import { ProvisionalBadge } from "./ProvisionalBadge";
import { SlaCountdown } from "./SlaCountdown";
import { KpiTile } from "./ui";

describe("<ProblemAlert>", () => {
  it("hiện thông báo tiếng Việt, mã lỗi, trace_id và lỗi theo trường", () => {
    const p = new ApiProblem({
      type: "https://errors.agrigreen.vn/validation_failed",
      title: "Validation failed",
      status: 422,
      code: "validation_failed",
      trace_id: "trace-42",
      errors: [{ field: "source_doc", reason: "Cần số văn bản làm căn cứ" }],
    });
    render(<ProblemAlert problem={p} />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Dữ liệu chưa hợp lệ");
    expect(alert).toHaveTextContent("validation_failed");
    expect(screen.getByTestId("trace-id")).toHaveTextContent("trace-42");
    expect(alert).toHaveTextContent("Cần số văn bản làm căn cứ");
  });
});

describe("<KpiTile> / <ProvisionalBadge>", () => {
  it("thiếu dữ liệu → hiện — kèm chú thích cho trình đọc màn hình", () => {
    render(<KpiTile color="green" icon="area" label="Diện tích đã triển khai" value={null} unit="ha" />);
    const tile = screen.getByRole("group", { name: "Diện tích đã triển khai" });
    expect(tile).toHaveTextContent("—");
    expect(tile).toHaveTextContent("Chưa có dữ liệu");
  });

  it("huy hiệu Số liệu tạm tính", () => {
    render(<ProvisionalBadge missing={2} total={6} />);
    expect(screen.getByRole("status")).toHaveTextContent("Số liệu tạm tính");
    expect(screen.getByRole("status")).toHaveTextContent("còn 2 trên 6 đơn vị");
  });
});

describe("<CoverageChip> / <CoverageLegend>", () => {
  it.each([
    ["59.99", "Thiếu"],
    ["60", "Cần chú ý"],
    ["85", "Đủ"],
    ["120", "Thừa"],
  ])("%s%% hiện nhãn %s", (pct, label) => {
    render(<CoverageChip status={coverageStatus(pct)} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("chú giải đủ 5 trạng thái, gồm Chưa có dữ liệu", () => {
    render(<CoverageLegend thresholds={{ sufficient_pct: "85", attention_pct: "60", surplus_pct: "120" }} />);
    for (const l of ["Đủ", "Cần chú ý", "Thiếu", "Thừa", "Chưa có dữ liệu"]) expect(screen.getByText(l)).toBeInTheDocument();
    expect(screen.getByText("≥ 120%")).toBeInTheDocument();
  });
});

describe("<SlaCountdown>", () => {
  const now = Date.parse("2026-09-27T08:00:00Z");
  it("còn hạn", () => {
    render(<SlaCountdown dueAt="2026-09-28T10:30:00Z" now={now} />);
    expect(screen.getByText("Còn 1 ngày 2 giờ")).toHaveClass("c-ok");
  });
  it("sắp hết hạn", () => {
    render(<SlaCountdown dueAt="2026-09-27T10:15:00Z" now={now} />);
    expect(screen.getByText("Còn 2 giờ 15 phút")).toHaveClass("c-warn");
  });
  it("quá hạn", () => {
    render(<SlaCountdown dueAt="2026-09-27T05:00:00Z" now={now} />);
    expect(screen.getByText("Quá hạn 3 giờ")).toHaveClass("c-bad");
  });
  it("đã xử lý trễ", () => {
    render(<SlaCountdown dueAt="2026-09-27T05:00:00Z" resolvedAt="2026-09-27T06:30:00Z" now={now} />);
    expect(screen.getByText("Xử lý trễ 1 giờ 30 phút")).toBeInTheDocument();
  });
});
