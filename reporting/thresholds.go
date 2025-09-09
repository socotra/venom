package reporting

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ThresholdConfig represents the complete threshold configuration
type ThresholdConfig struct {
	Defaults ThresholdValues `json:"defaults" yaml:"defaults"`
	Groups   map[string]ThresholdValues `json:"groups" yaml:"groups"`
	Endpoints map[string]ThresholdValues `json:"endpoints" yaml:"endpoints"`
	Options  ThresholdOptions `json:"options" yaml:"options"`
}

// ThresholdValues defines the threshold values for various metrics
type ThresholdValues struct {
	P50    *DurationThreshold `json:"p50,omitempty" yaml:"p50,omitempty"`
	P90    *DurationThreshold `json:"p90,omitempty" yaml:"p90,omitempty"`
	P95    *DurationThreshold `json:"p95,omitempty" yaml:"p95,omitempty"`
	P99    *DurationThreshold `json:"p99,omitempty" yaml:"p99,omitempty"`
	Avg    *DurationThreshold `json:"avg,omitempty" yaml:"avg,omitempty"`
	Max    *DurationThreshold `json:"max,omitempty" yaml:"max,omitempty"`
	ErrorRate *RateThreshold `json:"error_rate,omitempty" yaml:"error_rate,omitempty"`
	RPS    *RateThreshold `json:"rps,omitempty" yaml:"rps,omitempty"`
	MinSamples *int `json:"min_samples,omitempty" yaml:"min_samples,omitempty"`
}

// DurationThreshold represents a duration-based threshold
type DurationThreshold struct {
	Value    time.Duration `json:"value" yaml:"value"`
	Tolerance *float64 `json:"tolerance_percent,omitempty" yaml:"tolerance_percent,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for DurationThreshold
func (dt *DurationThreshold) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		// Handle string duration like "500ms"
		durationStr := value.Value
		duration, err := time.ParseDuration(durationStr)
		if err != nil {
			return fmt.Errorf("invalid duration format '%s': %w", durationStr, err)
		}
		dt.Value = duration
		return nil
	} else if value.Kind == yaml.MappingNode {
		// Handle object format with value and tolerance
		var temp struct {
			Value    string   `yaml:"value"`
			Tolerance *float64 `yaml:"tolerance_percent,omitempty"`
		}
		if err := value.Decode(&temp); err != nil {
			return err
		}
		
		duration, err := time.ParseDuration(temp.Value)
		if err != nil {
			return fmt.Errorf("invalid duration format '%s': %w", temp.Value, err)
		}
		dt.Value = duration
		dt.Tolerance = temp.Tolerance
		return nil
	}
	return fmt.Errorf("invalid DurationThreshold format")
}

// RateThreshold represents a rate-based threshold (0.0 to 1.0 for rates, any value for RPS)
type RateThreshold struct {
	Value    float64 `json:"value" yaml:"value"`
	Tolerance *float64 `json:"tolerance_percent,omitempty" yaml:"tolerance_percent,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for RateThreshold
func (rt *RateThreshold) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		// Handle numeric value
		val, err := strconv.ParseFloat(value.Value, 64)
		if err != nil {
			return fmt.Errorf("invalid rate value '%s': %w", value.Value, err)
		}
		rt.Value = val
		return nil
	} else if value.Kind == yaml.MappingNode {
		// Handle object format with value and tolerance
		var temp struct {
			Value    float64  `yaml:"value"`
			Tolerance *float64 `yaml:"tolerance_percent,omitempty"`
		}
		if err := value.Decode(&temp); err != nil {
			return err
		}
		rt.Value = temp.Value
		rt.Tolerance = temp.Tolerance
		return nil
	}
	return fmt.Errorf("invalid RateThreshold format")
}

// ThresholdOptions defines global options for threshold validation
type ThresholdOptions struct {
	TolerancePercent float64 `json:"tolerance_percent" yaml:"tolerance_percent"`
	MinSamples       int     `json:"min_samples" yaml:"min_samples"`
	SoftFail         bool    `json:"soft_fail" yaml:"soft_fail"`
}

// ThresholdBreach represents a threshold breach
type ThresholdBreach struct {
	Endpoint    string `json:"endpoint"`
	Metric      string `json:"metric"`
	Value       float64 `json:"value"`
	Threshold   float64 `json:"threshold"`
	Unit        string `json:"unit"`
	Severity    string `json:"severity"`
	SampleCount int64  `json:"sample_count"`
}

// DefaultThresholdConfig returns a sensible default threshold configuration
func DefaultThresholdConfig() *ThresholdConfig {
	return &ThresholdConfig{
		Defaults: ThresholdValues{
			P95: &DurationThreshold{Value: 500 * time.Millisecond},
			P99: &DurationThreshold{Value: 1000 * time.Millisecond},
			Avg: &DurationThreshold{Value: 200 * time.Millisecond},
			ErrorRate: &RateThreshold{Value: 0.01}, // 1%
		},
		Groups: map[string]ThresholdValues{
			"auth/*": {
				P95: &DurationThreshold{Value: 350 * time.Millisecond},
			},
			"catalog/*": {
				P95: &DurationThreshold{Value: 450 * time.Millisecond},
			},
		},
		Endpoints: map[string]ThresholdValues{
			"GET /users": {
				P95: &DurationThreshold{Value: 300 * time.Millisecond},
				Avg: &DurationThreshold{Value: 150 * time.Millisecond},
			},
			"POST /orders": {
				P95: &DurationThreshold{Value: 800 * time.Millisecond},
				Avg: &DurationThreshold{Value: 400 * time.Millisecond},
			},
		},
		Options: ThresholdOptions{
			TolerancePercent: 10.0, // 10% tolerance by default
			MinSamples:       100,  // Require at least 100 samples for reliable percentiles
			SoftFail:         false,
		},
	}
}

// GetThresholdForEndpoint returns the effective threshold values for a given endpoint
// Priority: endpoints > groups > defaults
func (tc *ThresholdConfig) GetThresholdForEndpoint(endpoint string) ThresholdValues {
	result := tc.Defaults

	// Check group patterns (order matters - first match wins)
	for pattern, groupThresholds := range tc.Groups {
		if matchesPattern(endpoint, pattern) {
			result = mergeThresholdValues(result, groupThresholds)
			break
		}
	}

	// Check exact endpoint matches
	if endpointThresholds, exists := tc.Endpoints[endpoint]; exists {
		result = mergeThresholdValues(result, endpointThresholds)
	}

	return result
}

// matchesPattern checks if an endpoint matches a pattern (supports wildcards and basic regex)
func matchesPattern(endpoint, pattern string) bool {
	// Convert wildcard pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "*", ".*")
	regexPattern = "^" + regexPattern + "$"
	
	matched, err := regexp.MatchString(regexPattern, endpoint)
	if err != nil {
		// If regex compilation fails, fall back to exact match
		return endpoint == pattern
	}
	
	return matched
}

// mergeThresholdValues merges two threshold value sets, with the second taking precedence
func mergeThresholdValues(base, override ThresholdValues) ThresholdValues {
	result := base

	if override.P50 != nil {
		result.P50 = override.P50
	}
	if override.P90 != nil {
		result.P90 = override.P90
	}
	if override.P95 != nil {
		result.P95 = override.P95
	}
	if override.P99 != nil {
		result.P99 = override.P99
	}
	if override.Avg != nil {
		result.Avg = override.Avg
	}
	if override.Max != nil {
		result.Max = override.Max
	}
	if override.ErrorRate != nil {
		result.ErrorRate = override.ErrorRate
	}
	if override.RPS != nil {
		result.RPS = override.RPS
	}
	if override.MinSamples != nil {
		result.MinSamples = override.MinSamples
	}

	return result
}

// ValidateThresholds checks if metrics violate any thresholds
func (tc *ThresholdConfig) ValidateThresholds(metrics *Metrics) []ThresholdBreach {
	var breaches []ThresholdBreach

	// Get minimum samples requirement
	minSamples := tc.Options.MinSamples
	if minSamples == 0 {
		minSamples = 100 // Default minimum
	}

	// Check each endpoint metric
	for metricName, metric := range metrics.Metrics {
		// Skip non-endpoint metrics
		if !isEndpointMetric(metricName) {
			continue
		}
		

		endpoint := metricName
		thresholds := tc.GetThresholdForEndpoint(endpoint)

		// Check sample count first
		sampleCount := int64(0)
		if count, ok := metric.Values["count"].(int64); ok {
			sampleCount = count
		} else if count, ok := metric.Values["count"].(float64); ok {
			sampleCount = int64(count)
		}

		// Skip if not enough samples
		if sampleCount < int64(minSamples) {
			continue
		}

		// Check duration-based thresholds
		breaches = append(breaches, tc.checkDurationThresholds(endpoint, metric, thresholds, sampleCount)...)
		
		// Check rate-based thresholds
		breaches = append(breaches, tc.checkRateThresholds(endpoint, metric, thresholds, sampleCount)...)
	}

	return breaches
}

// isEndpointMetric checks if a metric name represents an endpoint metric
func isEndpointMetric(metricName string) bool {
	// Skip system metrics
	systemMetrics := []string{
		"http_req_duration", "http_reqs", "http_req_failed", "iterations", 
		"checks", "data_sent", "data_received", "vus", "vus_max",
	}
	
	for _, sys := range systemMetrics {
		if strings.HasPrefix(metricName, sys) {
			return false
		}
	}
	
	return true
}

// checkDurationThresholds checks duration-based thresholds (p50, p90, p95, p99, avg, max)
func (tc *ThresholdConfig) checkDurationThresholds(endpoint string, metric *Metric, thresholds ThresholdValues, sampleCount int64) []ThresholdBreach {
	var breaches []ThresholdBreach

	// Define duration threshold checks
	checks := []struct {
		key       string
		threshold *DurationThreshold
		unit      string
	}{
		{"p(50)", thresholds.P50, "ms"},
		{"p(90)", thresholds.P90, "ms"},
		{"p(95)", thresholds.P95, "ms"},
		{"p(99)", thresholds.P99, "ms"},
		{"avg", thresholds.Avg, "ms"},
		{"max", thresholds.Max, "ms"},
	}

	for _, check := range checks {
		if check.threshold == nil {
			continue
		}

		value, ok := metric.Values[check.key].(float64)
		if !ok {
			continue
		}

		thresholdMs := float64(check.threshold.Value.Milliseconds())
		tolerance := tc.Options.TolerancePercent
		if check.threshold.Tolerance != nil {
			tolerance = *check.threshold.Tolerance
		}

		// Apply tolerance
		effectiveThreshold := thresholdMs * (1 + tolerance/100)

		if value > effectiveThreshold {
			severity := "error"
			if value <= thresholdMs*(1+tolerance/100*1.5) {
				severity = "warning"
			}

			breaches = append(breaches, ThresholdBreach{
				Endpoint:    endpoint,
				Metric:      check.key,
				Value:       value,
				Threshold:   thresholdMs,
				Unit:        check.unit,
				Severity:    severity,
				SampleCount: sampleCount,
			})
		}
	}

	return breaches
}

// checkRateThresholds checks rate-based thresholds (error_rate, rps)
func (tc *ThresholdConfig) checkRateThresholds(endpoint string, metric *Metric, thresholds ThresholdValues, sampleCount int64) []ThresholdBreach {
	var breaches []ThresholdBreach

	// Check error rate
	if thresholds.ErrorRate != nil {
		// Calculate error rate from the metric
		errorRate := 0.0
		if fails, ok := metric.Values["fails"].(int64); ok {
			if total, ok := metric.Values["count"].(int64); ok && total > 0 {
				errorRate = float64(fails) / float64(total)
			}
		}

		threshold := thresholds.ErrorRate.Value
		tolerance := tc.Options.TolerancePercent
		if thresholds.ErrorRate.Tolerance != nil {
			tolerance = *thresholds.ErrorRate.Tolerance
		}

		effectiveThreshold := threshold * (1 + tolerance/100)

		if errorRate > effectiveThreshold {
			severity := "error"
			if errorRate <= threshold*(1+tolerance/100*1.5) {
				severity = "warning"
			}

			breaches = append(breaches, ThresholdBreach{
				Endpoint:    endpoint,
				Metric:      "error_rate",
				Value:       errorRate * 100, // Convert to percentage for display
				Threshold:   threshold * 100,
				Unit:        "%",
				Severity:    severity,
				SampleCount: sampleCount,
			})
		}
	}

	// Check RPS
	if thresholds.RPS != nil {
		if rate, ok := metric.Values["rate"].(float64); ok {
			threshold := thresholds.RPS.Value
			tolerance := tc.Options.TolerancePercent
			if thresholds.RPS.Tolerance != nil {
				tolerance = *thresholds.RPS.Tolerance
			}

			effectiveThreshold := threshold * (1 + tolerance/100)

			if rate > effectiveThreshold {
				severity := "error"
				if rate <= threshold*(1+tolerance/100*1.5) {
					severity = "warning"
				}

				breaches = append(breaches, ThresholdBreach{
					Endpoint:    endpoint,
					Metric:      "rps",
					Value:       rate,
					Threshold:   threshold,
					Unit:        "req/s",
					Severity:    severity,
					SampleCount: sampleCount,
				})
			}
		}
	}

	return breaches
}

// GetBreachSummary returns a summary of threshold breaches
func (tc *ThresholdConfig) GetBreachSummary(breaches []ThresholdBreach) map[string]int {
	summary := map[string]int{
		"total":    len(breaches),
		"error":    0,
		"warning":  0,
	}

	for _, v := range breaches {
		summary[v.Severity]++
	}

	return summary
}

// LoadThresholdConfig loads threshold configuration from a YAML file
func LoadThresholdConfig(filename string) (*ThresholdConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read threshold config file: %w", err)
	}

	var config ThresholdConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse threshold config YAML: %w", err)
	}

	// Set defaults if not provided
	if config.Options.TolerancePercent == 0 {
		config.Options.TolerancePercent = 10.0
	}
	if config.Options.MinSamples == 0 {
		config.Options.MinSamples = 100
	}

	return &config, nil
}
