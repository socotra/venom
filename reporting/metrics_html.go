package reporting

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"

	"github.com/ovh/venom/reporting/aggregator"
)

//go:embed metrics_html_template.html
var templateContent embed.FS

func GenerateMetricsHTMLReport(metrics *aggregator.Metrics, outputFile string) error {
	return GenerateMetricsHTMLReportWithThresholds(metrics, outputFile, nil)
}

func GenerateMetricsHTMLReportWithThresholds(metrics *aggregator.Metrics, outputFile string, thresholdConfig *ThresholdConfig) error {
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics to JSON: %w", err)
	}

	// Use default threshold config if none provided
	if thresholdConfig == nil {
		thresholdConfig = DefaultThresholdConfig()
	}

	// Convert threshold config to JavaScript-compatible format
	jsThresholds := convertThresholdsForJS(thresholdConfig)
	thresholdsJSON, err := json.Marshal(jsThresholds)
	if err != nil {
		return fmt.Errorf("failed to marshal thresholds to JSON: %w", err)
	}

	templateData, err := templateContent.ReadFile("metrics_html_template.html")
	if err != nil {
		return fmt.Errorf("failed to read template file: %w", err)
	}

	tmpl, err := template.New("metrics_html_report").Parse(string(templateData))
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	data := struct {
		MetricsJSON    template.JS
		ThresholdsJSON template.JS
	}{
		MetricsJSON:    template.JS(metricsJSON),
		ThresholdsJSON: template.JS(thresholdsJSON),
	}

	err = tmpl.Execute(file, data)
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

// JSThresholdValues represents threshold values in JavaScript-compatible format
type JSThresholdValues struct {
	P50        *float64 `json:"p50,omitempty"`
	P90        *float64 `json:"p90,omitempty"`
	P95        *float64 `json:"p95,omitempty"`
	P99        *float64 `json:"p99,omitempty"`
	Avg        *float64 `json:"avg,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	ErrorRate  *float64 `json:"error_rate,omitempty"`
	RPS        *float64 `json:"rps,omitempty"`
	MinSamples *int     `json:"min_samples,omitempty"`
}

// JSThresholdConfig represents the complete threshold configuration in JavaScript-compatible format
type JSThresholdConfig struct {
	Defaults  JSThresholdValues            `json:"defaults"`
	Groups    map[string]JSThresholdValues `json:"groups"`
	Endpoints map[string]JSThresholdValues `json:"endpoints"`
	Options   struct {
		TolerancePercent float64 `json:"tolerance_percent"`
		MinSamples       int     `json:"min_samples"`
		SoftFail         bool    `json:"soft_fail"`
	} `json:"options"`
}

// convertThresholdsForJS converts Go threshold config to JavaScript-compatible format
func convertThresholdsForJS(config *ThresholdConfig) *JSThresholdConfig {
	jsConfig := &JSThresholdConfig{
		Defaults:  convertThresholdValuesForJS(config.Defaults),
		Groups:    make(map[string]JSThresholdValues),
		Endpoints: make(map[string]JSThresholdValues),
		Options: struct {
			TolerancePercent float64 `json:"tolerance_percent"`
			MinSamples       int     `json:"min_samples"`
			SoftFail         bool    `json:"soft_fail"`
		}{
			TolerancePercent: config.Options.TolerancePercent,
			MinSamples:       config.Options.MinSamples,
			SoftFail:         config.Options.SoftFail,
		},
	}

	// Convert groups
	for name, values := range config.Groups {
		jsConfig.Groups[name] = convertThresholdValuesForJS(values)
	}

	// Convert endpoints
	for name, values := range config.Endpoints {
		jsConfig.Endpoints[name] = convertThresholdValuesForJS(values)
	}

	return jsConfig
}

// convertThresholdValuesForJS converts ThresholdValues to JavaScript-compatible format
func convertThresholdValuesForJS(values ThresholdValues) JSThresholdValues {
	jsValues := JSThresholdValues{}

	// Convert duration thresholds to milliseconds
	if values.P50 != nil {
		ms := float64(values.P50.Value.Nanoseconds()) / 1e6
		jsValues.P50 = &ms
	}
	if values.P90 != nil {
		ms := float64(values.P90.Value.Nanoseconds()) / 1e6
		jsValues.P90 = &ms
	}
	if values.P95 != nil {
		ms := float64(values.P95.Value.Nanoseconds()) / 1e6
		jsValues.P95 = &ms
	}
	if values.P99 != nil {
		ms := float64(values.P99.Value.Nanoseconds()) / 1e6
		jsValues.P99 = &ms
	}
	if values.Avg != nil {
		ms := float64(values.Avg.Value.Nanoseconds()) / 1e6
		jsValues.Avg = &ms
	}
	if values.Max != nil {
		ms := float64(values.Max.Value.Nanoseconds()) / 1e6
		jsValues.Max = &ms
	}

	// Convert rate thresholds (already in correct format)
	if values.ErrorRate != nil {
		jsValues.ErrorRate = &values.ErrorRate.Value
	}
	if values.RPS != nil {
		jsValues.RPS = &values.RPS.Value
	}

	// Copy min samples
	if values.MinSamples != nil {
		jsValues.MinSamples = values.MinSamples
	}

	return jsValues
}
