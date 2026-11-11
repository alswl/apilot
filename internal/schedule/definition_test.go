package schedule

import (
	"github.com/alswl/apilot/internal/model"
	"testing"
	"time"
)

func TestScheduleDefinitions(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	n, e := Next(&model.Schedule{Type: "interval", Expression: "30s"}, now)
	if e != nil || !n.Equal(now.Add(30*time.Second)) {
		t.Fatal(n, e)
	}
	n, e = Next(&model.Schedule{Type: "cron", Expression: "0 9 * * 1-5", Timezone: "Asia/Shanghai"}, now)
	if e != nil || n.Hour() != 1 {
		t.Fatal(n, e)
	}
	for _, s := range []*model.Schedule{{Type: "interval", Expression: "0s"}, {Type: "cron", Expression: "bad", Timezone: "UTC"}, {Type: "cron", Expression: "* * * * *", Timezone: "Bad/Zone"}} {
		if _, e = Next(s, now); e == nil {
			t.Fatal("invalid schedule accepted", s)
		}
	}
}
