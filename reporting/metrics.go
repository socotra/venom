package reporting

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Logger interface for logging functionality
type Logger interface {
	Debug(ctx context.Context, format string, args ...interface{})
}

// Global logger instance - will be set by the main venom package
var globalLogger Logger

// SetLogger sets the global logger for the reporting package
func SetLogger(logger Logger) {
	globalLogger = logger
}

type MetricsCollector interface {
	RecordHTTPRequest(duration time.Duration, statusCode int, err error)
	RecordHTTPRequestWithEndpoint(duration time.Duration, statusCode int, method, endpoint string, err error)
	RecordTestStructure(groups map[string]*TestGroup, setupData map[string]string)
	GetMetrics() *Metrics
	Reset()
}

type Metrics struct {
	RootGroup *TestGroup         `json:"root_group"`
	Metrics   map[string]*Metric `json:"metrics"`
	SetupData map[string]string  `json:"setup_data,omitempty"`
	StartTime time.Time          `json:"start_time,omitempty"`
	EndTime   time.Time          `json:"end_time,omitempty"`
}

type TestGroup struct {
	Name   string                `json:"name"`
	Path   string                `json:"path"`
	ID     string                `json:"id"`
	Groups map[string]*TestGroup `json:"groups"`
	Checks map[string]*TestCheck `json:"checks"`
}

type TestCheck struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	ID     string `json:"id"`
	Passes int64  `json:"passes"`
	Fails  int64  `json:"fails"`
}

type Metric struct {
	Type   string                 `json:"type"`
	Values map[string]interface{} `json:"values"`
}

type MetricsConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Format  string `json:"format" yaml:"format"`
	Output  string `json:"output" yaml:"output"`
}

func DefaultMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		Enabled: false,
		Format:  "json",
		Output:  "",
	}
}

type metricsCollector struct {
	mu sync.RWMutex

	httpRequests    []time.Duration
	httpStatusCodes map[int]int64
	httpErrors      int64
	httpTotal       int64

	httpRequestsByEndpoint    map[string][]time.Duration
	httpStatusCodesByEndpoint map[string]map[int]int64
	httpErrorsByEndpoint      map[string]int64
	httpTotalByEndpoint       map[string]int64

	testGroups map[string]*TestGroup
	setupData  map[string]string

	startTime time.Time
	endTime   time.Time
}

func NewMetricsCollector() MetricsCollector {
	return &metricsCollector{
		httpStatusCodes:           make(map[int]int64),
		httpRequestsByEndpoint:    make(map[string][]time.Duration),
		httpStatusCodesByEndpoint: make(map[string]map[int]int64),
		httpErrorsByEndpoint:      make(map[string]int64),
		httpTotalByEndpoint:       make(map[string]int64),
		testGroups:                make(map[string]*TestGroup),
		setupData:                 make(map[string]string),
		startTime:                 time.Now(),
	}
}

func (mc *metricsCollector) RecordHTTPRequest(duration time.Duration, statusCode int, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.httpRequests = append(mc.httpRequests, duration)
	mc.httpTotal++

	// Consider both network errors and HTTP error status codes (4xx, 5xx) as failures
	isError := err != nil || statusCode >= 400

	if isError {
		mc.httpErrors++
	} else {
		mc.httpStatusCodes[statusCode]++
	}
}

func (mc *metricsCollector) RecordHTTPRequestWithEndpoint(duration time.Duration, statusCode int, method, endpoint string, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	endpointKey := fmt.Sprintf("%s %s", method, endpoint)

	mc.httpRequests = append(mc.httpRequests, duration)
	mc.httpTotal++

	// Consider both network errors and HTTP error status codes (4xx, 5xx) as failures
	isError := err != nil || statusCode >= 400

	if isError {
		mc.httpErrors++
	} else {
		mc.httpStatusCodes[statusCode]++
	}

	if mc.httpRequestsByEndpoint[endpointKey] == nil {
		mc.httpRequestsByEndpoint[endpointKey] = make([]time.Duration, 0)
		mc.httpStatusCodesByEndpoint[endpointKey] = make(map[int]int64)
		mc.httpTotalByEndpoint[endpointKey] = 0
		mc.httpErrorsByEndpoint[endpointKey] = 0
	}

	mc.httpRequestsByEndpoint[endpointKey] = append(mc.httpRequestsByEndpoint[endpointKey], duration)
	mc.httpTotalByEndpoint[endpointKey]++

	if isError {
		mc.httpErrorsByEndpoint[endpointKey]++
	} else {
		mc.httpStatusCodesByEndpoint[endpointKey][statusCode]++
	}

	// Always record status codes for tracking, regardless of whether they're errors
	mc.httpStatusCodesByEndpoint[endpointKey][statusCode]++
}

func (mc *metricsCollector) GetMetrics() *Metrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	mc.endTime = time.Now()

	metrics := &Metrics{
		Metrics:   make(map[string]*Metric),
		StartTime: mc.startTime,
		EndTime:   mc.endTime,
		RootGroup: &TestGroup{
			Name:   "",
			Path:   "",
			ID:     "d41d8cd98f00b204e9800998ecf8427e",
			Groups: mc.testGroups,
			Checks: make(map[string]*TestCheck),
		},
		SetupData: mc.setupData,
	}

	// HTTP metrics
	if mc.httpTotal > 0 {
		httpReqDuration := mc.calculateDurationMetrics(mc.httpRequests)
		httpReqDuration.Values["count"] = mc.httpTotal
		httpReqDuration.Values["rate"] = mc.calculateRate(mc.httpTotal, mc.startTime, mc.endTime)

		metrics.Metrics["http_req_duration"] = httpReqDuration
		metrics.Metrics["http_reqs"] = &Metric{
			Type: "counter",
			Values: map[string]interface{}{
				"count": mc.httpTotal,
				"rate":  mc.calculateRate(mc.httpTotal, mc.startTime, mc.endTime),
			},
		}

		// Status code distribution
		for statusCode, count := range mc.httpStatusCodes {
			metricName := fmt.Sprintf("http_req_status_%d", statusCode)
			metrics.Metrics[metricName] = &Metric{
				Type: "counter",
				Values: map[string]interface{}{
					"count": count,
				},
			}
		}

		// Error rate
		if mc.httpErrors > 0 {
			errorRate := float64(mc.httpErrors) / float64(mc.httpTotal) * 100
			metrics.Metrics["http_req_failed"] = &Metric{
				Type: "rate",
				Values: map[string]interface{}{
					"passes": 0,
					"fails":  mc.httpErrors,
					"thresholds": map[string]interface{}{
						"rate<0.01": false,
					},
					"value": errorRate,
				},
			}
		} else {
			// No errors
			metrics.Metrics["http_req_failed"] = &Metric{
				Type: "rate",
				Values: map[string]interface{}{
					"passes": mc.httpTotal,
					"fails":  0,
					"thresholds": map[string]interface{}{
						"rate<0.01": true,
					},
					"value": 0,
				},
			}
		}

		metrics.Metrics["iterations"] = &Metric{
			Type: "counter",
			Values: map[string]interface{}{
				"count": mc.httpTotal,
				"rate":  mc.calculateRate(mc.httpTotal, mc.startTime, mc.endTime),
			},
		}

		metrics.Metrics["checks"] = &Metric{
			Type: "rate",
			Values: map[string]interface{}{
				"passes": mc.httpTotal - mc.httpErrors,
				"fails":  mc.httpErrors,
				"value":  float64(mc.httpTotal-mc.httpErrors) / float64(mc.httpTotal),
			},
		}

		estimatedDataSent := mc.httpTotal * 1024
		estimatedDataReceived := mc.httpTotal * 2048

		metrics.Metrics["data_sent"] = &Metric{
			Type: "counter",
			Values: map[string]interface{}{
				"count": estimatedDataSent,
				"rate":  mc.calculateRate(estimatedDataSent, mc.startTime, mc.endTime),
			},
		}

		metrics.Metrics["data_received"] = &Metric{
			Type: "counter",
			Values: map[string]interface{}{
				"count": estimatedDataReceived,
				"rate":  mc.calculateRate(estimatedDataReceived, mc.startTime, mc.endTime),
			},
		}

		metrics.Metrics["vus"] = &Metric{
			Type: "gauge",
			Values: map[string]interface{}{
				"value": 1,
				"min":   1,
				"max":   1,
			},
		}

		metrics.Metrics["vus_max"] = &Metric{
			Type: "gauge",
			Values: map[string]interface{}{
				"value": 1,
				"min":   1,
				"max":   1,
			},
		}

		// Per-endpoint HTTP metrics
		for endpointKey, requests := range mc.httpRequestsByEndpoint {
			if len(requests) > 0 {
				endpointDuration := mc.calculateDurationMetrics(requests)
				endpointDuration.Values["count"] = mc.httpTotalByEndpoint[endpointKey]
				endpointDuration.Values["rate"] = mc.calculateRate(mc.httpTotalByEndpoint[endpointKey], mc.startTime, mc.endTime)

				endpointName := endpointKey
				if idx := strings.Index(endpointKey, " "); idx != -1 {
					endpointName = endpointKey[idx+1:]
				}

				metricName := endpointName
				metrics.Metrics[metricName] = endpointDuration

				checkID := generateID(fmt.Sprintf("::%s", endpointName))
				metrics.RootGroup.Checks[endpointName] = &TestCheck{
					Name:   endpointName,
					Path:   fmt.Sprintf("::%s", endpointName),
					ID:     checkID,
					Passes: mc.httpTotalByEndpoint[endpointKey] - mc.httpErrorsByEndpoint[endpointKey],
					Fails:  mc.httpErrorsByEndpoint[endpointKey],
				}

				// Per-endpoint status codes
				if statusCodes, exists := mc.httpStatusCodesByEndpoint[endpointKey]; exists {
					for statusCode, count := range statusCodes {
						statusMetricName := fmt.Sprintf("http_req_status_%s_%d", endpointName, statusCode)
						metrics.Metrics[statusMetricName] = &Metric{
							Type: "counter",
							Values: map[string]interface{}{
								"count": count,
							},
						}
					}
				}

				// Per-endpoint error rate
				if mc.httpErrorsByEndpoint[endpointKey] > 0 {
					endpointErrorRate := float64(mc.httpErrorsByEndpoint[endpointKey]) / float64(mc.httpTotalByEndpoint[endpointKey]) * 100
					errorMetricName := fmt.Sprintf("http_req_failed_%s", endpointName)
					metrics.Metrics[errorMetricName] = &Metric{
						Type: "rate",
						Values: map[string]interface{}{
							"value": endpointErrorRate,
						},
					}
				}
			}
		}
	}

	return metrics
}

func (mc *metricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.httpRequests = nil
	mc.httpStatusCodes = make(map[int]int64)
	mc.httpErrors = 0
	mc.httpTotal = 0

	mc.httpRequestsByEndpoint = make(map[string][]time.Duration)
	mc.httpStatusCodesByEndpoint = make(map[string]map[int]int64)
	mc.httpErrorsByEndpoint = make(map[string]int64)
	mc.httpTotalByEndpoint = make(map[string]int64)

	mc.startTime = time.Now()
	mc.endTime = time.Time{}
}

func (mc *metricsCollector) calculateDurationMetrics(durations []time.Duration) *Metric {
	if len(durations) == 0 {
		return &Metric{
			Type:   "trend",
			Values: make(map[string]interface{}),
		}
	}

	values := make([]float64, len(durations))
	for i, d := range durations {
		values[i] = float64(d.Milliseconds())
	}

	sort.Float64s(values)

	metric := &Metric{
		Type: "trend",
		Values: map[string]interface{}{
			"min": values[0],
			"max": values[len(values)-1],
			"avg": mc.calculateAverage(values),
		},
	}

	if len(values) > 0 {
		metric.Values["p(50)"] = mc.calculatePercentile(values, 50)
		metric.Values["p(90)"] = mc.calculatePercentile(values, 90)
		metric.Values["p(95)"] = mc.calculatePercentile(values, 95)
		metric.Values["p(99)"] = mc.calculatePercentile(values, 99)
	}

	return metric
}

func (mc *metricsCollector) calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (mc *metricsCollector) calculatePercentile(values []float64, percentile int) float64 {
	if len(values) == 0 {
		return 0
	}

	index := float64(percentile) / 100.0 * float64(len(values)-1)
	if index == float64(int(index)) {
		return values[int(index)]
	}

	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	weight := index - float64(lower)

	return values[lower]*(1-weight) + values[upper]*weight
}

func (mc *metricsCollector) calculateRate(count int64, start, end time.Time) float64 {
	duration := end.Sub(start).Seconds()
	if duration <= 0 {
		return 0
	}
	return float64(count) / duration
}

func SaveMetricsToFile(metrics *Metrics, filename string) error {
	data, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write metrics file: %w", err)
	}

	return nil
}

func PrintMetricsSummary(ctx context.Context, metrics *Metrics) {
	if metrics == nil || len(metrics.Metrics) == 0 {
		return
	}

	if globalLogger != nil {
		globalLogger.Debug(ctx, "=== Performance Metrics Summary ===")
	}

	// HTTP metrics summary
	if httpReqDuration, exists := metrics.Metrics["http_req_duration"]; exists {
		values := httpReqDuration.Values
		if globalLogger != nil {
			globalLogger.Debug(ctx, "HTTP Requests:")
			globalLogger.Debug(ctx, "  Total: %v", metrics.Metrics["http_reqs"].Values["count"])
			globalLogger.Debug(ctx, "  Duration - Min: %.2fms, Max: %.2fms, Avg: %.2fms",
				values["min"], values["max"], values["avg"])
			if p50, ok := values["p(50)"].(float64); ok {
				globalLogger.Debug(ctx, "  Percentiles - P50: %.2fms, P90: %.2fms, P95: %.2fms, P99: %.2fms",
					p50, values["p(90)"], values["p(95)"], values["p(99)"])
			}
		}
	}

	// Exec metrics summary
	if execDuration, exists := metrics.Metrics["exec_duration"]; exists {
		values := execDuration.Values
		if globalLogger != nil {
			globalLogger.Debug(ctx, "Exec Commands:")
			globalLogger.Debug(ctx, "  Total: %v", metrics.Metrics["exec_commands"].Values["count"])
			globalLogger.Debug(ctx, "  Duration - Min: %.2fms, Max: %.2fms, Avg: %.2fms",
				values["min"], values["max"], values["avg"])
			if p50, ok := values["p(50)"].(float64); ok {
				globalLogger.Debug(ctx, "  Percentiles - P50: %.2fms, P90: %.2fms, P95: %.2fms, P99: %.2fms",
					p50, values["p(90)"], values["p(95)"], values["p(99)"])
			}
		}
	}

	if globalLogger != nil {
		globalLogger.Debug(ctx, "=== End Performance Metrics ===")
	}
}

const MetricsCollectorContextKey ContextKey = "metrics_collector"

// ContextKey represents a context key type
type ContextKey string

func GetMetricsCollectorFromCtx(ctx context.Context) MetricsCollector {
	if collector, ok := ctx.Value(MetricsCollectorContextKey).(MetricsCollector); ok {
		return collector
	}
	return nil
}

func (mc *metricsCollector) RecordTestStructure(groups map[string]*TestGroup, setupData map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.testGroups = groups
	mc.setupData = setupData
}

func generateID(input string) string {
	hash := md5.Sum([]byte(input))
	return fmt.Sprintf("%x", hash)
}
