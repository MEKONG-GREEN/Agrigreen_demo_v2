## BF-TT-MACH-WEB — Máy móc HTX: Năm SX, số lượng > 0 trên web (#103)

- **Nguồn:** PRD `kn-bugfix-brd-alignment` TT-10 ("Bắt buộc Xã/phường, HTX/Chủ sở hữu, Loại máy, Số lượng > 0, Năm SX ≤ năm hiện tại"). Phía máy chủ đã gộp: #113 BF-SRV-GUARD (số lượng nguyên 1 – 1.000.000, lỗi trường `lines[i].quantity`) và #119 BF-SRV-KN-ATTRS (năm sản xuất ở hồ sơ KN riêng `/v1/kn/machine-profiles/{cooperative_ref}`). Gói này chỉ sửa web và máy chủ giả lập, không đổi API, Go hay migration.
- **Màn `/kn/du-lieu/may-moc`:** bảng khai báo máy của HTX đã chọn có thêm cột **Năm SX** (năm lấy từ hồ sơ KN, ghép theo loại máy). Mỗi dòng bắt buộc Loại máy, Số lượng, Năm SX. Kiểm khi rời ô và trước khi Lưu, còn lỗi thì không gọi API:
  - Số lượng: số nguyên > 0, tối đa 1.000.000 (như máy chủ).
  - Năm SX: số nguyên, 1900 ≤ năm ≤ năm hiện tại theo lịch Việt Nam (cùng mốc máy chủ #119).
  - Không trùng loại máy (lỗi ở dòng sau). Dòng mới chọn sẵn loại chưa khai báo, năm để trống.
- **HTX/Chủ sở hữu bắt buộc:** phải chọn HTX ở bộ lọc thì bảng khai báo mới hiện, nên mọi dòng lưu đều gắn một HTX. Xã/phường lấy theo HTX.
- **Lưu:** một lần bấm gọi lần lượt `PUT /v1/kn/machine-counts/{ref}` (số lượng, pf/mechanization) rồi `PUT /v1/kn/machine-profiles/{ref}` (năm, thay toàn bộ, loại vắng bị bỏ khỏi hồ sơ). Hai lời gọi nằm trong cùng một lượt thao tác (`useAction`, #129). Nếu lời gọi thứ hai lỗi, bấm Lưu lại sẽ phát lại lời gọi đầu bằng khóa cũ, máy chủ trả kết quả cũ, không ghi số lượng lần hai.
- **Lỗi máy chủ theo trường** gắn vào ô của đúng dòng: `lines[i].quantity` → ô Số lượng dòng i; `lines.<loại máy>.year_made` | `.machine_type` → ô của dòng có loại đó.
- **Giả định:**
  - Năm gắn theo **loại máy** của HTX (một năm cho mỗi dòng loại × số lượng), theo #119. Nếu khách cần năm theo từng chiếc thì phải đổi mô hình hồ sơ.
  - Dòng đã khai báo số lượng trước gói này chưa có năm. Người dùng phải nhập năm cho mọi dòng trước khi lưu lại. Không tự điền năm.
  - Số lượng lưu được nhưng năm lỗi (vd mạng) thì hai sổ tạm lệch đến lần lưu sau. Máy chủ không gộp hai module vào một giao dịch (xem BF-SRV-KN-ATTRS).
- **Mock:** máy chủ giả lập có `GET/PUT /v1/kn/machine-profiles/{ref}` (năm 1900 – năm hiện tại VN, lỗi theo trường như Go) và từ chối số lượng 0 ở `/v1/kn/machine-counts` như #113.
