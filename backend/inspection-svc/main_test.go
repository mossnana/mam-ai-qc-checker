package main

import (
	"encoding/json"
	"testing"
)

func TestJobJSONUsesFrontendContract(t *testing.T) {
	payload, err := json.Marshal(job{ID: "job-1", Status: "EVALUATED", Tasks: map[string]*task{"task-1": {ID: "task-1", RuleType: "IMAGE_DIMENSION", State: "DONE"}}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["id"] != "job-1" || decoded["status"] != "EVALUATED" {
		t.Fatalf("job fields must use camelCase: %s", payload)
	}
	if _, legacyKey := decoded["ID"]; legacyKey {
		t.Fatalf("legacy ID key leaked into API response: %s", payload)
	}
}
