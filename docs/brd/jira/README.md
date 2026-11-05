# Tệp nối BRD ↔ Jira

Mỗi hệ có một tệp `<mã-hệ>.yaml` do skill [brd-to-backlog](../../../.claude/skills/brd-to-backlog/SKILL.md) ghi khi tạo ticket Jira. Tệp giúp chạy lại hoặc cập nhật theo BRD mới mà không tạo ticket trùng.

**Chỉ lưu tham chiếu**: link Drive, file id, phiên bản, mã mục/mã yêu cầu BRD, mã tạm, mã ticket Jira. **Không** lưu nội dung BRD (đoạn văn, tóm tắt, summary ticket). Nội dung nằm trên Drive và Jira.

## Định dạng

```yaml
he: app-htx                      # mã hệ: chữ thường, gạch ngang
brd:
  link: https://drive.google.com/file/d/<file-id>/view
  file_id: <file-id>
  phien_ban: v2.0                # theo tên tệp
  modified_time: 2026-11-02T08:00:00Z
jira:
  site: <ten>.atlassian.net
  project: <KEY>
  label: brd-app-htx
items:
  - id: E01                      # mã tạm, không đổi giữa các lần chạy
    loai: epic
    jira: <KEY>-101
    muc_brd: ["§4.1"]
    ma_yc: ["UR-AD-04", "FN-AD-04.04"]
  - id: E01-S01
    loai: story
    cha: E01
    jira: <KEY>-102
    sprint: 1
    muc_brd: ["§4.1"]
    ma_yc: ["FN-AD-04.04", "FN-AD-04.04-BR1"]
  - id: E01-S01-T01
    loai: subtask
    cha: E01-S01
    jira: <KEY>-103
```

Quy tắc:
- `items` ghi tăng dần sau mỗi ticket tạo thành công; mục chưa có `jira` là chưa tạo.
- Ticket bị đóng do BRD bỏ yêu cầu: giữ mục, thêm `dong: true`.
- Commit tệp sau mỗi lần chạy bước 7, commit message tiếng Việt theo AGENTS.md §4.
