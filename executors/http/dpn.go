package http

import (
	"crypto/md5"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Precompiled regex patterns for better performance
var (
	reVersion     = regexp.MustCompile(`^(api-)?v\d+([a-z0-9]+)?$`) // v1, v2beta, api-v2
	reDateVersion = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)       // 2024-10-01
	reUUID        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	reULID        = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	reKSUID       = regexp.MustCompile(`^[0-9A-Za-z]{27}$`)
	reMongoID     = regexp.MustCompile(`^[0-9a-f]{24}$`)
	rePureDigits  = regexp.MustCompile(`^\d{6,}$`)
	reTimestamp   = regexp.MustCompile(`^\d{10}(\d{3})?$`)
	reHexyBlob    = regexp.MustCompile(`^[0-9a-f]{12,}$`)
	reResourceKey = regexp.MustCompile(`^[A-Za-z]+[-_]*\d+([A-Za-z0-9-_]+)?$`)
	reLocale      = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`) // en, es, en-US
	reTemplateVar = regexp.MustCompile(`^{{\.([^}]*)}}$`)        // {{.variable}} or {{.perviousStep.value}} or {{.}}
)

// Keep list for semantically significant tokens
var keepList = []*regexp.Regexp{
	regexp.MustCompile(`^status\d{3}$`),
	regexp.MustCompile(`^http2$`),
	regexp.MustCompile(`^ipv6$`),
	regexp.MustCompile(`^(\.well-known|openid-configuration|oauth2|healthz|readyz|livez|metrics|search|bulk|export|jwks)$`),
	regexp.MustCompile(`^(json|ndjson|csv|xml)$`),
	reLocale,
}

// API prefixes that can be dropped anywhere (but prefer early segments)
var apiPrefixes = map[string]bool{
	"api":     true,
	"rest":    true,
	"graphql": true,
}

// DPNConfig holds configuration for the DPN
type DPNConfig struct {
	MaxEndpoints int
	CacheSize    int
}

// DefaultDPNConfig returns the default DPN configuration
func DefaultDPNConfig() *DPNConfig {
	return &DPNConfig{
		MaxEndpoints: getMaxEndpoints(),
		CacheSize:    8192,
	}
}

// DPNState holds the state for a single DPN instance
type DPNState struct {
	mu                 sync.RWMutex
	cache              map[string]string
	endpointCollisions map[string]string
	endpointCount      int
	endpointsBucketed  int
	config             *DPNConfig
}

// NewDPNState creates a new DPN state instance
func NewDPNState(config *DPNConfig) *DPNState {
	if config == nil {
		config = DefaultDPNConfig()
	}
	return &DPNState{
		cache:              make(map[string]string),
		endpointCollisions: make(map[string]string),
		config:             config,
	}
}

// getMaxEndpoints returns the maximum number of unique endpoints allowed
// Can be configured via VENOM_MAX_ENDPOINTS environment variable
func getMaxEndpoints() int {
	if envVal := os.Getenv("VENOM_MAX_ENDPOINTS"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			return val
		}
	}
	return 5000 // Default: 5000 endpoints
}

// ExtractSimpleEndpoint transforms URLs into stable endpoint templates
func ExtractSimpleEndpoint(path string) string {
	// Create a temporary state for this call
	state := NewDPNState(nil)
	return ExtractSimpleEndpointWithState(path, state)
}

// ExtractSimpleEndpointWithMethod includes HTTP method as prefix
func ExtractSimpleEndpointWithMethod(path string, method string) string {
	// Create a temporary state for this call
	state := NewDPNState(nil)
	return ExtractSimpleEndpointWithMethodAndState(path, method, state)
}

// ExtractSimpleEndpointWithMethodAndState includes HTTP method prefix with state management
func ExtractSimpleEndpointWithMethodAndState(path string, method string, state *DPNState) string {
	// Normalize method to uppercase
	method = strings.ToUpper(method)
	if method == "" {
		method = "GET"
	}

	// Extract the endpoint without method
	endpoint := ExtractSimpleEndpointWithState(path, state)

	// Add method prefix
	return method + "_" + endpoint
}

// ExtractSimpleEndpointWithState normalizes path with state management
func ExtractSimpleEndpointWithState(path string, state *DPNState) string {
	// Check cache first
	state.mu.RLock()
	if cached, exists := state.cache[path]; exists {
		state.mu.RUnlock()
		return cached
	}
	state.mu.RUnlock()

	path = strings.ToLower(path)

	// Strip query parameters, fragments, and matrix parameters
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	if idx := strings.Index(path, "#"); idx != -1 {
		path = path[:idx]
	}
	if idx := strings.Index(path, ";"); idx != -1 {
		path = path[:idx]
	}

	path = strings.TrimSuffix(path, "/")
	if path == "" || path == "/" {
		return "root"
	}

	// Tokenize path
	parts := strings.Split(path, "/")
	tokens := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tokens = append(tokens, part)
		}
	}

	if len(tokens) == 0 {
		return "root"
	}

	// Classify and prune tokens
	keptTokens := []string{}

	for i, token := range tokens {
		token = normalizeTemplateVariable(token)
		if token == "" {
			continue
		}

		// Check keep list first
		for _, keepPattern := range keepList {
			if keepPattern.MatchString(token) {
				keptTokens = append(keptTokens, token)
				goto nextToken
			}
		}

		// Keep special tokens
		if token == "me" || token == "self" || token == "current" {
			keptTokens = append(keptTokens, token)
			continue
		}

		// Drop API prefixes in early positions
		if apiPrefixes[token] && i <= 2 {
			continue
		}

		// Drop version tokens
		if reVersion.MatchString(token) || reDateVersion.MatchString(token) {
			continue
		}

		// Drop HTTP method tokens
		if isHTTPMethod(token) {
			continue
		}

		// Drop ID-like tokens
		if isIDLike(token) {
			continue
		}

		// Drop tokens that look like IDs
		if len(token) >= 6 && looksLikeID(token) {
			continue
		}

		keptTokens = append(keptTokens, token)

	nextToken:
	}

	// Shape template
	var result string
	if len(keptTokens) <= 3 {
		result = strings.Join(keptTokens, "_")
	} else {
		if len(keptTokens) >= 3 {
			result = keptTokens[0] + "_" + keptTokens[1] + "_" + keptTokens[len(keptTokens)-1]
		} else {
			result = strings.Join(keptTokens, "_")
		}
	}

	if len(keptTokens) > 0 {
		result = trimExtIfAny(result)
	}

	result = regexp.MustCompile(`_+`).ReplaceAllString(result, "_")
	result = strings.Trim(result, "_")

	if len(result) > 80 {
		result = result[:80]
	}

	if result == "" {
		result = strings.ReplaceAll(path, "/", "_")
		result = strings.Trim(result, "_")
	}

	result = handleCollisionsAndCardinalityWithState(result, path, state)

	// Cache result
	state.mu.Lock()
	if len(state.cache) >= state.config.CacheSize {
		state.cache = make(map[string]string)
	}
	state.cache[path] = result
	state.mu.Unlock()

	return result
}

// isHTTPMethod checks if token is an HTTP method
func isHTTPMethod(token string) bool {
	return token == "get" || token == "post" || token == "put" ||
		token == "patch" || token == "delete" || token == "head" || token == "options"
}

// isIDLike checks if token matches known ID patterns
func isIDLike(token string) bool {
	return reUUID.MatchString(token) || reULID.MatchString(token) || reKSUID.MatchString(token) ||
		reMongoID.MatchString(token) || rePureDigits.MatchString(token) || reTimestamp.MatchString(token) ||
		reHexyBlob.MatchString(token) || reResourceKey.MatchString(token)
}

// looksLikeID determines if a token looks like an ID based on digit ratio and patterns
func looksLikeID(token string) bool {
	if len(token) < 6 {
		return false
	}

	// Count digits
	digitCount := 0
	digitRuns := 0
	inDigitRun := false

	for _, char := range token {
		if char >= '0' && char <= '9' {
			digitCount++
			if !inDigitRun {
				digitRuns++
				inDigitRun = true
			}
		} else {
			inDigitRun = false
		}
	}

	digitRatio := float64(digitCount) / float64(len(token))
	return digitRatio >= 0.4 || digitRuns >= 2
}

// trimExtIfAny removes file extensions
func trimExtIfAny(s string) string {
	if i := strings.LastIndexByte(s, '.'); i > 0 && i >= len(s)-6 {
		return s[:i]
	}
	return s
}

// normalizeTemplateVariable strips out template variables
func normalizeTemplateVariable(token string) string {
	if reTemplateVar.MatchString(token) {
		return ""
	}
	return token
}

// handleCollisionsAndCardinalityWithState handles endpoint collisions and cardinality limits
func handleCollisionsAndCardinalityWithState(normalized, original string, state *DPNState) string {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.endpointCount >= state.config.MaxEndpoints {
		state.endpointsBucketed++
		return "other"
	}

	if existingOriginal, exists := state.endpointCollisions[normalized]; exists && existingOriginal != original {
		hash := fmt.Sprintf("%x", md5.Sum([]byte(original)))[:8]
		normalizedWithHash := normalized + "_" + hash

		state.endpointCollisions[normalizedWithHash] = original
		state.endpointCount++

		return normalizedWithHash
	}

	state.endpointCollisions[normalized] = original
	state.endpointCount++

	return normalized
}

// ExtractSimpleEndpointWithGraphQL implements DPN with GraphQL operation detection
func ExtractSimpleEndpointWithGraphQL(path string, contentType string, body []byte) string {
	if strings.HasSuffix(path, "/graphql") || strings.HasSuffix(path, "/gql") {
		if contentType == "application/json" && len(body) > 0 {
			if operationName := extractGraphQLOperation(body); operationName != "" {
				return "graphql"
			}
		}
		return "graphql"
	}

	return ExtractSimpleEndpoint(path)
}

// NormalizeEndpoint implements DPN with GraphQL operation detection and HTTP method prefix
func NormalizeEndpoint(path string, method string, contentType string, body []byte) string {
	if strings.HasSuffix(path, "/graphql") || strings.HasSuffix(path, "/gql") {
		method = strings.ToUpper(method)
		if method == "" {
			method = "GET"
		}

		if contentType == "application/json" && len(body) > 0 {
			if operationName := extractGraphQLOperation(body); operationName != "" {
				return method + "_graphql"
			}
		}
		return method + "_graphql"
	}

	return ExtractSimpleEndpointWithMethod(path, method)
}

// extractGraphQLOperation extracts operationName from GraphQL request body
func extractGraphQLOperation(body []byte) string {
	bodyStr := string(body)
	opNamePattern := regexp.MustCompile(`"operationName"\s*:\s*"([^"]+)"`)
	if matches := opNamePattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// GetCardinalityStats returns statistics about endpoint cardinality (stateless)
func GetCardinalityStats() map[string]interface{} {
	state := NewDPNState(nil)
	return GetCardinalityStatsWithState(state)
}

// GetCardinalityStatsWithState returns statistics about endpoint cardinality
func GetCardinalityStatsWithState(state *DPNState) map[string]interface{} {
	state.mu.RLock()
	defer state.mu.RUnlock()

	return map[string]interface{}{
		"unique_endpoints":   state.endpointCount,
		"max_endpoints":      state.config.MaxEndpoints,
		"endpoints_bucketed": state.endpointsBucketed,
		"cardinality_ratio":  float64(state.endpointCount) / float64(state.config.MaxEndpoints),
		"cache_size":         len(state.cache),
		"collision_map_size": len(state.endpointCollisions),
	}
}
