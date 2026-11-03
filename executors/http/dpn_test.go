package http

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractSimpleEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
		notes    string
	}{
		// Core & Hygiene
		{"root path", "/", "root", "Root path"},
		{"simple health", "/health", "health", "Simple health"},
		{"k8s healthz", "/healthz", "healthz", "K8s style"},
		{"k8s readyz", "/readyz", "readyz", "K8s style"},
		{"status code", "/status/200", "status_200", "Keep meaningful 3-digit status"},
		{"metrics endpoint", "/metrics", "metrics", "Prom metrics endpoint"},
		{"double slashes", "/double//slashes///here", "double_slashes_here", "Collapse slashes"},
		{"trailing slash", "/trailing/slash/", "trailing_slash", "Trim trailing slash"},
		{"query fragment", "/path?query=1#frag", "path", "Drop query/fragment"},

		// API Prefixes & Versions
		{"api v1 users", "/api/v1/users", "users", "Drop api, v1"},
		{"rest v3 customers", "/rest/v3/customers/cust123", "customers", "Drop rest, v3, ID"},
		{"graphql v2 schema", "/graphql/v2/schema", "schema", "Drop graphql, v2"},
		{"api-v2 payments", "/api-v2/payments/charge", "payments_charge", "api-v2 as version/prefix"},
		{"date version", "/v2024-08-01/charges/abc123", "charges", "Date-style version dropped"},
		{"prefix after mount", "/svc/api/v1/orders/123456", "svc_orders", "Prefix after mount point"},

		// Method Token in Path (avoid GET_get_*)
		{"get method token", "/get/delay/1", "delay_1", "Drop leading method token"},
		{"post method token", "/post/data", "data", "Drop leading method token"},
		{"put method token", "/put/users/123", "users_123", "Drop leading method token"},

		// IDs (UUID/ULID/Mongo/numeric/mixed)
		{"uuid", "/users/550e8400-e29b-41d4-a716-446655440000/profile", "users_profile", "UUID"},
		{"ulid", "/sessions/01ARZ3NDEKTSV4RRFFQ69G5FAV", "sessions", "ULID"},
		{"mongoid", "/obj/507f1f77bcf86cd799439011", "obj", "MongoID"},
		{"timestamp", "/events/1699999999999", "events", "Timestamp (ms)"},
		{"long numeric", "/tenants/12345678/billing", "tenants_billing", "Long numeric ID"},
		{"resource digits", "/users/user123/profile", "users_profile", "Resource+digits dropped"},
		{"multiple resource ids", "/orders/order_456/items/item789", "orders_items", "Multiple resource IDs dropped"},
		{"hexy id", "/keys/ab12cd34ef56ab78", "keys", "Hexy-ish ID"},

		// Meaningful numerics (keep)
		{"status200", "/status200/check", "status200_check", "Keeplist token"},
		{"http2", "/http2/support", "http2_support", "Keeplist token"},

		// "me/self/current" aren't IDs
		{"users me", "/users/me/profile", "users_me_profile", "Keep me"},
		{"accounts self", "/accounts/self/limits", "accounts_self_limits", "Keep self"},
		{"profiles current", "/profiles/current", "profiles_current", "Keep current"},

		// Locales & Formats
		{"content en", "/content/en/articles", "content_en_articles", "Keep locale"},
		{"content en-US", "/content/en-US/articles", "content_en-us_articles", "Lowercase normalized"},
		{"export csv", "/export/csv", "export_csv", "Keep format token"},
		{"feed json", "/feed/recent.json", "feed_recent", "Trim file extension on last token"},

		// Well-known Paths
		{"well-known openid", "/.well-known/openid-configuration", ".well-known_openid-configuration", "Must keep both"},
		{"well-known jwks", "/.well-known/jwks.json", ".well-known_jwks", "Trim extension"},

		// Deep Paths → head(2) + tail(1)
		{"deep billing path", "/billing/tenant123/invoices/invoice456/items/item789", "billing_invoices_items", "Long path shaping"},
		{"deep orders path", "/api/v2/orders/order456/items/item789", "orders_items", "Long path shaping"},
		{"generic deep path", "/a/b/c/d/e", "a_b_e", "Generic deep path"},

		// Matrix params / path params / oddities
		{"matrix param", "/app;jsessionid=ABC123/home", "app", "Drop matrix param"},
		{"matrix param with ext", "/reports;region=us/2024/summary.pdf", "reports", "Drop matrix + trim ext"},
		{"query param", "/users/123?expand=roles", "users_123", "Drop query"},

		// Enhanced special cases
		{"well-known openid", "/.well-known/openid-configuration", ".well-known_openid-configuration", "Keep well-known paths"},
		{"jwks endpoint", "/.well-known/jwks.json", ".well-known_jwks", "Keep jwks with extension trimming"},
		{"oauth2 endpoint", "/oauth2/authorize", "oauth2_authorize", "Keep oauth2"},
		{"healthz endpoint", "/healthz", "healthz", "Keep healthz"},
		{"readyz endpoint", "/readyz", "readyz", "Keep readyz"},
		{"livez endpoint", "/livez", "livez", "Keep livez"},
		{"metrics endpoint", "/metrics", "metrics", "Keep metrics"},
		{"bulk endpoint", "/api/v1/bulk/upload", "bulk_upload", "Keep bulk"},
		{"export endpoint", "/export/csv", "export_csv", "Keep export"},
		{"search endpoint", "/search/users", "search_users", "Keep search"},

		// Locales and formats
		{"content en-US", "/content/en-US/articles", "content_en-us_articles", "Keep locale en-US"},
		{"content es", "/content/es/news", "content_es_news", "Keep locale es"},
		{"json format", "/api/v1/data.json", "data", "Keep json format"},
		{"csv format", "/reports/export.csv", "reports_export", "Keep csv format"},
		{"ndjson format", "/logs/events.ndjson", "logs_events.ndjson", "Keep ndjson format"},
		{"xml format", "/config/settings.xml", "config_settings", "Keep xml format"},

		// Special cases: me/self/current
		{"users me", "/users/me/profile", "users_me_profile", "Keep me"},
		{"accounts self", "/accounts/self/settings", "accounts_self_settings", "Keep self"},
		{"profiles current", "/profiles/current", "profiles_current", "Keep current"},

		// Template variables (should be stripped out)
		{"template variable", "/data/{{.previousStep.value}}/view", "data_view", "Strip out template variables"},
		{"template variable simple", "/data/{{.value}}/validate", "data_validate", "Strip out simple template variables"},
		{"template variable in path", "/auth/view/{{.tenant_id}}/remove", "auth_view_remove", "Strip out template variables in middle of path"},
		{"template variable with dots", "/config/{{.previousStep.value.vaule2}}/create", "config_create", "Strip out nested template variables"},

		// Advanced version patterns
		{"date version", "/api/v2024-10-01/users", "users", "Drop date-style version"},
		{"api-v2 version", "/api-v2/payments", "payments", "Drop api-v2 version"},
		{"v2alpha1 version", "/api/v2alpha1/config", "config", "Drop v2alpha1 version"},

		// File extensions with dates
		{"reports with date", "/reports/2024-10-01/summary.pdf", "reports_summary", "Drop date and trim extension"},

		// Edge cases
		{"empty path", "", "root", "Empty path"},
		{"single slash", "/", "root", "Single slash"},
		{"multiple slashes", "///", "root", "Multiple slashes"},
		{"mixed case", "/API/V1/Users", "users", "Mixed case normalized"},
		{"special chars", "/path-with-dashes/and_underscores", "path-with-dashes_and_underscores", "Special chars preserved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSimpleEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("ExtractSimpleEndpoint(%q) = %q, want %q (%s)", tt.path, result, tt.expected, tt.notes)
			}
		})
	}
}

func TestExtractSimpleEndpointEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		// Very long paths (should be truncated)
		{"very long path", "/this/is/a/very/long/path/that/would/produce/a/name/longer/than/eighty/characters/once/normalized", "this_is_normalized"},

		// All numeric paths
		{"all numeric", "/123/456/789", "123_456_789"},

		// All special chars
		{"all special", "/!@#$%^&*()", "!@"},

		// Mixed ID patterns
		{"mixed ids", "/users/123/orders/456/items/789", "users_123_789"},

		// Version variations
		{"version beta", "/api/v1beta/users", "users"},
		{"version alpha", "/api/v2alpha1/users", "users"},
		{"version rc", "/api/v1.0-rc1/users", "users"},

		// Complex resource patterns
		{"complex resource", "/api/v1/users/user-123-abc/profile", "users_profile"},
		{"complex resource 2", "/api/v1/orders/order_456_def/items/item-789-ghi", "orders_items"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSimpleEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("ExtractSimpleEndpoint(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestLooksLikeID(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected bool
	}{
		{"short token", "abc", false},
		{"exactly 6 chars", "abc123", true}, // digit_ratio = 0.5 >= 0.4
		{"mixed long", "user123abc", false}, // digit_ratio = 0.33, digitRuns = 1, should not be ID
		{"mostly letters", "userabc", false},
		{"mostly digits", "123456", true},
		{"alternating", "a1b2c3", true},     // 2 digit runs
		{"status code", "status200", false}, // should be in keep list
		{"http2", "http2", false},           // should be in keep list
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeID(tt.token)
			if result != tt.expected {
				t.Errorf("looksLikeID(%q) = %v, want %v", tt.token, result, tt.expected)
			}
		})
	}
}

func TestTrimExtIfAny(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no extension", "filename", "filename"},
		{"json extension", "data.json", "data"},
		{"csv extension", "export.csv", "export"},
		{"pdf extension", "report.pdf", "report"},
		{"xml extension", "config.xml", "config"},
		{"long extension", "file.verylongext", "file.verylongext"}, // > 6 chars
		{"dot at start", ".hidden", ".hidden"},
		{"multiple dots", "file.backup.json", "file.backup"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimExtIfAny(tt.input)
			if result != tt.expected {
				t.Errorf("trimExtIfAny(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeTemplateVariable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple template var", "{{.tenant_locator}}", ""},
		{"nested template var", "{{.setup.tenant_locator}}", ""},
		{"deep nested template var", "{{.setup.tenant.locator}}", ""},
		{"not a template var", "regular_token", "regular_token"},
		{"empty template var", "{{.}}", ""},
		{"malformed template var", "{{.tenant_locator", "{{.tenant_locator"},
		{"multiple dots", "{{.a.b.c.d}}", ""},
		{"single dot", "{{.simple}}", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeTemplateVariable(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeTemplateVariable(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractSimpleEndpointWithGraphQL(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		contentType string
		body        []byte
		expected    string
	}{
		{"graphql endpoint", "/api/graphql", "application/json", nil, "graphql"},
		{"graphql with gql", "/gql", "application/json", nil, "graphql"},
		{"graphql with operation", "/graphql", "application/json", []byte(`{"operationName":"ListUsers"}`), "graphql"},
		{"graphql without operation", "/graphql", "application/json", []byte(`{"query":"{users{id}}"}`), "graphql"},
		{"non-graphql endpoint", "/api/users", "application/json", nil, "users"},
		{"graphql with complex body", "/graphql", "application/json", []byte(`{"operationName":"GetUserProfile","variables":{"id":123}}`), "graphql"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSimpleEndpointWithGraphQL(tt.path, tt.contentType, tt.body)
			if result != tt.expected {
				t.Errorf("ExtractSimpleEndpointWithGraphQL(%q, %q, %q) = %q, want %q", tt.path, tt.contentType, string(tt.body), result, tt.expected)
			}
		})
	}
}

func TestExtractSimpleEndpointWithMethod(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		method   string
		expected string
		notes    string
	}{
		// Basic method prefixes
		{"GET root", "/", "GET", "GET_root", "GET method with root path"},
		{"POST users", "/api/v1/users", "POST", "POST_users", "POST method with API path"},
		{"PUT profile", "/users/123/profile", "PUT", "PUT_users_123_profile", "PUT method with user path (ID kept in stateless mode)"},
		{"DELETE item", "/orders/456/items/789", "DELETE", "DELETE_orders_456_789", "DELETE method with deep path (IDs kept in stateless mode)"},
		{"PATCH settings", "/users/me/settings", "PATCH", "PATCH_users_me_settings", "PATCH method with me path"},
		{"HEAD health", "/health", "HEAD", "HEAD_health", "HEAD method with health check"},
		{"OPTIONS cors", "/api/cors", "OPTIONS", "OPTIONS_cors", "OPTIONS method with API path"},

		// Method normalization
		{"lowercase get", "/users", "get", "GET_users", "Lowercase method normalized to uppercase"},
		{"mixed case post", "/posts", "PoSt", "POST_posts", "Mixed case method normalized"},
		{"empty method", "/data", "", "GET_data", "Empty method defaults to GET"},

		// Complex paths with method prefixes
		{"GET deep path", "/api/v2/orders/order123/items/item456", "GET", "GET_orders_items", "GET with deep path normalization"},
		{"POST graphql", "/graphql", "POST", "POST_graphql", "POST to GraphQL endpoint"},
		{"PUT file upload", "/upload/files/file123.pdf", "PUT", "PUT_upload_files_file123", "PUT with file extension trimming (ID kept in stateless mode)"},
		{"DELETE with query", "/users/123?force=true", "DELETE", "DELETE_users_123", "DELETE with query params (dropped, but ID kept in stateless mode)"},

		// Special endpoints
		{"GET well-known", "/.well-known/openid-configuration", "GET", "GET_.well-known_openid-configuration", "GET with well-known path"},
		{"POST metrics", "/metrics", "POST", "POST_metrics", "POST to metrics endpoint"},
		{"PUT healthz", "/healthz", "PUT", "PUT_healthz", "PUT to healthz endpoint"},

		// Template variables with method prefixes (stripped out)
		{"PATCH template var", "/policy/{{.setup.tenant_locator}}/holds", "PATCH", "PATCH_policy_holds", "PATCH with template variable stripped"},
		{"PATCH simple template", "/policy/{{.tenant_locator}}/validate", "PATCH", "PATCH_policy_validate", "PATCH with simple template variable stripped"},
		{"PATCH nested template", "/config/{{.setup.tenant.locator}}/create", "PATCH", "PATCH_config_create", "PATCH with nested template variable stripped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSimpleEndpointWithMethod(tt.path, tt.method)
			if result != tt.expected {
				t.Errorf("ExtractSimpleEndpointWithMethod(%q, %q) = %q, want %q (%s)", tt.path, tt.method, result, tt.expected, tt.notes)
			}
		})
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		method      string
		contentType string
		body        []byte
		expected    string
		notes       string
	}{
		// GraphQL endpoints with methods
		{"POST graphql", "/api/graphql", "POST", "application/json", nil, "POST_graphql", "POST to GraphQL endpoint"},
		{"GET graphql", "/gql", "GET", "application/json", nil, "GET_graphql", "GET to GraphQL endpoint"},
		{"POST graphql with operation", "/graphql", "POST", "application/json", []byte(`{"operationName":"ListUsers"}`), "POST_graphql", "POST with GraphQL operation"},
		{"PUT graphql without operation", "/graphql", "PUT", "application/json", []byte(`{"query":"{users{id}}"}`), "PUT_graphql", "PUT without operation name"},

		// Non-GraphQL endpoints with methods
		{"GET users", "/api/users", "GET", "application/json", nil, "GET_users", "GET to regular API endpoint"},
		{"POST users", "/api/users", "POST", "application/json", nil, "POST_users", "POST to regular API endpoint"},
		{"PUT profile", "/users/123/profile", "PUT", "application/json", nil, "PUT_users_123_profile", "PUT to user profile"},

		// Method normalization with GraphQL
		{"lowercase post graphql", "/graphql", "post", "application/json", nil, "POST_graphql", "Lowercase method normalized"},
		{"mixed case get graphql", "/gql", "GeT", "application/json", nil, "GET_graphql", "Mixed case method normalized"},
		{"empty method graphql", "/graphql", "", "application/json", nil, "GET_graphql", "Empty method defaults to GET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeEndpoint(tt.path, tt.method, tt.contentType, tt.body)
			if result != tt.expected {
				t.Errorf("NormalizeEndpoint(%q, %q, %q, %q) = %q, want %q (%s)", tt.path, tt.method, tt.contentType, string(tt.body), result, tt.expected, tt.notes)
			}
		})
	}
}

func TestCollisionHandling(t *testing.T) {
	// Test collision detection with paths that should normalize to the same endpoint
	path1 := "/api/v1/users/abc123/profile"
	path2 := "/api/v1/users/def456/profile"

	result1 := ExtractSimpleEndpoint(path1)
	result2 := ExtractSimpleEndpoint(path2)

	// Both should normalize to the same base endpoint
	if !strings.HasPrefix(result1, "users_profile") || !strings.HasPrefix(result2, "users_profile") {
		t.Errorf("Expected both to start with 'users_profile', got: %q and %q", result1, result2)
	}

	// With stateless implementation, they should be the same since each call creates a new state
	if result1 != result2 {
		t.Errorf("Expected same results with stateless implementation, got different: %q and %q", result1, result2)
	}

	// Both should NOT have hash suffixes in stateless mode
	if strings.Contains(result1, "_") && len(strings.Split(result1, "_")) > 2 {
		t.Errorf("Expected no hash suffixes in stateless mode, got: %q", result1)
	}
}

func TestCardinalityLimit(t *testing.T) {
	// Test cardinality limit with a reasonable number of endpoints
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/test/endpoint/%d", i)
		result := ExtractSimpleEndpoint(path)

		// Just verify the function works without hitting limits
		if result == "" {
			t.Errorf("Expected non-empty result for path %s, got: %q", path, result)
		}
	}
}

// Benchmark tests for performance
func BenchmarkExtractSimpleEndpoint(b *testing.B) {
	paths := []string{
		"/api/v1/users/user123/profile",
		"/billing/tenant456/invoices/invoice789/items/item123",
		"/.well-known/openid-configuration",
		"/users/me/settings",
		"/export/csv",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			ExtractSimpleEndpoint(path)
		}
	}
}

func BenchmarkLooksLikeID(b *testing.B) {
	tokens := []string{
		"user123",
		"abc123def",
		"status200",
		"verylongtoken123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, token := range tokens {
			looksLikeID(token)
		}
	}
}
