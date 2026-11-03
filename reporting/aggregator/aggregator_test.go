package aggregator

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestAggregateFiles(t *testing.T) {
	// Create test metrics files
	testFiles := createTestMetricsFiles(t)
	defer cleanupTestFiles(testFiles)

	// Test basic aggregation
	config := &Config{
		MaxEndpoints:     10,
		NoBucket:         false,
		MergePercentiles: "weighted",
	}

	result, err := AggregateFiles(testFiles, config)
	if err != nil {
		t.Fatalf("AggregateFiles failed: %v", err)
	}

	// Verify basic structure
	if result.RootGroup == nil {
		t.Error("RootGroup should not be nil")
	}

	if len(result.Metrics) == 0 {
		t.Error("Metrics should not be empty")
	}

	// Verify checks aggregation
	if len(result.RootGroup.Checks) != 3 {
		t.Errorf("Expected 3 checks, got %d", len(result.RootGroup.Checks))
	}

	// Verify specific check aggregation
	if check, exists := result.RootGroup.Checks["status_200"]; exists {
		if check.Passes != 2 {
			t.Errorf("Expected 2 passes for status_200, got %d", check.Passes)
		}
		if check.Fails != 0 {
			t.Errorf("Expected 0 fails for status_200, got %d", check.Fails)
		}
	} else {
		t.Error("status_200 check should exist")
	}
}

func TestAggregateFilesWithCardinalityLimit(t *testing.T) {
	// Create test files with many endpoints
	testFiles := createTestMetricsFilesWithManyEndpoints(t)
	defer cleanupTestFiles(testFiles)

	// Test with low cardinality limit
	config := &Config{
		MaxEndpoints:     2,
		NoBucket:         false,
		MergePercentiles: "weighted",
	}

	result, err := AggregateFiles(testFiles, config)
	if err != nil {
		t.Fatalf("AggregateFiles failed: %v", err)
	}

	// Should have "other" bucket
	if _, exists := result.Metrics["other"]; !exists {
		t.Error("Should have 'other' bucket when cardinality limit is exceeded")
	}
}

func TestAggregateFilesNoBucket(t *testing.T) {
	// Create test files with many endpoints
	testFiles := createTestMetricsFilesWithManyEndpoints(t)
	defer cleanupTestFiles(testFiles)

	// Test with no bucketing
	config := &Config{
		MaxEndpoints:     2,
		NoBucket:         true,
		MergePercentiles: "weighted",
	}

	result, err := AggregateFiles(testFiles, config)
	if err != nil {
		t.Fatalf("AggregateFiles failed: %v", err)
	}

	// Should not have "other" bucket
	if _, exists := result.Metrics["other"]; exists {
		t.Error("Should not have 'other' bucket when no-bucket is enabled")
	}

	// Should have fewer endpoints due to dropping
	if len(result.Metrics) > 3 { // Only global metrics + 2 allowed endpoints
		t.Errorf("Expected fewer endpoints when no-bucket is enabled, got %d", len(result.Metrics))
	}
}

func TestReadMetricsFile(t *testing.T) {
	// Create a test metrics file
	metrics := &Metrics{
		RootGroup: &TestGroup{
			Name:   "test",
			Path:   "::test",
			ID:     "test123",
			Groups: make(map[string]*TestGroup),
			Checks: map[string]*TestCheck{
				"test_check": {
					Name:   "test_check",
					Path:   "::test::test_check",
					ID:     "check123",
					Passes: 1,
					Fails:  0,
				},
			},
		},
		Metrics: map[string]*Metric{
			"test_metric": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   100.0,
					"min":   100.0,
					"max":   100.0,
				},
			},
		},
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}

	// Write to file
	filename := "test_metrics.json"
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("Failed to marshal metrics: %v", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(filename)

	// Read and verify
	readMetrics, err := ReadMetricsFile(filename)
	if err != nil {
		t.Fatalf("ReadMetricsFile failed: %v", err)
	}

	if readMetrics.RootGroup.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", readMetrics.RootGroup.Name)
	}

	if len(readMetrics.Metrics) != 1 {
		t.Errorf("Expected 1 metric, got %d", len(readMetrics.Metrics))
	}
}

func TestMergeTrendMetric(t *testing.T) {
	target := &Metric{
		Type: "trend",
		Values: map[string]interface{}{
			"count": 2.0,
			"avg":   100.0,
			"min":   50.0,
			"max":   150.0,
			"p(90)": 140.0,
		},
	}

	source := &Metric{
		Type: "trend",
		Values: map[string]interface{}{
			"count": 3.0,
			"avg":   200.0,
			"min":   100.0,
			"max":   300.0,
			"p(90)": 280.0,
		},
	}

	mergeTrendMetric(target, source, "weighted")

	// Verify merged values
	if count := getFloat64(target.Values, "count", 0); count != 5.0 {
		t.Errorf("Expected count 5.0, got %f", count)
	}

	if avg := getFloat64(target.Values, "avg", 0); avg != 160.0 {
		t.Errorf("Expected avg 160.0, got %f", avg)
	}

	if min := getFloat64(target.Values, "min", 0); min != 50.0 {
		t.Errorf("Expected min 50.0, got %f", min)
	}

	if max := getFloat64(target.Values, "max", 0); max != 300.0 {
		t.Errorf("Expected max 300.0, got %f", max)
	}

	if p90 := getFloat64(target.Values, "p(90)", 0); p90 != 224.0 {
		t.Errorf("Expected p(90) 224.0, got %f", p90)
	}
}

func TestMergeCounterMetric(t *testing.T) {
	target := &Metric{
		Type: "counter",
		Values: map[string]interface{}{
			"count": 10.0,
			"rate":  5.0,
		},
	}

	source := &Metric{
		Type: "counter",
		Values: map[string]interface{}{
			"count": 20.0,
			"rate":  10.0,
		},
	}

	mergeCounterMetric(target, source)

	// Verify merged values
	if count := getFloat64(target.Values, "count", 0); count != 30.0 {
		t.Errorf("Expected count 30.0, got %f", count)
	}
}

func TestMergeRateMetric(t *testing.T) {
	target := &Metric{
		Type: "rate",
		Values: map[string]interface{}{
			"passes": 8.0,
			"fails":  2.0,
			"value":  0.8,
		},
	}

	source := &Metric{
		Type: "rate",
		Values: map[string]interface{}{
			"passes": 9.0,
			"fails":  1.0,
			"value":  0.9,
		},
	}

	mergeRateMetric(target, source)

	// Verify merged values
	if passes := getFloat64(target.Values, "passes", 0); passes != 17.0 {
		t.Errorf("Expected passes 17.0, got %f", passes)
	}

	if fails := getFloat64(target.Values, "fails", 0); fails != 3.0 {
		t.Errorf("Expected fails 3.0, got %f", fails)
	}

	if value := getFloat64(target.Values, "value", 0); value != 0.85 {
		t.Errorf("Expected value 0.85, got %f", value)
	}
}

func TestIsGlobalMetric(t *testing.T) {
	globalTests := []struct {
		name     string
		expected bool
	}{
		{"checks", true},
		{"data_received", true},
		{"http_req_duration", true},
		{"http_req_failed", true},
		{"http_reqs", true},
		{"iterations", true},
		{"vus", true},
		{"vus_max", true},
		{"http_req_status_200", false},
		{"status_200", false},
		{"delay_1", false},
		{"users_profile", false},
	}

	for _, test := range globalTests {
		result := isGlobalMetric(test.name)
		if result != test.expected {
			t.Errorf("isGlobalMetric(%s) = %v, expected %v", test.name, result, test.expected)
		}
	}
}

func TestWriteOutput(t *testing.T) {
	metrics := &Metrics{
		RootGroup: &TestGroup{
			Name:   "test",
			Path:   "::test",
			ID:     "test123",
			Groups: make(map[string]*TestGroup),
			Checks: make(map[string]*TestCheck),
		},
		Metrics:   make(map[string]*Metric),
		SetupData: make(map[string]string),
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}

	filename := "test_output.json"
	err := WriteOutput(metrics, filename)
	if err != nil {
		t.Fatalf("WriteOutput failed: %v", err)
	}
	defer os.Remove(filename)

	// Verify file was created and contains valid JSON
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	var readMetrics Metrics
	err = json.Unmarshal(data, &readMetrics)
	if err != nil {
		t.Fatalf("Failed to unmarshal output file: %v", err)
	}

	if readMetrics.RootGroup.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", readMetrics.RootGroup.Name)
	}
}

// Helper functions

func createTestMetricsFiles(t *testing.T) []string {
	files := []string{}

	// File 1
	metrics1 := &Metrics{
		RootGroup: &TestGroup{
			Name:   "",
			Path:   "",
			ID:     "test1",
			Groups: make(map[string]*TestGroup),
			Checks: map[string]*TestCheck{
				"status_200": {
					Name:   "status_200",
					Path:   "::status_200",
					ID:     "check1",
					Passes: 1,
					Fails:  0,
				},
				"delay_1": {
					Name:   "delay_1",
					Path:   "::delay_1",
					ID:     "check2",
					Passes: 1,
					Fails:  0,
				},
			},
		},
		Metrics: map[string]*Metric{
			"status_200": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   100.0,
					"min":   100.0,
					"max":   100.0,
				},
			},
			"delay_1": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   200.0,
					"min":   200.0,
					"max":   200.0,
				},
			},
			"http_req_duration": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 2.0,
					"avg":   150.0,
					"min":   100.0,
					"max":   200.0,
				},
			},
		},
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}

	filename1 := "test_metrics_1.json"
	files = append(files, filename1)
	writeMetricsToFile(t, metrics1, filename1)

	// File 2
	metrics2 := &Metrics{
		RootGroup: &TestGroup{
			Name:   "",
			Path:   "",
			ID:     "test2",
			Groups: make(map[string]*TestGroup),
			Checks: map[string]*TestCheck{
				"status_200": {
					Name:   "status_200",
					Path:   "::status_200",
					ID:     "check1",
					Passes: 1,
					Fails:  0,
				},
				"status_404": {
					Name:   "status_404",
					Path:   "::status_404",
					ID:     "check3",
					Passes: 1,
					Fails:  0,
				},
			},
		},
		Metrics: map[string]*Metric{
			"status_200": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   150.0,
					"min":   150.0,
					"max":   150.0,
				},
			},
			"status_404": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   50.0,
					"min":   50.0,
					"max":   50.0,
				},
			},
			"http_req_duration": {
				Type: "trend",
				Values: map[string]interface{}{
					"count": 2.0,
					"avg":   100.0,
					"min":   50.0,
					"max":   150.0,
				},
			},
		},
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}

	filename2 := "test_metrics_2.json"
	files = append(files, filename2)
	writeMetricsToFile(t, metrics2, filename2)

	return files
}

func createTestMetricsFilesWithManyEndpoints(t *testing.T) []string {
	files := []string{}

	// Create multiple files with many different endpoints
	for i := 1; i <= 3; i++ {
		metrics := &Metrics{
			RootGroup: &TestGroup{
				Name:   "",
				Path:   "",
				ID:     "test",
				Groups: make(map[string]*TestGroup),
				Checks: make(map[string]*TestCheck),
			},
			Metrics: make(map[string]*Metric),
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}

		// Add many different endpoints
		for j := 1; j <= 5; j++ {
			endpointName := fmt.Sprintf("endpoint_%d_%d", i, j)
			metrics.Metrics[endpointName] = &Metric{
				Type: "trend",
				Values: map[string]interface{}{
					"count": 1.0,
					"avg":   float64(100 + j),
					"min":   float64(100 + j),
					"max":   float64(100 + j),
				},
			}
		}

		filename := fmt.Sprintf("test_many_endpoints_%d.json", i)
		files = append(files, filename)
		writeMetricsToFile(t, metrics, filename)
	}

	return files
}

func writeMetricsToFile(t *testing.T, metrics *Metrics, filename string) {
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("Failed to marshal metrics: %v", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file %s: %v", filename, err)
	}
}

func cleanupTestFiles(files []string) {
	for _, file := range files {
		os.Remove(file)
	}
}
