package discovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wso2/adc/internal/config"
)

type fakeDeepflowClient struct {
	lastSQL string
}

func (f *fakeDeepflowClient) Query(_ context.Context, sql string) ([]map[string]interface{}, error) {
	f.lastSQL = sql
	return nil, nil
}
func (f *fakeDeepflowClient) Ping(_ context.Context) error { return nil }
func (f *fakeDeepflowClient) Close()                       {}

// TestQuerySignatures_NoQuotedDatetimeLiterals guards against regression of the
// DeepFlow TZ bug: ClickHouse DateTime64 columns are parsed against the column's
// own timezone when compared to quoted literals, so `start_time >= "..."` silently
// produces wrong windows. The fix uses toUnixTimestamp(start_time) >= <unix_int>
// which is TZ-invariant.
func TestQuerySignatures_NoQuotedDatetimeLiterals(t *testing.T) {
	fake := &fakeDeepflowClient{}
	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{
			TrafficFilter: config.TrafficFilterConfig{
				Protocol:          6,
				L7Protocols:       []string{"HTTP"},
				MinDirectionScore: 200,
				ObservationPoint:  "s-p",
			},
			Schedule: config.ScheduleConfig{
				MaxSignaturesPerCycle: 1000,
			},
		},
	}
	p := &Phase{cfg: cfg, client: fake}

	start := time.Unix(1775910281, 0)
	end := time.Unix(1775910881, 0)
	if _, err := p.querySignatures(context.Background(), start, end, nil); err != nil {
		t.Fatalf("querySignatures returned unexpected error: %v", err)
	}

	sql := fake.lastSQL
	forbidden := []string{
		`start_time >= "`,
		`start_time < "`,
		`start_time <= "`,
	}
	for _, bad := range forbidden {
		if strings.Contains(sql, bad) {
			t.Errorf("generated SQL contains quoted datetime literal %q (regression to TZ bug):\n%s", bad, sql)
		}
	}
	required := []string{
		"toUnixTimestamp(start_time) >= 1775910281",
		"toUnixTimestamp(start_time) < 1775910881",
		"toUnixTimestamp(any(start_time)) AS sample_start_time",
	}
	for _, req := range required {
		if !strings.Contains(sql, req) {
			t.Errorf("generated SQL missing required fragment %q:\n%s", req, sql)
		}
	}
}
