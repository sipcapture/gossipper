package reporthtml

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/stats"
)

func TestWriteContainsKeySections(t *testing.T) {
	t.Parallel()
	s := stats.Summary{
		SchemaVersion: stats.SummarySchemaVersion,
		ToolVersion:   "Gossipper test",
		StartedAt:     time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC),
		Duration:      5 * time.Second,
		TotalCalls:    10,
		SuccessCalls:  9,
		FailedCalls:   1,
		SuccessRatio:  0.9,
		Findings:      []string{"Calls: total=10 finished=10 success=9 failed=1 (success 90.0% of total)."},
	}
	var buf bytes.Buffer
	if err := Write(&buf, s); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, sub := range []string{"Gossipper run report", "Total calls", "10", "Findings", "Media", "</html>"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("missing %q in output", sub)
		}
	}
}
