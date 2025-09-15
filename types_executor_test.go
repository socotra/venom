package venom

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindHeaderInUserExecutor(t *testing.T) {
	ctx := context.Background()

	// Create test case with preserved headers in result
	result := map[string]interface{}{
		"internal_headers.step_0_http.result.headers.Socotra-Request-Id": "test-request-id-456",
		"other_key": "other_value",
	}

	tc := &TestCase{}

	// Test finding header
	headerValue := findHeaderInUserExecutor(ctx, result, tc, "Socotra-Request-Id")
	assert.Equal(t, "test-request-id-456", headerValue, "Should find header in result map")

	// Test header not found
	notFoundValue := findHeaderInUserExecutor(ctx, result, tc, "Non-Existent-Header")
	assert.Equal(t, "", notFoundValue, "Should return empty string for non-existent header")
}

func TestHasHTTPSteps(t *testing.T) {
	v := &Venom{}

	tests := []struct {
		name     string
		steps    []TestStepResult
		expected bool
	}{
		{
			name: "has http step",
			steps: []TestStepResult{
				{Name: "http"},
				{Name: "other"},
			},
			expected: true,
		},
		{
			name: "no http step",
			steps: []TestStepResult{
				{Name: "other"},
				{Name: "another"},
			},
			expected: false,
		},
		{
			name: "case insensitive http",
			steps: []TestStepResult{
				{Name: "HTTP"},
				{Name: "other"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.hasHTTPSteps(tt.steps)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasPreservedHeaders(t *testing.T) {
	v := &Venom{}

	tests := []struct {
		name     string
		vars     H
		expected bool
	}{
		{
			name: "has preserved headers",
			vars: H{
				"internal_headers.step_0_http.Socotra-Request-Id": "test-id",
				"other_var": "value",
			},
			expected: true,
		},
		{
			name: "no preserved headers",
			vars: H{
				"other_var":   "value",
				"another_var": "value2",
			},
			expected: false,
		},
		{
			name:     "empty vars",
			vars:     H{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.hasPreservedHeaders(tt.vars)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPreserveHeadersFromComputedVars(t *testing.T) {
	InitTestLogger(t)
	ctx := context.Background()
	v := &Venom{}

	// Create a test case with computed vars containing headers
	tc := &TestCase{
		TestStepResults: []TestStepResult{
			{
				Name: "http",
				ComputedVars: H{
					"result.headers.Socotra-Request-Id": "test-request-id-789",
					"result.headers.Content-Type":       "application/json",
					"other_var":                         "value",
				},
			},
		},
		computedVars: H{},
	}

	// Test header preservation
	v.preserveHeadersFromInternalSteps(ctx, tc)

	// Verify headers were preserved from computed vars
	expectedKey := "internal_headers.step_0_http.result.headers.Socotra-Request-Id"
	expectedValue := "test-request-id-789"

	value, exists := tc.computedVars[expectedKey]
	assert.True(t, exists, "Header should be preserved from computed vars")
	assert.Equal(t, expectedValue, value, "Header value should match")
}
