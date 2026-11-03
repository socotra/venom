package metricsreport

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ovh/venom/reporting"
	"github.com/ovh/venom/reporting/aggregator"
)

var Cmd = &cobra.Command{
	Use:   "metrics-report [flags] metrics_*.json",
	Short: "Aggregate metrics files and generate reports",
	Long: `Aggregate multiple metrics files and generate reports in various formats.

This command combines aggregation and reporting functionality, allowing you to:
- Aggregate multiple metrics files from parallel Venom runs
- Generate HTML reports with interactive visualizations
- Output JSON data for further processing
- Check performance thresholds and generate JUnit XML for CI
- Control which outputs are generated

Note: By default, threshold breaches are reported but don't cause the command to fail.
Use --fail-on-breaches to exit with error code on threshold violations.

Examples:
  # Basic aggregation and HTML report
  venom metrics-report metrics_*.json

  # Generate only HTML report (skip JSON file)
  venom metrics-report metrics_*.json --html-only

  # Generate only JSON (skip HTML)
  venom metrics-report metrics_*.json --json-only

  # Custom output files
  venom metrics-report metrics_*.json -o aggregated.json --html-output report.html

  # Check thresholds with custom config
  venom metrics-report metrics_*.json --check-thresholds --thresholds my_thresholds.yml

  # Generate JUnit XML for CI integration
  venom metrics-report metrics_*.json --check-thresholds --junit results.xml

  # Fail on breaches (exit with error code on violations)
  venom metrics-report metrics_*.json --check-thresholds --fail-on-breaches

  # With aggregation options
  venom metrics-report metrics_*.json --max-endpoints=5000 --html-only`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMetricsReport,
}

var (
	// Output options
	jsonOutput string
	htmlOutput string
	textOutput string
	jsonOnly   bool
	htmlOnly   bool

	// Aggregation options
	maxEndpoints     int
	noBucket         bool
	mergePercentiles string

	// Threshold checking options
	checkThresholds bool
	thresholdsFile  string
	junitOutput     string
	failOnBreaches  bool
)

func init() {
	// Output flags
	Cmd.Flags().StringVarP(&jsonOutput, "output", "o", "aggregated_metrics.json", "JSON output file path")
	Cmd.Flags().StringVar(&htmlOutput, "html-output", "metrics_report.html", "HTML output file path")
	Cmd.Flags().StringVar(&textOutput, "text-output", "metrics_summary.txt", "Text summary output file path")
	Cmd.Flags().BoolVar(&jsonOnly, "json-only", false, "Generate only JSON output")
	Cmd.Flags().BoolVar(&htmlOnly, "html-only", false, "Generate only HTML output")

	// Aggregation flags
	Cmd.Flags().IntVar(&maxEndpoints, "max-endpoints", 2000, "Maximum unique endpoints allowed")
	Cmd.Flags().BoolVar(&noBucket, "no-bucket", false, "Drop overflow endpoints instead of bucketing into 'other'")
	Cmd.Flags().StringVar(&mergePercentiles, "merge-percentiles", "weighted", "Merge strategy for percentiles (weighted|sketch)")

	// Threshold checking flags
	Cmd.Flags().BoolVar(&checkThresholds, "check-thresholds", false, "Check metrics against threshold configuration")
	Cmd.Flags().StringVar(&thresholdsFile, "thresholds", "thresholds.yml", "Threshold configuration file path")
	Cmd.Flags().StringVar(&junitOutput, "junit", "", "JUnit XML output file for threshold breaches")
	Cmd.Flags().BoolVar(&failOnBreaches, "fail-on-breaches", false, "Exit with error code on threshold breaches (default: soft fail)")
}

func runMetricsReport(cmd *cobra.Command, args []string) error {
	// Validate flags
	if jsonOnly && htmlOnly {
		return fmt.Errorf("cannot specify both --json-only and --html-only")
	}

	if mergePercentiles != "weighted" && mergePercentiles != "sketch" {
		return fmt.Errorf("invalid merge-percentiles value. Must be 'weighted' or 'sketch'")
	}

	// Expand glob patterns
	var inputFiles []string
	for _, pattern := range args {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("error expanding pattern %s: %w", pattern, err)
		}
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: No files match pattern %s\n", pattern)
		}
		inputFiles = append(inputFiles, matches...)
	}

	if len(inputFiles) == 0 {
		return fmt.Errorf("no input files found")
	}

	fmt.Printf("Processing %d metrics files...\n", len(inputFiles))

	// Create aggregator configuration
	config := &aggregator.Config{
		MaxEndpoints:     maxEndpoints,
		NoBucket:         noBucket,
		MergePercentiles: mergePercentiles,
	}

	// Run aggregation
	result, err := aggregator.AggregateFiles(inputFiles, config)
	if err != nil {
		return fmt.Errorf("error aggregating metrics: %w", err)
	}

	fmt.Printf("Successfully aggregated %d files\n", len(inputFiles))
	fmt.Printf("Total endpoints: %d\n", len(result.Metrics))
	fmt.Printf("Total checks: %d\n", len(result.RootGroup.Checks))

	// Determine what outputs to generate
	generateJSON := !htmlOnly
	generateHTML := !jsonOnly

	// Generate JSON output
	if generateJSON {
		err = aggregator.WriteOutput(result, jsonOutput)
		if err != nil {
			return fmt.Errorf("error writing JSON output: %w", err)
		}
		fmt.Printf("JSON report generated: %s\n", jsonOutput)
	}

	// Generate HTML output
	if generateHTML {
		// Load threshold configuration for HTML report (optional)
		var thresholdConfig *reporting.ThresholdConfig

		// Try to load thresholds from specified file first, then fallback to thresholds.yml, then defaults
		if thresholdsFile != "" {
			// Load from specified file
			thresholdConfig, err = reporting.LoadThresholdConfig(thresholdsFile)
			if err != nil {
				return fmt.Errorf("failed to load threshold config from %s: %w", thresholdsFile, err)
			}
			fmt.Printf("Using threshold configuration from %s for HTML report\n", thresholdsFile)
		} else {
			// Try to load thresholds.yml from current directory, fallback to defaults
			if _, err := os.Stat("thresholds.yml"); err == nil {
				thresholdConfig, err = reporting.LoadThresholdConfig("thresholds.yml")
				if err != nil {
					// If loading fails, use defaults instead of failing
					fmt.Printf("Warning: failed to load thresholds.yml, using default configuration: %v\n", err)
					thresholdConfig = reporting.DefaultThresholdConfig()
				} else {
					fmt.Printf("Using threshold configuration from thresholds.yml for HTML report\n")
				}
			} else {
				// Use default configuration
				thresholdConfig = reporting.DefaultThresholdConfig()
				fmt.Printf("Using default threshold configuration for HTML report\n")
			}
		}

		err = reporting.GenerateMetricsHTMLReportWithThresholds(result, htmlOutput, thresholdConfig)
		if err != nil {
			return fmt.Errorf("error generating HTML report: %w", err)
		}
		fmt.Printf("HTML report generated: %s\n", htmlOutput)
	}

	// Check thresholds if requested (before generating summary to get status)
	var thresholdStatus string
	var thresholdsConfigured bool
	var breaches []reporting.ThresholdBreach
	if checkThresholds {
		status, configured, breachList, err := checkThresholdBreaches(result)
		if err != nil {
			return fmt.Errorf("error checking thresholds: %w", err)
		}
		thresholdStatus = status
		thresholdsConfigured = configured
		breaches = breachList
	}

	// Generate text summary
	err = generateTextSummary(result, thresholdStatus, thresholdsConfigured, breaches, textOutput)
	if err != nil {
		return fmt.Errorf("error generating text summary: %w", err)
	}
	fmt.Printf("Text summary generated: %s\n", textOutput)

	return nil
}

func checkThresholdBreaches(metrics *aggregator.Metrics) (string, bool, []reporting.ThresholdBreach, error) {
	// Load threshold configuration
	var config *reporting.ThresholdConfig
	var err error
	var thresholdsConfigured bool

	if thresholdsFile != "" {
		// Load from specified file
		config, err = reporting.LoadThresholdConfig(thresholdsFile)
		if err != nil {
			return "", false, nil, fmt.Errorf("failed to load threshold config from %s: %w", thresholdsFile, err)
		}
		thresholdsConfigured = true
	} else {
		// Try to load thresholds.yml from current directory, fallback to defaults
		if _, err := os.Stat("thresholds.yml"); err == nil {
			config, err = reporting.LoadThresholdConfig("thresholds.yml")
			if err != nil {
				return "", false, nil, fmt.Errorf("failed to load threshold config from thresholds.yml: %w", err)
			}
			fmt.Printf("Using threshold configuration from thresholds.yml\n")
			thresholdsConfigured = true
		} else {
			// Use default configuration - not considered "configured"
			config = reporting.DefaultThresholdConfig()
			fmt.Printf("Using default threshold configuration\n")
			thresholdsConfigured = false
		}
	}

	// Convert aggregator.Metrics to reporting.Metrics for validation
	reportingMetrics := &reporting.Metrics{
		RootGroup: convertTestGroup(metrics.RootGroup),
		Metrics:   convertMetrics(metrics.Metrics),
		SetupData: metrics.SetupData,
		StartTime: metrics.StartTime,
		EndTime:   metrics.EndTime,
	}

	// Validate thresholds
	breaches := config.ValidateThresholds(reportingMetrics)
	summary := config.GetBreachSummary(breaches)

	// Print summary
	fmt.Printf("\n=== Threshold Validation Results ===\n")
	fmt.Printf("Total breaches: %d\n", summary["total"])
	fmt.Printf("Errors: %d\n", summary["error"])
	fmt.Printf("Warnings: %d\n", summary["warning"])

	if len(breaches) > 0 {
		fmt.Printf("\nBreaches:\n")
		for _, v := range breaches {
			fmt.Printf("  %s [%s] %s: %.2f%s (threshold: %.2f%s, samples: %d)\n",
				v.Severity, v.Endpoint, v.Metric, v.Value, v.Unit, v.Threshold, v.Unit, v.SampleCount)
		}

		// Generate JUnit XML if requested
		if junitOutput != "" {
			err = generateJUnitXML(breaches, junitOutput)
			if err != nil {
				return "❌ Fail", thresholdsConfigured, breaches, fmt.Errorf("failed to generate JUnit XML: %w", err)
			}
			fmt.Printf("JUnit XML report generated: %s\n", junitOutput)
		}

		// Determine status based on errors
		status := "⚠️  Warning"
		if summary["error"] > 0 {
			status = "❌ Fail"
		}

		// Exit with error code only if fail-on-breaches is explicitly enabled
		if failOnBreaches {
			return status, thresholdsConfigured, breaches, fmt.Errorf("threshold breaches detected: %d errors, %d warnings", summary["error"], summary["warning"])
		}

		return status, thresholdsConfigured, breaches, nil
	} else {
		fmt.Printf("✅ All thresholds passed!\n")
		return "✅ Pass", thresholdsConfigured, []reporting.ThresholdBreach{}, nil
	}
}

// convertTestGroup converts aggregator.TestGroup to reporting.TestGroup
func convertTestGroup(ag *aggregator.TestGroup) *reporting.TestGroup {
	if ag == nil {
		return nil
	}

	vg := &reporting.TestGroup{
		Name:   ag.Name,
		Path:   ag.Path,
		ID:     ag.ID,
		Groups: make(map[string]*reporting.TestGroup),
		Checks: make(map[string]*reporting.TestCheck),
	}

	for k, v := range ag.Groups {
		vg.Groups[k] = convertTestGroup(v)
	}

	for k, v := range ag.Checks {
		vg.Checks[k] = &reporting.TestCheck{
			Name:   v.Name,
			Path:   v.Path,
			ID:     v.ID,
			Passes: v.Passes,
			Fails:  v.Fails,
		}
	}

	return vg
}

// convertMetrics converts aggregator.Metrics map to reporting.Metrics map
func convertMetrics(am map[string]*aggregator.Metric) map[string]*reporting.Metric {
	vm := make(map[string]*reporting.Metric)

	for k, v := range am {
		vm[k] = &reporting.Metric{
			Type:   v.Type,
			Values: v.Values,
		}
	}

	return vm
}

func generateJUnitXML(breaches []reporting.ThresholdBreach, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Count tests and failures
	totalTests := len(breaches)
	failures := 0
	for _, v := range breaches {
		if v.Severity == "error" {
			failures++
		}
	}

	// Write JUnit XML header
	fmt.Fprintf(file, `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="venom-thresholds" tests="%d" failures="%d" time="0">
`, totalTests, failures)

	// Write test cases for each violation
	for _, v := range breaches {
		fmt.Fprintf(file, `  <testcase name="%s - %s" classname="thresholds">
    <failure message="Threshold violation: %.2f%s exceeds %.2f%s (samples: %d)" type="threshold">
%s: %s - %s violation
Value: %.2f%s
Threshold: %.2f%s
Samples: %d
    </failure>
  </testcase>
`, v.Endpoint, v.Metric, v.Value, v.Unit, v.Threshold, v.Unit, v.SampleCount,
			v.Severity, v.Endpoint, v.Metric, v.Value, v.Unit, v.Threshold, v.Unit, v.SampleCount)
	}

	// Write JUnit XML footer
	fmt.Fprintf(file, "</testsuite>\n")

	return nil
}

func generateTextSummary(metrics *aggregator.Metrics, thresholdStatus string, thresholdsConfigured bool, breaches []reporting.ThresholdBreach, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create text output file: %w", err)
	}
	defer file.Close()

	fmt.Fprintln(file, "⚡ Performance Metrics (Venom)")

	// Get HTTP requests count
	totalRequests := int64(0)
	if httpReqs, exists := metrics.Metrics["http_reqs"]; exists {
		if count, ok := httpReqs.Values["count"].(float64); ok {
			totalRequests = int64(count)
		} else if count, ok := httpReqs.Values["count"].(int64); ok {
			totalRequests = count
		}
	}

	// Get HTTP duration metrics
	avgResponseTime := 0.0
	p95 := 0.0
	p99 := 0.0
	minTime := 0.0
	maxTime := 0.0

	if httpDuration, exists := metrics.Metrics["http_req_duration"]; exists {
		if avg, ok := httpDuration.Values["avg"].(float64); ok {
			avgResponseTime = avg
		}
		if p95Val, ok := httpDuration.Values["p(95)"].(float64); ok {
			p95 = p95Val
		}
		if p99Val, ok := httpDuration.Values["p(99)"].(float64); ok {
			p99 = p99Val
		}
		if min, ok := httpDuration.Values["min"].(float64); ok {
			minTime = min
		}
		if max, ok := httpDuration.Values["max"].(float64); ok {
			maxTime = max
		}
	}

	// Get HTTP failures
	httpFailures := int64(0)
	failureRate := 0.0
	if httpFailed, exists := metrics.Metrics["http_req_failed"]; exists {
		if fails, ok := httpFailed.Values["fails"].(float64); ok {
			httpFailures = int64(fails)
		} else if fails, ok := httpFailed.Values["fails"].(int64); ok {
			httpFailures = fails
		}
		if totalRequests > 0 {
			failureRate = float64(httpFailures) / float64(totalRequests) * 100
		}
	}

	// Calculate test duration
	testDuration := time.Duration(0)
	if !metrics.StartTime.IsZero() && !metrics.EndTime.IsZero() {
		testDuration = metrics.EndTime.Sub(metrics.StartTime)
	}

	// Find top 5 slowest endpoints
	type endpointStat struct {
		name string
		p95  float64
	}
	var endpointStats []endpointStat

	// Create a set of endpoints that have breaches (if thresholds are configured)
	breachingEndpoints := make(map[string]bool)
	if thresholdsConfigured && len(breaches) > 0 {
		for _, breach := range breaches {
			breachingEndpoints[breach.Endpoint] = true
		}
	}

	globalMetrics := []string{
		"checks", "data_received", "data_sent", "http_req_duration",
		"http_req_failed", "http_reqs", "iterations", "vus", "vus_max",
		"http_req_blocked", "http_req_connecting", "http_req_sending",
		"http_req_waiting", "http_req_receiving", "http_req_tls_handshaking",
	}

	for metricName, metric := range metrics.Metrics {
		// Skip global metrics
		isGlobal := false
		for _, global := range globalMetrics {
			if metricName == global || strings.HasPrefix(metricName, global+"_") {
				isGlobal = true
				break
			}
		}
		if isGlobal {
			continue
		}

		// Only process trend metrics (endpoint duration metrics)
		if metric.Type == "trend" {
			// Get P95 value for sorting
			if p95, ok := metric.Values["p(95)"].(float64); ok && p95 > 0 {
				// If thresholds are configured, only include endpoints that breach thresholds
				// Otherwise, include all endpoints
				if !thresholdsConfigured || breachingEndpoints[metricName] {
					endpointStats = append(endpointStats, endpointStat{
						name: metricName,
						p95:  p95,
					})
				}
			}
		}
	}

	// Sort by P95 response time (descending)
	sort.Slice(endpointStats, func(i, j int) bool {
		return endpointStats[i].p95 > endpointStats[j].p95
	})

	// Print summary
	fmt.Fprintf(file, "• Total HTTP Requests: %d\n", totalRequests)
	fmt.Fprintf(file, "• Avg Response Time: %.0f ms (P95: %.0f ms, P99: %.0f ms)\n", avgResponseTime, p95, p99)
	fmt.Fprintf(file, "• Min/Max: %.0f ms / %.0f ms\n", minTime, maxTime)
	fmt.Fprintf(file, "• HTTP Failures: %d (%.2f%% failure rate)\n", httpFailures, failureRate)

	// Only show threshold status if thresholds are configured
	if thresholdsConfigured {
		fmt.Fprintf(file, "• Threshold Status: %s\n", thresholdStatus)
	}

	// Format duration
	durationMinutes := testDuration.Minutes()
	if durationMinutes < 1 {
		fmt.Fprintf(file, "• Test Duration: %.1f sec\n", testDuration.Seconds())
	} else {
		fmt.Fprintf(file, "• Test Duration: %.1f min\n", durationMinutes)
	}

	// Print top 5 slowest endpoints
	if len(endpointStats) > 0 {
		fmt.Fprintln(file, "\nTop 5 Slowest Endpoints:")
		topN := 5
		if len(endpointStats) < topN {
			topN = len(endpointStats)
		}
		for i := 0; i < topN; i++ {
			fmt.Fprintf(file, "  %d. %s: %.0f ms (P95)\n", i+1, endpointStats[i].name, endpointStats[i].p95)
		}
	}

	return nil
}
