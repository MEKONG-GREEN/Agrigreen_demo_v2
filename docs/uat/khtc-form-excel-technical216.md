# KHTC v2.3 — kiểm chứng codec kỹ thuật #216

Căn cứ: FN-PN-03.02, BR-DL-20, BR-DL-11/12 và FN-AD-04.02. Đây là kiểm chứng supplier kỹ thuật, **không phải UAT hoàn tất chức năng**. [Phạm vi và hợp đồng](../kn/gia-dinh/KHTC-PN-FORM-EXCEL.md).

| Kịch bản | Native test |
|---|---|
| Workbook thật, giữ chữ cái và Mã hạng mục hiển thị, chữ bắt đầu `=` là literal | TestGeneratorKeepsLettersVisibleCodesAndLiteralText |
| Đổi vị trí/chèn dòng, tên trùng, lời văn theo section ID | TestRoundTripMatchesItemCodeAndKeepsUnsavedValues; TestInsertedRowsAndNarrativeUseStableIdentities |
| Ô khóa/system/formula bị sửa: bỏ qua; editable formula: lỗi dòng, không evaluate | TestLockedChangesAndFormulasAreIgnoredWithoutEvaluation; TestEditableFormulasExcludeOnlyTheirRow |
| Dòng sai không loại dòng đúng; required/choice/Unicode length | TestInvalidRowDoesNotExcludeValidRows; TestRequiredChoiceAndUnicodeLength |
| Số mũ cực trị, integer rất dài, giới hạn chữ số/lẻ, giữ chính xác chuỗi số | TestNumericBoundsBeforeParsingAndExactPrecision |
| Mã trùng/ngoài phạm vi, sai sheet/cột/snapshot/kỳ/phòng/version | TestDuplicateAndUnknownCodesCannotEnterTheEditor; TestWrongWorkbookIdentityOrShapeFailsWholeFile |
| Keep/replace từng ô, giữ dữ liệu manual; preview opaque/foreign; deepcopy snapshot/input/output/upload | TestMergeKeepReplaceOpaquePreviewAndDetachedState |
| Điều chỉnh có kỳ đóng, lý do, số khác 0; giữ partial valid, keep/replace nhóm | TestAdjustmentsRequireAllowedPeriodReasonAndPreservePartialRows |
| ZIP/XML/cell/rows/size, DTD/entity, macro/external/duplicate/path traversal | TestHostilePackagesAndEarlyResourceBounds; TestPreflightAcceptsGeneratedXLSX |
| Formula-only extra cells, marker literal và integer âm 0 theo manual | TestFormulaOnlyExtraCellsCannotBypassShapeValidation; TestLiteralMarkerTextAndIntegerNegativeZeroFollowFieldRules |
| Formula subset rõ ràng; expression unsupported không fallback | TestSnapshotBoundsAndUnsupportedFormulaAreExplicit |
| Xuất diagnostics không chứa raw value/formula, gọi đồng thời không chung trạng thái sửa được | TestErrorExportAndConcurrentCallsDoNotShareMutableState |

RED gốc của hai test roundtrip/partial-valid FAIL ở Generate chưa có codec; sau triển khai chạy lại native. Các thất bại trung gian được bảo toàn, không sửa assertion/budget để xanh. Packet cung cấp SHA/log/inventory, controlled fault FAIL→exact restore→control PASS và bộ cổng cuối trên source đã commit.

Chưa có producer đã phát hành/gán thật hoặc file provenance trong #216. Chưa chạy luồng live download/import, manual+Excel Save/Submit, current-authority, multipart lost ACK hoặc browser desktop/mobile vì chưa mở route/UI; các tiêu chí đó là bắt buộc của [#300](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/300) và [#301](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/301). [#291](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/291) không được coi technical hash này là original-file proof. Bộ native và các cổng chạy theo hướng dẫn gói; không thay kiểm Linux race/CI đúng SHA sau ROOT release.
