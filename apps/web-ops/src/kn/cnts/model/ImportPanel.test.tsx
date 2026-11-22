// W11.53c, #176 review P2: kết quả xem trước của tệp A trả muộn không được bật Ghi cho tệp B chưa xem trước.
import { act as rtlAct, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ImportPanel } from "./ImportPanel";
import type { CntsModelApi, ImportResult } from "./api";

const P = "0190f1c4-7d3e-7000-8000-0000000000d1";
const result = (over: Partial<ImportResult> = {}): ImportResult => ({
  project_ref: P, project_code: "DA-X", valid: true, committed: false, replayed: false, site_count: 1, household_count: 0,
  sites: [], households: [], issues: [], issues_truncated: false, site_refs: [], ...over,
});
const xlsx = (name: string, bytes: number[]) => new File([new Uint8Array([0x50, 0x4b, ...bytes])], name, { type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" });
const bytesOf = async (f: File) => Array.from(new Uint8Array(await f.arrayBuffer()));

function harness() {
  const previews: { file: File; resolve: (r: ImportResult) => void }[] = [];
  const commits: File[] = [];
  const mapi = {
    importTemplate: vi.fn(),
    previewImport: vi.fn((_ref: string, file: File) => new Promise<ImportResult>((resolve) => previews.push({ file, resolve }))),
    commitImport: vi.fn(async (_ref: string, file: File) => (commits.push(file), result({ committed: true }))),
  } as unknown as CntsModelApi;
  render(<ImportPanel mapi={mapi} projectRef={P} projectCode="DA-X" onDone={() => undefined} />);
  const input = () => screen.getByLabelText(/Tệp Excel nạp/) as HTMLInputElement;
  const choose = async (f: File) => {
    fireEvent.change(input(), { target: { files: [f] } });
    await screen.findByText(f.name);
  };
  const ghi = () => screen.getByRole("button", { name: "Ghi vào dự án" });
  return { previews, commits, input, choose, ghi };
}

describe("ImportPanel — xem trước gắn đúng tệp đã chụp", () => {
  it("đối chứng: xem trước A hợp lệ → Ghi gửi đúng A", async () => {
    const h = harness();
    await h.choose(xlsx("A.xlsx", [1]));
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    await waitFor(() => expect(h.previews).toHaveLength(1));
    await rtlAct(async () => h.previews[0]!.resolve(result()));
    await waitFor(() => expect(h.ghi()).toBeEnabled());
    fireEvent.click(h.ghi());
    await waitFor(() => expect(h.commits).toHaveLength(1));
    expect(await bytesOf(h.commits[0]!)).toEqual([0x50, 0x4b, 1]);
  });

  it("race: xem trước A đang chạy → chọn B → phản hồi A về muộn: không bật Ghi, không gửi B chưa xem trước", async () => {
    const h = harness();
    await h.choose(xlsx("A.xlsx", [1]));
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    await waitFor(() => expect(h.previews).toHaveLength(1));
    expect(h.input()).toBeDisabled(); // khóa đổi tệp khi đang xem trước
    // Cưỡng bức đổi tệp (ô bị khóa vẫn nhận sự kiện change giả lập): phản hồi A phải bị bỏ.
    fireEvent.change(h.input(), { target: { files: [xlsx("B.xlsx", [2])] } });
    await rtlAct(async () => h.previews[0]!.resolve(result()));
    await waitFor(() => expect(screen.getByRole("button", { name: "Xem trước" })).toBeEnabled());
    expect(h.ghi()).toBeDisabled();
    fireEvent.click(h.ghi());
    expect(h.commits).toHaveLength(0);
    expect(screen.queryByText("Ghi được")).toBeNull();
    // B phải được xem trước riêng rồi mới ghi; Ghi gửi đúng B.
    await screen.findByText("B.xlsx");
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    await waitFor(() => expect(h.previews).toHaveLength(2));
    expect(await bytesOf(h.previews[1]!.file)).toEqual([0x50, 0x4b, 2]);
    await rtlAct(async () => h.previews[1]!.resolve(result()));
    await waitFor(() => expect(h.ghi()).toBeEnabled());
    fireEvent.click(h.ghi());
    await waitFor(() => expect(h.commits).toHaveLength(1));
    expect(await bytesOf(h.commits[0]!)).toEqual([0x50, 0x4b, 2]);
  });
});
