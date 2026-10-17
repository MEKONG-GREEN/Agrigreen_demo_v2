package notify

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"testing"
)

func TestLegacyDigestJobContract(t *testing.T) {
	module := New(nil, clock.System{}, Deps{})
	jobs := module.Jobs(nil, nil)
	if len(jobs) != 1 {
		t.Fatal("missing legacy digest job")
	}
	for _, job := range jobs {
		every, ok := job.Spec.(schedule.Every)
		if !ok {
			t.Fatal("legacy digest schedule type")
		}
		data, err := json.Marshal(struct {
			Name       string
			EveryNS    int64
			RunOnStart bool
			TimeoutNS  int64
		}{job.Name, int64(every.Interval), job.RunOnStart, int64(job.Timeout)})
		if err != nil {
			t.Fatal(err)
		}
		t.Log("OLD_DIGEST_JOB=" + string(data))
	}
}
