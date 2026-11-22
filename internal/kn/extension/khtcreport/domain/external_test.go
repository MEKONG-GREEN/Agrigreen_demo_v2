package domain

import (
	"testing"

	"github.com/google/uuid"
)

// W11.67: số liệu Nhập ngoài nằm trong băm đầu vào (BR-DL-04) nhưng không đổi băm của bản soạn không có số liệu ngoài.
func TestWithExternalHash(t *testing.T) {
	base := Data{InputHash: "abc"}
	same, err := WithExternal(base, nil)
	if err != nil || same.InputHash != "abc" || same.External != nil {
		t.Fatalf("không có số liệu ngoài phải giữ băm: %+v %v", same, err)
	}
	fig := ExternalFigure{EntryID: uuid.New(), IndicatorID: uuid.New(), Name: "Diện tích nuôi tôm nước lợ", Value: "26400", Unit: "ha", Agency: "Chi cục Thủy sản", Document: "email", SourceDate: "2026-09-29"}
	a, err := WithExternal(base, []ExternalFigure{fig})
	if err != nil || a.InputHash == "abc" || len(a.External) != 1 {
		t.Fatalf("có số liệu ngoài phải đổi băm: %+v %v", a, err)
	}
	fig.Value = "26100"
	b, _ := WithExternal(base, []ExternalFigure{fig})
	if b.InputHash == a.InputHash {
		t.Fatal("đổi giá trị nguồn ngoài phải đổi băm (Nguồn đã thay đổi)")
	}
}
