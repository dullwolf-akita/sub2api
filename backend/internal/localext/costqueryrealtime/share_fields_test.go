package costqueryrealtime

import (
	"strings"
	"testing"
)

func TestProjectShareRowOmitsUpstreamAccountFields(t *testing.T) {
	row := map[string]any{
		"account_id":      int64(1),
		"account_name":    "sample-account",
		"channel_id":      int64(2),
		"user_id":         int64(3),
		"actual_cost":     1.25,
		"requested_model": "demo-model",
		"created_at":      "2026-11-12T00:00:00Z",
	}
	out := projectShareRow(row)
	for _, k := range []string{"account_id", "account_name", "channel_id", "user_id"} {
		if _, ok := out[k]; ok {
			t.Fatalf("share row must not contain %s", k)
		}
	}
	if out["actual_cost"] != 1.25 {
		t.Fatalf("actual_cost not preserved: %v", out["actual_cost"])
	}
	if out["requested_model"] != "demo-model" {
		t.Fatalf("requested_model not preserved: %v", out["requested_model"])
	}
}

func TestShareColumnsAndLabelsOmitSecrets(t *testing.T) {
	hidden := []string{"account_id", "account_name", "channel_id"}
	for _, c := range hidden {
		if !isShareHiddenField(c) {
			t.Fatalf("%s should be hidden on share paths", c)
		}
	}
	for _, c := range shareExportColumns() {
		if isShareHiddenField(c) {
			t.Fatalf("share export columns include %s", c)
		}
	}
	for _, c := range shareSearchColumns() {
		if isShareHiddenField(c) {
			t.Fatalf("share search columns include %s", c)
		}
	}
	labels := shareLabels()
	for _, c := range hidden {
		if _, ok := labels[c]; ok {
			t.Fatalf("share labels include %s", c)
		}
	}
	if _, ok := labels["actual_cost"]; !ok {
		t.Fatal("share labels missing actual_cost")
	}
}

func TestBuildWhereShareDoesNotSearchAccountName(t *testing.T) {
	sqlShare, _ := buildWhereWithSearch("", "needle", shareSearchColumns())
	if strings.Contains(sqlShare, "account_name") {
		t.Fatalf("share search SQL includes account_name: %s", sqlShare)
	}
	sqlAdmin, _ := buildWhere("", "needle")
	if !strings.Contains(sqlAdmin, "account_name") {
		t.Fatalf("admin search SQL should still include account_name: %s", sqlAdmin)
	}
}
