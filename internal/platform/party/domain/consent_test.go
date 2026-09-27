package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

var now = time.Date(2026, 12, 3, 2, 0, 0, 0, time.UTC)

func valid() Consent {
	return Consent{
		ID: ids.New(), SubjectRef: ids.New(), Purpose: "mrv.evidence", TextVersion: "2026-11",
		Status: Granted, Channel: FieldApp, EventTime: now.Add(-36 * time.Hour),
	}
}

func TestValidate(t *testing.T) {
	if err := valid().Validate(now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Consent){
		"id":           func(c *Consent) { c.ID = uuid.New() },
		"subject_ref":  func(c *Consent) { c.SubjectRef = uuid.Nil },
		"purpose":      func(c *Consent) { c.Purpose = "MRV Evidence" },
		"text_version": func(c *Consent) { c.TextVersion = "" },
		"status":       func(c *Consent) { c.Status = "maybe" },
		"channel":      func(c *Consent) { c.Channel = "email" },
		"event_time":   func(c *Consent) { c.EventTime = now.Add(10 * time.Minute) },
	}
	for field, mutate := range cases {
		c := valid()
		mutate(&c)
		var fe FieldError
		if err := c.Validate(now); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: muốn lỗi trường %s, nhận %v", field, field, err)
		}
	}
}

// Ngoại tuyến 48 giờ: bản ghi rút đồng ý đến trễ nhưng xảy ra trước không được ghi đè trạng thái mới hơn.
func TestSupersedesByEventTime(t *testing.T) {
	cur := &Current{Status: Granted, EventTime: now.Add(-1 * time.Hour), Seq: 5}
	late := valid()
	late.Status, late.EventTime, late.Seq = Withdrawn, now.Add(-30*time.Hour), 6
	if late.Supersedes(cur) {
		t.Fatal("sự việc cũ hơn không được thay trạng thái hiện tại")
	}
	newer := valid()
	newer.EventTime, newer.Seq = now, 7
	if !newer.Supersedes(cur) {
		t.Fatal("sự việc mới hơn phải thay")
	}
	tie := valid()
	tie.EventTime, tie.Seq = cur.EventTime, 8
	if !tie.Supersedes(cur) {
		t.Fatal("cùng event_time: bản ghi vào sổ sau thắng")
	}
	if !valid().Supersedes(nil) {
		t.Fatal("chưa có trạng thái: luôn thay")
	}
}
