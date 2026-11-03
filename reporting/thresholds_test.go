package reporting

import (
	"testing"
	"time"
)

func TestDefaultThresholdConfig(t *testing.T) {
	config := DefaultThresholdConfig()

	if config.Defaults.P95 == nil {
		t.Error("Default P95 threshold should be set")
	}

	if config.Defaults.P95.Value != 500*time.Millisecond {
		t.Errorf("Expected default P95 to be 500ms, got %v", config.Defaults.P95.Value)
	}

	if config.Options.TolerancePercent != 10.0 {
		t.Errorf("Expected default tolerance to be 10%%, got %v", config.Options.TolerancePercent)
	}
}

func TestGetThresholdForEndpoint(t *testing.T) {
	config := DefaultThresholdConfig()

	// Test exact endpoint match
	thresholds := config.GetThresholdForEndpoint("GET /users")
	if thresholds.P95 == nil || thresholds.P95.Value != 300*time.Millisecond {
		t.Error("GET /users should have P95 threshold of 300ms")
	}

	// Test group pattern match
	thresholds = config.GetThresholdForEndpoint("auth/login")
	if thresholds.P95 == nil || thresholds.P95.Value != 350*time.Millisecond {
		t.Error("auth/* endpoints should have P95 threshold of 350ms")
	}

	// Test default fallback
	thresholds = config.GetThresholdForEndpoint("GET /unknown")
	if thresholds.P95 == nil || thresholds.P95.Value != 500*time.Millisecond {
		t.Error("Unknown endpoints should use default P95 threshold of 500ms")
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		endpoint string
		pattern  string
		expected bool
	}{
		{"auth/login", "auth/*", true},
		{"auth/logout", "auth/*", true},
		{"catalog/products", "catalog/*", true},
		{"GET /users/profile", "auth/*", false},
		{"GET /users", "GET /users", true},
		{"POST /users", "GET /users", false},
	}

	for _, test := range tests {
		result := matchesPattern(test.endpoint, test.pattern)
		if result != test.expected {
			t.Errorf("matchesPattern(%q, %q) = %v, expected %v",
				test.endpoint, test.pattern, result, test.expected)
		}
	}
}

func TestValidateThresholds(t *testing.T) {
	config := DefaultThresholdConfig()

	// Create test metrics with a breach
	metrics := &Metrics{
		Metrics: map[string]*Metric{
			"GET /users": {
				Type: "trend",
				Values: map[string]interface{}{
					"p(95)": 400.0,      // Exceeds 300ms threshold
					"avg":   120.0,      // Within 150ms threshold
					"count": int64(150), // Above min samples
				},
			},
			"GET /slow": {
				Type: "trend",
				Values: map[string]interface{}{
					"p(95)": 600.0,     // Exceeds default 500ms threshold
					"count": int64(50), // Below min samples - should be skipped
				},
			},
		},
	}

	breaches := config.ValidateThresholds(metrics)

	if len(breaches) != 1 {
		t.Errorf("Expected 1 breach, got %d", len(breaches))
	}

	if len(breaches) > 0 {
		v := breaches[0]
		if v.Endpoint != "GET /users" {
			t.Errorf("Expected breach for GET /users, got %s", v.Endpoint)
		}
		if v.Metric != "p(95)" {
			t.Errorf("Expected p(95) breach, got %s", v.Metric)
		}
		if v.Value != 400.0 {
			t.Errorf("Expected breach value 400.0, got %v", v.Value)
		}
	}
}

func TestMergeThresholdValues(t *testing.T) {
	base := ThresholdValues{
		P95: &DurationThreshold{Value: 500 * time.Millisecond},
		Avg: &DurationThreshold{Value: 200 * time.Millisecond},
	}

	override := ThresholdValues{
		P95: &DurationThreshold{Value: 300 * time.Millisecond},
		P99: &DurationThreshold{Value: 1000 * time.Millisecond},
	}

	result := mergeThresholdValues(base, override)

	// P95 should be overridden
	if result.P95.Value != 300*time.Millisecond {
		t.Errorf("Expected P95 to be overridden to 300ms, got %v", result.P95.Value)
	}

	// Avg should remain from base
	if result.Avg.Value != 200*time.Millisecond {
		t.Errorf("Expected Avg to remain 200ms, got %v", result.Avg.Value)
	}

	// P99 should be added from override
	if result.P99.Value != 1000*time.Millisecond {
		t.Errorf("Expected P99 to be added as 1000ms, got %v", result.P99.Value)
	}
}

func TestIsEndpointMetric(t *testing.T) {
	tests := []struct {
		metricName string
		expected   bool
	}{
		{"GET /users", true},
		{"POST /orders", true},
		{"http_req_duration", false},
		{"http_reqs", false},
		{"http_req_failed", false},
		{"iterations", false},
		{"checks", false},
		{"data_sent", false},
		{"data_received", false},
		{"vus", false},
		{"vus_max", false},
	}

	for _, test := range tests {
		result := isEndpointMetric(test.metricName)
		if result != test.expected {
			t.Errorf("isEndpointMetric(%q) = %v, expected %v",
				test.metricName, result, test.expected)
		}
	}
}

func TestGetBreachSummary(t *testing.T) {
	config := DefaultThresholdConfig()

	breaches := []ThresholdBreach{
		{Severity: "error"},
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "warning"},
		{Severity: "warning"},
	}

	summary := config.GetBreachSummary(breaches)

	if summary["total"] != 5 {
		t.Errorf("Expected total breaches 5, got %d", summary["total"])
	}
	if summary["error"] != 2 {
		t.Errorf("Expected error breaches 2, got %d", summary["error"])
	}
	if summary["warning"] != 3 {
		t.Errorf("Expected warning breaches 3, got %d", summary["warning"])
	}
}
