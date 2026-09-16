package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func assertMixedSyntaxProtocol(t *testing.T, payload []byte, components []component) {
	t.Helper()
	records := protocolRecords(t, payload)
	assertRecoveryCompletion(t, records)
	assertRecoveryMeasurements(t, records)
	assertRecoveryCoverage(t, records, components)
	assertRecoveryDiagnostics(t, records)
}

func assertRecoveryCompletion(t *testing.T, records map[string][]map[string]any) {
	t.Helper()
	terminal := records["terminal"]
	if len(terminal) != 1 || terminal[0]["status"] != "success" {
		t.Fatalf("terminal records = %#v", terminal)
	}
	if failed, _ := terminal[0]["failed_unit_ids"].([]any); len(failed) != 0 {
		t.Fatalf("syntax-only unit was terminally failed: %v", failed)
	}
	plans := records["execution_plan"]
	if len(plans) != 1 || int(plans[0]["parsed_source_count"].(float64)) != 1 {
		t.Fatalf("execution plan = %#v", plans)
	}
}

func assertRecoveryMeasurements(t *testing.T, records map[string][]map[string]any) {
	t.Helper()
	measurements := map[string]int{}
	positive := map[string]int{}
	for _, record := range records["measurement"] {
		path, _ := record["path"].(string)
		measurements[path]++
		value, _ := record["value"].(float64)
		if value > 0 {
			positive[path]++
		}
	}
	if measurements["broken.go"] != 0 || positive["valid.go"] == 0 {
		t.Fatalf("measurement paths = %#v positive=%#v", measurements, positive)
	}
}

func assertRecoveryCoverage(t *testing.T, records map[string][]map[string]any, components []component) {
	t.Helper()
	coverage := map[string][]map[string]any{}
	for _, record := range records["coverage"] {
		path, _ := record["path"].(string)
		component, _ := record["component_id"].(string)
		coverage[path+"/"+component] = append(coverage[path+"/"+component], record)
	}
	for _, component := range components {
		key := "broken.go/" + component.ID
		entries := coverage[key]
		if len(entries) != 1 || entries[0]["state"] != "failed" {
			t.Fatalf("broken coverage %s = %#v", component.ID, entries)
		}
	}
	if entries := coverage["valid.go/cognitive_complexity"]; len(entries) != 1 || entries[0]["state"] != "complete" {
		t.Fatalf("valid routine coverage = %#v", entries)
	}
	if entries := coverage["valid.go/god_class"]; len(entries) != 1 || entries[0]["state"] != "unavailable" {
		t.Fatalf("valid type coverage = %#v", entries)
	}
}

func assertRecoveryDiagnostics(t *testing.T, records map[string][]map[string]any) {
	t.Helper()
	locations := map[string]bool{}
	warnings := map[string]bool{}
	for _, record := range records["diagnostic"] {
		code, _ := record["code"].(string)
		path, _ := record["path"].(string)
		message, _ := record["message"].(string)
		if code == "SYNTAX_ERROR" && path == "broken.go" {
			parts := strings.Split(message, ":")
			if len(parts) >= 3 && parts[0] == path {
				locations[fmt.Sprintf("%s:%s", parts[1], parts[2])] = true
			}
		}
		if code == "COVERAGE_UNAVAILABLE" && path == "valid.go" {
			warnings[message] = true
		}
	}
	if len(locations) < 2 || len(warnings) != 1 {
		t.Fatalf("syntax locations=%v peer warnings=%v", locations, warnings)
	}
}

func protocolRecords(t *testing.T, payload []byte) map[string][]map[string]any {
	t.Helper()
	records := make(map[string][]map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(payload))
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return records
			}
			t.Fatal(err)
		}
		typeName, ok := record["type"].(string)
		if !ok {
			t.Fatalf("protocol record has no type: %#v", record)
		}
		records[typeName] = append(records[typeName], record)
	}
}
