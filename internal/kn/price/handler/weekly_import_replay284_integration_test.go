package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Exercise the permanent owner receipt without a shared response cache.
// Current authority and the original file/key/body remain unchanged, while a
// later valid command edits the same cell before the lost-ACK retry arrives.
func TestWeeklyImportReplayAfterSuccessor284(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		name := "original_without_overwrite_confirmation"
		if confirmed {
			name = "original_with_overwrite_confirmation"
		}
		t.Run(name, func(t *testing.T) {
			e := weeklyFixture284(t)
			ctx := authz.WithPrincipal(context.Background(), e.who["tinh"])
			base, err := e.svc.Read(ctx, "kn", "information", "")
			if err != nil {
				t.Fatal(err)
			}
			leaf := base.Report.Rows[1]
			data := weeklyCSV284(t, [][]string{
				{"Tên hạng mục", "Tiến độ tuần", "Kinh phí tuần"},
				{leaf.Name, "5", "10"},
			})
			preview, err := e.svc.PreviewImport(ctx, "kn", "information", base.Report.ID, base.Version, data)
			if err != nil || preview.ValidRows != 1 || preview.OverwriteRows != 0 {
				t.Fatalf("valid initial import fixture: preview=%+v err=%v", preview, err)
			}
			key := ids.New().String()
			first, err := e.svc.CommitImport(ctx, "kn", "information", base.Report.ID, key, base.Version, preview.SHA256, confirmed, data)
			if err != nil {
				t.Fatal("first import", err)
			}
			next := weeklyCommand284(first, "save")
			next.Rows = []domain.WeeklyPatch{{ID: leaf.ID, Progress: ptr284("7"), Money: ptr284("25")}}
			successor, err := e.svc.Write(ctx, "kn", "information", ids.New().String(), next)
			if err != nil {
				t.Fatal("successor save", err)
			}
			replayed, err := e.svc.CommitImport(ctx, "kn", "information", base.Report.ID, key, base.Version, preview.SHA256, confirmed, data)
			if err != nil {
				t.Fatalf("same file/key/input after successor must return original permanent receipt, got %v", err)
			}
			if replayed.Version != first.Version || replayed.Report.ID != first.Report.ID {
				t.Fatalf("retry returned another receipt: got version %d, want %d", replayed.Version, first.Version)
			}
			latest, err := e.svc.Read(ctx, "kn", "information", base.Report.ID)
			if err != nil {
				t.Fatal(err)
			}
			if latest.Version != successor.Version || len(latest.Log) != len(successor.Log) {
				t.Fatal("retry appended an event or changed successor")
			}
			row := findWeekly284(t, latest, leaf.Name)
			assertDecimal284(t, row.Progress, "7")
			assertDecimal284(t, row.Money, "25")

			// A new import still requires confirmation before overwriting the successor.
			_, err = e.svc.CommitImport(ctx, "kn", "information", base.Report.ID, ids.New().String(), latest.Version, preview.SHA256, false, data)
			var confirmation *problem.Problem
			if !errors.As(err, &confirmation) || confirmation.Status != 409 || confirmation.Code != "price.weekly_confirmation" {
				t.Fatalf("fresh import must require exact overwrite confirmation, got %v", err)
			}
			current, err := e.svc.Read(ctx, "kn", "information", base.Report.ID)
			if err != nil || current.Version != successor.Version {
				t.Fatal("rejected fresh overwrite changed current state", err)
			}
		})
	}
}
