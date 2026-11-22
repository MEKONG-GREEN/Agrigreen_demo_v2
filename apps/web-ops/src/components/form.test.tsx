import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Decimal } from "@/lib/exactDecimal";
import { NumberField, parseNumberInput, SelectField, TextField, validateForm } from "./form";

describe("parseNumberInput (R-A1)", () => {
  it("chặn số mũ và chuỗi vượt giới hạn trước bộ đọc decimal (BF-SWEEP-HCTH-REST)", () => {
    const parse = vi.spyOn(Decimal, "parse");
    try {
      for (const value of ["1e100000", "1e-100000", "1e1000", "9".repeat(100000)]) {
        expect(parseNumberInput(value, { maxInputLength: 32 }).ok).toBe(false);
      }
      expect(parse).not.toHaveBeenCalled();
    } finally { parse.mockRestore(); }
  });
  it("không nhận số âm theo mặc định, kể cả dạng vi-VN", () => {
    expect(parseNumberInput("-1")).toEqual({ ok: false, error: "Không được nhập số âm." });
    expect(parseNumberInput("-0,5")).toEqual({ ok: false, error: "Không được nhập số âm." });
    expect(parseNumberInput("1.234,5")).toEqual({ ok: true, value: "1234.5" });
    expect(parseNumberInput("0")).toEqual({ ok: true, value: "0" });
  });

  it("bắt buộc, số nguyên, > 0, giá trị lớn nhất, số lẻ", () => {
    expect(parseNumberInput("", { required: true })).toEqual({ ok: false, error: "Bắt buộc nhập." });
    expect(parseNumberInput("")).toEqual({ ok: true, value: "" });
    expect(parseNumberInput("2,5", { integer: true }).ok).toBe(false);
    expect(parseNumberInput("0", { positive: true })).toEqual({ ok: false, error: "Phải lớn hơn 0." });
    expect(parseNumberInput("2031", { integer: true, max: 2026 })).toEqual({ ok: false, error: "Không được lớn hơn 2026." });
    expect(parseNumberInput("1,234", { maxDecimals: 2 }).ok).toBe(false);
    expect(parseNumberInput("abc")).toEqual({ ok: false, error: "Không phải số hợp lệ." });
    expect(parseNumberInput("-3", { allowNegative: true })).toEqual({ ok: true, value: "-3" });
  });

  it("so sánh đủ độ chính xác, không cắt ở chữ số thập phân thứ 7 (review Codex #97)", () => {
    expect(parseNumberInput("-0.0000001").ok).toBe(false);
    expect(parseNumberInput("0.0000001", { positive: true }).ok).toBe(true);
    expect(parseNumberInput("1.0000001", { max: "1" }).ok).toBe(false);
    expect(parseNumberInput("1.0000000", { max: "1" }).ok).toBe(true);
    expect(parseNumberInput("-0,00000000001")).toEqual({ ok: false, error: "Không được nhập số âm." });
  });
});

describe("validateForm (R-A4)", () => {
  it("báo lỗi theo từng trường và chuẩn hóa số", () => {
    const r = validateForm(
      { ten: "", xa: "x1", so_luong: "-2", dien_tich: "1,5" },
      {
        ten: { kind: "text", required: true },
        xa: { kind: "text", required: true },
        so_luong: { kind: "number", required: true, integer: true, positive: true },
        dien_tich: { kind: "number" },
      },
    );
    expect(r.valid).toBe(false);
    expect(r.errors).toEqual({ ten: "Bắt buộc nhập.", so_luong: "Không được nhập số âm." });
    expect(r.values.dien_tich).toBe("1.5");
  });
});

describe("ô nhập", () => {
  it("trường bắt buộc có dấu * và lỗi gắn với ô qua aria-describedby", () => {
    render(<TextField label="Tên HTX" value="" onChange={() => undefined} required error="Bắt buộc nhập." />);
    const input = screen.getByRole("textbox", { name: /Tên HTX/ });
    expect(input).toHaveAttribute("aria-required", "true");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("*")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Bắt buộc nhập.");
    expect(input.getAttribute("aria-describedby")).toBe(screen.getByRole("alert").id);
  });

  it("ô số báo lỗi số âm khi rời ô", async () => {
    const user = userEvent.setup();
    function Host() {
      const [v, setV] = useState("");
      const [err, setErr] = useState<string>();
      return <NumberField label="Số lượng" unit="chiếc" value={v} onChange={setV} required rule={{ integer: true }} error={err} onBlurError={setErr} />;
    }
    render(<Host />);
    const input = screen.getByRole("textbox", { name: /Số lượng \(chiếc\)/ });
    expect(input).toHaveAttribute("inputmode", "numeric");
    await user.type(input, "-5");
    await user.tab();
    expect(screen.getByRole("alert")).toHaveTextContent("Không được nhập số âm.");
  });

  it("ô chọn có lựa chọn rỗng đầu tiên", () => {
    render(<SelectField label="Loại" value="" onChange={() => undefined} options={[{ value: "htx", label: "HTX" }]} required />);
    const sel = screen.getByRole("combobox", { name: /Loại/ });
    expect(sel).toHaveValue("");
    expect(screen.getByRole("option", { name: "HTX" })).toBeInTheDocument();
  });
});
