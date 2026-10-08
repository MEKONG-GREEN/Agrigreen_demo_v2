package ingest

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func owned(string) bool { return true }

func code(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// Kiểm hợp đồng mặc định từ chối: hợp đồng lạ / phiên bản lạ / hợp đồng feed → 404; proposed chỉ phục vụ khi
// bật cờ dev/test VÀ không phải production; chưa nối port chủ → 403.
func TestGateDefaultDeny(t *testing.T) {
	cases := []struct {
		name     string
		gate     Gate
		c        string
		v        int
		want     string
		servedOK bool
	}{
		{"hợp đồng lạ", Gate{AllowProposed: true, Owned: owned}, "khong_co", 1, ErrContractUnknown.Code, false},
		{"phiên bản lạ", Gate{AllowProposed: true, Owned: owned}, "roll_count_statement", 9, ErrContractUnknown.Code, false},
		{"hợp đồng feed không nạp được", Gate{AllowProposed: true, Owned: owned}, "admin_units", 1, ErrContractUnknown.Code, false},
		{"proposed không cờ", Gate{Owned: owned}, "roll_count_statement", 1, ErrContractNotServable.Code, false},
		{"proposed có cờ nhưng production", Gate{AllowProposed: true, Production: true, Owned: owned}, "roll_count_statement", 1, ErrContractNotServable.Code, false},
		{"proposed có cờ, chưa nối port chủ", Gate{AllowProposed: true}, "roll_count_statement", 1, ErrNoOwnerPort.Code, false},
		{"proposed có cờ, dev, có port chủ", Gate{AllowProposed: true, Owned: owned}, "collection_schedule", 1, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := tc.gate.Resolve(tc.c, tc.v)
			if tc.servedOK {
				if err != nil || c == nil || c.Kind != integration.KindIngest {
					t.Fatalf("phải phục vụ: %v", err)
				}
				return
			}
			if code(err) != tc.want {
				t.Fatalf("muốn %s, nhận %v", tc.want, err)
			}
		})
	}
	if !Known("roll_count_statement") || Known("admin_units") || Known("khong_co") {
		t.Fatal("Known chỉ nhận hợp đồng nạp")
	}
}

func example(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/integration/examples/roll_count_statement.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const exampleRef = "0199b8d2-2200-7c01-8d02-e03f04a05b06"

// Phong bì: khóa lạ, hợp đồng khác path, batch_ref ≠ Idempotency-Key, thiếu items → 400 integration.batch_invalid;
// vượt max_batch → 413.
func TestParseEnvelopeAndSize(t *testing.T) {
	raw := example(t)
	env, err := ParseEnvelope(raw, "roll_count_statement", strings.ToUpper(exampleRef))
	if err != nil || len(env.Items) != 1 || env.Items[0].EntityKey == "" {
		t.Fatalf("mẫu hợp lệ: %v %+v", err, env)
	}
	bad := []struct {
		name, body, path, key string
	}{
		{"khóa lạ ở phong bì", strings.Replace(string(raw), `"items"`, `"extra": 1, "items"`, 1), "roll_count_statement", exampleRef},
		{"hợp đồng khác path", string(raw), "collection_schedule", exampleRef},
		{"batch_ref khác Idempotency-Key", string(raw), "roll_count_statement", ids.New().String()},
		{"items rỗng", `{"contract":"roll_count_statement","contract_version":1,"batch_ref":"` + exampleRef + `","sent_at":"2026-10-09T18:05:00+07:00","items":[]}`, "roll_count_statement", exampleRef},
		{"không phải JSON", `{`, "roll_count_statement", exampleRef},
	}
	for _, b := range bad {
		if _, err := ParseEnvelope([]byte(b.body), b.path, b.key); code(err) != ErrBatchInvalid.Code {
			t.Errorf("%s: muốn %s, nhận %v", b.name, ErrBatchInvalid.Code, err)
		}
	}
	c, err := integration.Get("roll_count_statement", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSize(c, c.Ingest.MaxBatch); err != nil {
		t.Fatalf("đúng max_batch phải nhận: %v", err)
	}
	if err := CheckSize(c, c.Ingest.MaxBatch+1); code(err) != ErrBatchTooLarge.Code {
		t.Fatalf("vượt max_batch: %v", err)
	}
	if v, err := ContractVersionOf(raw); err != nil || v != 1 {
		t.Fatalf("contract_version: %d %v", v, err)
	}
	if _, err := ContractVersionOf([]byte(`{"contract_version":"x"}`)); code(err) != ErrBatchInvalid.Code {
		t.Fatalf("contract_version sai kiểu: %v", err)
	}
	bk, err := BusinessKey(c, env.Items[0].Record)
	if err != nil || bk != "0199b8d1-0a00-7b01-8c02-d03e04f05a06:1" {
		t.Fatalf("khóa nghiệp vụ statement_ref:revision: %q %v", bk, err)
	}
}

// 429: bão hòa khi độ sâu hàng đợi của CHÍNH đối tác ≥ giới hạn; đối tác khác không bị ảnh hưởng.
func TestSaturationPerPartner(t *testing.T) {
	if Saturation(1, 2) || !Saturation(2, 2) || !Saturation(DefaultMaxQueued, 0) || Saturation(DefaultMaxQueued-1, -1) {
		t.Fatal("Saturation: depth ≥ giới hạn")
	}
	m := NewMemory(2)
	ctx := context.Background()
	a, b := ids.New(), ids.New()
	for range 2 {
		if err := m.Publish(ctx, a, outbox.Envelope{EventID: ids.New()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Publish(ctx, a, outbox.Envelope{EventID: ids.New()}); !errors.Is(err, ErrQueueSaturated) {
		t.Fatalf("đối tác a đầy → ErrQueueSaturated, nhận %v", err)
	}
	if err := m.Publish(ctx, b, outbox.Envelope{EventID: ids.New()}); err != nil {
		t.Fatalf("đối tác b không bị a chặn: %v", err)
	}
	if n, _ := m.Depth(ctx, a); n != 2 {
		t.Fatalf("độ sâu a = 2, nhận %d", n)
	}
}

// Hàng đợi trong tiến trình: lỗi → giao lại; lần giao cuối (inbox.FinalAttempt) → DLQ của đối tác + Failure
// DeadLettered; thông điệp độc của a không chặn b.
func TestMemoryDLQOnPoison(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(10)
	a, b := ids.New(), ids.New()
	poison, good := outbox.Envelope{EventID: ids.New()}, outbox.Envelope{EventID: ids.New()}
	_ = m.Publish(ctx, a, poison)
	_ = m.Publish(ctx, b, good)
	attempts := map[uuid.UUID]int{}
	var failures []inbox.Failure
	w := Worker{
		process: func(_ context.Context, queue string, env outbox.Envelope) error {
			attempts[env.EventID]++
			if env.EventID == poison.EventID {
				return errors.New("port chủ lỗi")
			}
			if queue != QueueName(b) {
				t.Errorf("thông điệp b phải ở hàng đợi của b, nhận %s", queue)
			}
			return nil
		},
		Failure: func(_ context.Context, f inbox.Failure) error { failures = append(failures, f); return nil },
	}
	m.Drain(ctx, w)
	if attempts[good.EventID] != 1 {
		t.Fatalf("thông điệp lành của b xử lý đúng một lần: %d", attempts[good.EventID])
	}
	if attempts[poison.EventID] != inbox.DeliveryLimit+1 {
		t.Fatalf("thông điệp độc giao %d lần (giới hạn %d + lần cuối), nhận %d", inbox.DeliveryLimit+1, inbox.DeliveryLimit, attempts[poison.EventID])
	}
	if dl := m.DeadLettered(a); len(dl) != 1 || dl[0].EventID != poison.EventID || len(m.DeadLettered(b)) != 0 {
		t.Fatalf("DLQ riêng của a: %v / b: %v", dl, m.DeadLettered(b))
	}
	last := failures[len(failures)-1]
	if !last.DeadLettered || last.Consumer != QueueName(a) || len(failures) != inbox.DeliveryLimit+1 {
		t.Fatalf("Failure: %d lần, lần cuối DeadLettered: %+v", len(failures), last)
	}
	if n, _ := m.Depth(ctx, a); n != 0 {
		t.Fatalf("hàng đợi a rỗng sau DLQ: %d", n)
	}
}

func TestQueueArgs(t *testing.T) {
	c := ids.New()
	args := QueueArgs(c, 0)
	if args["x-queue-type"] != "quorum" || args["x-delivery-limit"] != inbox.DeliveryLimit || args["x-max-length"] != DefaultMaxQueued ||
		args["x-overflow"] != "reject-publish" || args["x-dead-letter-exchange"] != outbox.DeadLetterExchange || args["x-dead-letter-routing-key"] != QueueName(c) {
		t.Fatalf("tham số quorum queue: %v", args)
	}
	if !strings.HasPrefix(QueueName(c), QueuePrefix) {
		t.Fatal("tên hàng đợi theo đối tác")
	}
}
