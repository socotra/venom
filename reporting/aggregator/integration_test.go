package aggregator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDMAEndToEndWorkflow(t *testing.T) {
	// This test simulates the complete GNU Parallel + DMA workflow
	// 1. Run multiple Venom tests in parallel
	// 2. Generate individual metrics files
	// 3. Aggregate them using the DMA tool
	// 4. Verify the unified output

	// Create test YAML files
	testFiles := createTestYAMLFiles(t)
	defer cleanupTestYAMLFiles(testFiles)

	// Run Venom tests in parallel (simulating GNU Parallel)
	metricsFiles := runParallelVenomTests(t, testFiles)
	defer cleanupMetricsFiles(metricsFiles)

	// Verify individual metrics files were created
	for _, file := range metricsFiles {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Errorf("Metrics file %s was not created", file)
		}
	}

	// Aggregate using DMA
	aggregatedFile := "aggregated_integration_test.json"
	err := runDMAAggregation(t, metricsFiles, aggregatedFile)
	if err != nil {
		t.Fatalf("DMA aggregation failed: %v", err)
	}
	defer os.Remove(aggregatedFile)

	// Verify aggregated output
	result, err := verifyAggregatedOutput(t, aggregatedFile)
	if err != nil {
		t.Fatalf("Failed to verify aggregated output: %v", err)
	}

	// Verify key aspects of aggregation
	verifyAggregationResults(t, result, len(testFiles))
}

func TestDMACardinalityControl(t *testing.T) {
	// Test that cardinality limits work correctly in the full workflow
	testFiles := createTestYAMLFilesWithManyEndpoints(t)
	defer cleanupTestYAMLFiles(testFiles)

	metricsFiles := runParallelVenomTests(t, testFiles)
	defer cleanupMetricsFiles(metricsFiles)

	// Test with low cardinality limit
	aggregatedFile := "aggregated_cardinality_test.json"
	err := runDMAAggregationWithOptions(t, metricsFiles, aggregatedFile, "--max-endpoints", "3", "--no-bucket")
	if err != nil {
		t.Fatalf("DMA aggregation with cardinality control failed: %v", err)
	}
	defer os.Remove(aggregatedFile)

	// Verify cardinality control worked
	result, err := verifyAggregatedOutput(t, aggregatedFile)
	if err != nil {
		t.Fatalf("Failed to verify aggregated output: %v", err)
	}

	// Should have very few endpoints due to cardinality limit
	endpointCount := 0
	for name := range result.Metrics {
		if !isGlobalMetric(name) {
			endpointCount++
		}
	}

	if endpointCount > 3 {
		t.Errorf("Expected at most 3 endpoints with max-endpoints=3, got %d", endpointCount)
	}

	// Should not have "other" bucket with no-bucket option
	if _, exists := result.Metrics["other"]; exists {
		t.Error("Should not have 'other' bucket with no-bucket option")
	}
}

func TestDMAPerformance(t *testing.T) {
	// Test performance with many files
	testFiles := createManyTestYAMLFiles(t, 10)
	defer cleanupTestYAMLFiles(testFiles)

	start := time.Now()
	metricsFiles := runParallelVenomTests(t, testFiles)
	defer cleanupMetricsFiles(metricsFiles)

	aggregatedFile := "aggregated_performance_test.json"
	err := runDMAAggregation(t, metricsFiles, aggregatedFile)
	if err != nil {
		t.Fatalf("DMA aggregation failed: %v", err)
	}
	defer os.Remove(aggregatedFile)

	duration := time.Since(start)
	t.Logf("DMA workflow completed in %v", duration)

	// Should complete within reasonable time (< 30 seconds for 10 files)
	if duration > 30*time.Second {
		t.Errorf("DMA workflow took too long: %v", duration)
	}
}

// Helper functions

func createTestYAMLFiles(t *testing.T) []string {
	files := []string{}

	for i := 1; i <= 3; i++ {
		content := fmt.Sprintf(`name: Integration Test %d

testcases:
  - name: HTTP Test %d
    steps:
      - name: Status 200
        type: http
        method: GET
        url: "https://httpbin.org/status/200"
        assertions:
          - result.statuscode ShouldEqual 200

      - name: Delay %d
        type: http
        method: GET
        url: "https://httpbin.org/delay/%d"
        assertions:
          - result.statuscode ShouldEqual 200

      - name: Status 404
        type: http
        method: GET
        url: "https://httpbin.org/status/404"
        assertions:
          - result.statuscode ShouldEqual 404
`, i, i, i, i)

		filename := fmt.Sprintf("integration_test_%d.yml", i)
		err := os.WriteFile(filename, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test YAML file %s: %v", filename, err)
		}
		files = append(files, filename)
	}

	return files
}

func createTestYAMLFilesWithManyEndpoints(t *testing.T) []string {
	files := []string{}

	for i := 1; i <= 5; i++ {
		content := fmt.Sprintf(`name: Cardinality Test %d

testcases:
  - name: Many Endpoints %d
    steps:
`, i, i)

		// Add many different endpoints
		for j := 1; j <= 10; j++ {
			content += fmt.Sprintf(`      - name: Endpoint %d_%d
        type: http
        method: GET
        url: "https://httpbin.org/status/%d"
        assertions:
          - result.statuscode ShouldEqual %d

`, i, j, 200+j, 200+j)
		}

		filename := fmt.Sprintf("cardinality_test_%d.yml", i)
		err := os.WriteFile(filename, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test YAML file %s: %v", filename, err)
		}
		files = append(files, filename)
	}

	return files
}

func createManyTestYAMLFiles(t *testing.T, count int) []string {
	files := []string{}

	for i := 1; i <= count; i++ {
		content := fmt.Sprintf(`name: Performance Test %d

testcases:
  - name: Simple Test %d
    steps:
      - name: Status 200
        type: http
        method: GET
        url: "https://httpbin.org/status/200"
        assertions:
          - result.statuscode ShouldEqual 200
`, i, i)

		filename := fmt.Sprintf("performance_test_%d.yml", i)
		err := os.WriteFile(filename, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test YAML file %s: %v", filename, err)
		}
		files = append(files, filename)
	}

	return files
}

func runParallelVenomTests(t *testing.T, testFiles []string) []string {
	metricsFiles := []string{}

	// Run tests in parallel (simulating GNU Parallel)
	for i, testFile := range testFiles {
		metricsFile := fmt.Sprintf("metrics_%d.json", i+1)
		metricsFiles = append(metricsFiles, metricsFile)

		cmd := exec.Command("./venom", "run", testFile, "--metrics-enabled", "--metrics-output", metricsFile)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Venom test output: %s", string(output))
			t.Fatalf("Venom test failed for %s: %v", testFile, err)
		}
	}

	return metricsFiles
}

func runDMAAggregation(t *testing.T, metricsFiles []string, outputFile string) error {
	// Build the aggregator if not already built
	cmd := exec.Command("go", "build", "-o", "venom-aggregate", "cmd/venom/aggregate/main.go")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to build aggregator: %v", err)
	}

	// Run aggregation
	args := append([]string{"-output", outputFile}, metricsFiles...)
	cmd = exec.Command("./venom-aggregate", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aggregation failed: %v\nOutput: %s", err, string(output))
	}

	return nil
}

func runDMAAggregationWithOptions(t *testing.T, metricsFiles []string, outputFile string, options ...string) error {
	// Build the aggregator if not already built
	cmd := exec.Command("go", "build", "-o", "venom-aggregate", "cmd/venom/aggregate/main.go")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to build aggregator: %v", err)
	}

	// Run aggregation with options
	args := append([]string{"-output", outputFile}, options...)
	args = append(args, metricsFiles...)
	cmd = exec.Command("./venom-aggregate", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aggregation failed: %v\nOutput: %s", err, string(output))
	}

	return nil
}

func verifyAggregatedOutput(t *testing.T, aggregatedFile string) (*Metrics, error) {
	data, err := os.ReadFile(aggregatedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read aggregated file: %v", err)
	}

	var result Metrics
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal aggregated file: %v", err)
	}

	return &result, nil
}

func verifyAggregationResults(t *testing.T, result *Metrics, expectedTestCount int) {
	// Verify basic structure
	if result.RootGroup == nil {
		t.Error("RootGroup should not be nil")
	}

	if len(result.Metrics) == 0 {
		t.Error("Metrics should not be empty")
	}

	// Verify we have metrics from all test files
	// Each test file should contribute to the total
	totalChecks := 0
	for _, check := range result.RootGroup.Checks {
		totalChecks += int(check.Passes + check.Fails)
	}

	// Each test has 3 steps, so we expect 3 * testCount total checks
	expectedChecks := 3 * expectedTestCount
	if totalChecks < expectedChecks {
		t.Errorf("Expected at least %d total checks, got %d", expectedChecks, totalChecks)
	}

	// Verify we have global metrics
	globalMetricsFound := 0
	expectedGlobalMetrics := []string{"http_req_duration", "http_reqs", "iterations", "checks"}
	for _, metric := range expectedGlobalMetrics {
		if _, exists := result.Metrics[metric]; exists {
			globalMetricsFound++
		}
	}

	if globalMetricsFound < 2 {
		t.Errorf("Expected at least 2 global metrics, found %d", globalMetricsFound)
	}
}

func cleanupTestYAMLFiles(files []string) {
	for _, file := range files {
		os.Remove(file)
	}
}

func cleanupMetricsFiles(files []string) {
	for _, file := range files {
		os.Remove(file)
	}
}
