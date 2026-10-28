import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AlertList, deltaPct, formatDelta, HotMetric, progressColor, sparkPath } from "./dashboard";
import { BreadcrumbTailProvider, useBreadcrumbTailValue } from "@/shell/Breadcrumb";
import { DetailPage, LabelValueBlock, LogPanel, RelatedTabs } from "./detail";
import { DataTable, FilterBar, FilterChip, pageItems, rangeText, RowActions } from "./list";

describe("List", () => {
  it("số trang: 1 2 3 … 10, lân cận trang hiện tại", () => {
    expect(pageItems(1, 10)).toEqual([1, 2, 3, "gap", 10]);
    expect(pageItems(5, 10)).toEqual([1, "gap", 4, 5, 6, "gap", 10]);
    expect(pageItems(10, 10)).toEqual([1, "gap", 8, 9, 10]);
    expect(pageItems(2, 4)).toEqual([1, 2, 3, 4]);
  });

  it("Hiển thị a–b trên n", () => {
    expect(rangeText(1, 10, 23)).toBe("Hiển thị 1–10 trên 23");
    expect(rangeText(3, 10, 23)).toBe("Hiển thị 21–23 trên 23");
    expect(rangeText(1, 10, 0)).toBe("Không có dòng nào");
  });

  it("bảng có STT theo trang, phân trang bấm được", async () => {
    const user = userEvent.setup();
    const onPage = vi.fn();
    render(
      <DataTable
        caption="Thử"
        columns={[{ key: "n", header: "Tên", cell: (r: { n: string }) => r.n }]}
        rows={[{ n: "a" }, { n: "b" }]}
        rowKey={(r) => r.n}
        page={2}
        pageSize={2}
        total={5}
        onPage={onPage}
      />,
    );
    const rows = screen.getAllByRole("row");
    expect(within(rows[0]!).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(rows[1]!).getAllByRole("cell")[0]).toHaveTextContent("3");
    expect(screen.getByRole("button", { name: "Trang 2" })).toHaveAttribute("aria-current", "page");
    await user.click(screen.getByRole("button", { name: "Trang sau" }));
    expect(onPage).toHaveBeenCalledWith(3);
  });

  it("bảng không phân trang (thay <table> tự viết) vẫn có STT từ 1, không có thanh trang", () => {
    render(<DataTable caption="Thử" columns={[{ key: "n", header: "Tên", cell: (r: { n: string }) => r.n }]} rows={[{ n: "a" }, { n: "b" }]} rowKey={(r) => r.n} />);
    const rows = screen.getAllByRole("row");
    expect(within(rows[2]!).getAllByRole("cell")[0]).toHaveTextContent("2");
    expect(screen.queryByRole("navigation")).toBeNull();
  });

  it("thẻ lọc tô xanh khi khác mặc định; Đặt lại", async () => {
    const user = userEvent.setup();
    const onReset = vi.fn();
    const onChange = vi.fn();
    render(
      <FilterBar onReset={onReset}>
        <FilterChip label="Trạng thái" value="draft" onChange={onChange} options={[{ value: "", label: "Tất cả" }, { value: "draft", label: "Lưu nháp" }]} />
      </FilterBar>,
    );
    const select = screen.getByRole("combobox", { name: "Trạng thái" });
    expect(select.closest(".fchip")).toHaveClass("is-set");
    await user.selectOptions(select, "");
    expect(onChange).toHaveBeenCalledWith("");
    await user.click(screen.getByRole("button", { name: "Đặt lại" }));
    expect(onReset).toHaveBeenCalled();
  });

  it("xem/sửa/xóa; chỉ xem khi không truyền sửa/xóa (Cấp Quốc gia)", () => {
    const { rerender } = render(<RowActions label="HTX A" viewHref="/x" editHref="/x?sua=1" onDelete={() => undefined} />);
    expect(screen.getByRole("link", { name: "Xem HTX A" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sửa HTX A" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Xóa HTX A" })).toBeInTheDocument();
    rerender(<RowActions label="HTX A" viewHref="/x" />);
    expect(screen.queryByRole("link", { name: "Sửa HTX A" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Xóa HTX A" })).toBeNull();
  });
});

describe("Detail", () => {
  it("DetailPage (R-A2): ← quay lại, tiêu đề h1, nút góc phải, mắt xích breadcrumb; không có Log thì một khối", () => {
    function Tail() {
      return <output aria-label="tail">{useBreadcrumbTailValue().map((c) => c.label).join(" / ")}</output>;
    }
    render(
      <BreadcrumbTailProvider>
        <Tail />
        <DetailPage back={{ href: "/kn/du-lieu/htx", label: "Danh sách HTX" }} title="Thêm HTX" actions={<button type="button">Lưu</button>}>
          <p>Nội dung form</p>
        </DetailPage>
      </BreadcrumbTailProvider>,
    );
    expect(screen.getByRole("link", { name: "Danh sách HTX" })).toHaveAttribute("href", "/kn/du-lieu/htx");
    expect(screen.getByRole("heading", { level: 1, name: "Thêm HTX" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lưu" })).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "tail" })).toHaveTextContent("Thêm HTX");
    expect(document.querySelector(".detail-grid")).toBeNull();
  });

  it("khối Nhãn – Giá trị, giá trị trống → —", () => {
    render(<LabelValueBlock title="Nội dung 1" items={[{ label: "Tên", value: "HTX A" }, { label: "Ghi chú", value: "" }]} />);
    expect(screen.getByRole("heading", { name: "Nội dung 1" })).toBeInTheDocument();
    expect(screen.getByText("Ghi chú").nextSibling).toHaveTextContent("—");
  });

  it("khung Log: sự kiện, người · thời điểm, cũ → mới, lý do", () => {
    render(
      <LogPanel
        entries={[{ event: "Cập nhật diện tích", actor_display: "Nguyễn Văn An", at: "2026-09-07T03:20:00Z", changes: [{ field: "Diện tích", old: "118", new: "120" }], reason: "Đo lại" }]}
      />,
    );
    const log = screen.getByRole("complementary", { name: "Log" });
    expect(log).toHaveTextContent("Cập nhật diện tích");
    expect(log).toHaveTextContent(/Nguyễn Văn An · .*07\/09\/2026/);
    expect(log).toHaveTextContent("Diện tích: 118 → 120");
    expect(log).toHaveTextContent("Lý do: Đo lại");
  });

  it("tab liên quan", async () => {
    const user = userEvent.setup();
    render(
      <RelatedTabs
        tabs={[
          { id: "a", label: "Liên quan 1", content: <p>Nội dung A</p> },
          { id: "b", label: "Liên quan 2", content: <p>Nội dung B</p> },
        ]}
      />,
    );
    expect(screen.getByText("Nội dung A")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Liên quan 2" }));
    expect(screen.getByRole("tab", { name: "Liên quan 2" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Nội dung B")).toBeInTheDocument();
  });
});

describe("Dashboard", () => {
  it("sparkline, delta, màu tiến độ theo ngưỡng", () => {
    expect(sparkPath([1]).line).toBe("");
    expect(sparkPath([0, 10]).line).toBe("M0.0 36.0 L100.0 8.0");
    expect(formatDelta(deltaPct(106.2, 100))).toBe("+6,2%");
    expect(formatDelta(deltaPct(95.9, 100))).toBe("−4,1%");
    expect(progressColor(90)).toBe("var(--primary)");
    expect(progressColor(35)).toBe("var(--chart-orange)");
    expect(progressColor(60)).toBe("var(--chart-blue)");
  });

  it("thẻ Chỉ số nóng và danh sách Cảnh báo theo mức", () => {
    render(
      <>
        <HotMetric label="DT xuống giống lúa (ha)" value="72.410" delta="+6,2% so kỳ trước" series={[1, 2, 3]} />
        <AlertList
          alerts={[
            { id: "1", level: "urgent", title: "Rầy nâu tăng mạnh", at: "2026-09-07T01:15:00Z", href: "/x" },
            { id: "2", level: "info", title: "Đã cập nhật tổng hợp", at: "2026-09-08T02:15:00Z" },
          ]}
          allHref="/thong-bao"
        />
      </>,
    );
    expect(screen.getByText("72.410")).toBeInTheDocument();
    const alerts = screen.getByRole("region", { name: "Cảnh báo" });
    expect(within(alerts).getByText("Khẩn")).toBeInTheDocument();
    expect(within(alerts).getByText("Thông tin")).toBeInTheDocument();
    expect(within(alerts).getByText("2")).toHaveClass("count-pill");
    expect(within(alerts).getByRole("link", { name: "Xem: Rầy nâu tăng mạnh" })).toHaveAttribute("href", "/x");
    expect(within(alerts).getByRole("link", { name: "Xem tất cả" })).toHaveAttribute("href", "/thong-bao");
  });
});
