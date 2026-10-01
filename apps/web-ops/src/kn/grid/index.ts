// API công khai của bộ vẽ lưới mẫu báo cáo (P3b) — P4 (màn nhập Cấp Xã, tổng hợp Cấp Tỉnh) dùng lại:
//
//   <FormEntry definition versionKey communes values onValuesChange evaluate openSlots system />
//       màn nhập hoàn chỉnh: mỗi xã một tab + tab "Tất cả các xã" (chỉ xem, dòng tổng), vấn đề tại ô.
//   <FormGrid definition rows values computed openSlots issues onChange … />
//       lưới đúng bố cục Excel (tự chọn dòng: communeRows / allCommuneRows), bàn phím + dán khối từ Excel.
//   useFormEvaluation({definition, communes, values, evaluate}) → { computed, result, pending, problem }
//       tính trên máy chủ, giãn cách; evaluate = formEvaluator(api, versionId) (src/kn/forms/api.ts).
//
// Giá trị ô là CHUỖI: số decimal dạng máy chủ ("38.5"), ngày "YYYY-MM-DD". Hiển thị/nhập vi-VN ("38,5").
export { FormGrid, GridLegend, type FormGridProps } from "./FormGrid";
export { FormEntry, applyChanges, ALL_TAB, type FormEntryProps } from "./FormEntry";
export { useFormEvaluation, type FormEvaluator, type FormEvaluationState } from "./useFormEvaluation";
export {
  allCommuneRows,
  applyPaste,
  buildEvalInput,
  buildHeader,
  cellRole,
  communeRows,
  computedCells,
  displayCell,
  editText,
  isEditableRole,
  ISSUE_LABEL,
  normalizeCellInput,
  parseClipboard,
  rowKeyOf,
  type CellChange,
  type CellRole,
  type GridCommune,
  type GridRow,
  type GridValues,
} from "./model";
export { Decimal, formatDecimalVi, normalizeNumberInput } from "./decimal";
export { columnIndex, columnLetter } from "./letters";
