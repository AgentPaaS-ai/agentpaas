package cli

import (
	"strings"
	"testing"
)

func TestValidCloudCronExpr_MinIntervalFiveMinutes(t *testing.T) {
	cases := []struct {
		expr string
		ok   bool
	}{
		{"every_5m", true},
		{"every_15m", true},
		{"every_1h", true},
		{"every_1m", false},
		{"30 9 * * 1-5", true},
		{"0 * * * *", true},
		{"*/5 * * * *", true},
		{"* * * * *", false},
		{"*/1 * * * *", false},
		{"*/2 * * * *", false},
		{"*/3 * * * *", false},
		{"*/4 * * * *", false},
		{"", false},
		{"not-a-cron", false},
		{"* * * *", false},
	}
	for _, tc := range cases {
		got := validCloudCronExpr(tc.expr)
		if got != tc.ok {
			t.Errorf("validCloudCronExpr(%q) = %v, want %v", tc.expr, got, tc.ok)
		}
	}
}

func TestCloudCronSet_RejectsEvery1mWithFiveMinuteHelp(t *testing.T) {
	_, _, err := executeCloudCmd(t, "", "cloud", "cron", "set", "dep_x", "--expr", "every_1m")
	if err == nil {
		t.Fatal("expected error for every_1m, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "every_1m") && !strings.Contains(msg, "every_5m|every_15m|every_1h") {
		t.Errorf("error still advertises every_1m without 5m floor: %v", err)
	}
	if !strings.Contains(msg, "every_5m|every_15m|every_1h") {
		t.Errorf("error = %v, want every_5m|every_15m|every_1h", err)
	}
	if strings.Contains(msg, "every_1m|") {
		t.Errorf("error still lists every_1m as allowed: %v", err)
	}
}

func TestCloudCronHelp_DropsEvery1m(t *testing.T) {
	cron := newCloudCronCmd()
	if strings.Contains(cron.Long, "every_1m") {
		t.Errorf("cron Long still mentions every_1m: %s", cron.Long)
	}
	if !strings.Contains(cron.Long, "every_5m, every_15m, every_1h") {
		t.Errorf("cron Long = %q, want named intervals every_5m, every_15m, every_1h", cron.Long)
	}
	set := newCloudCronSetCmd()
	usage := set.Flags().Lookup("expr").Usage
	if strings.Contains(usage, "every_1m") {
		t.Errorf("expr flag still mentions every_1m: %s", usage)
	}
	if !strings.Contains(usage, "every_5m|every_15m|every_1h") {
		t.Errorf("expr flag = %q, want every_5m|every_15m|every_1h", usage)
	}
}
