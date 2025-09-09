package aggregator

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"strings"
	"sync"
	"time"
)

type Config struct {
	MaxEndpoints     int    `json:"max_endpoints"`
	NoBucket         bool   `json:"no_bucket"`
	MergePercentiles string `json:"merge_percentiles"`
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

func AggregateFiles(inputFiles []string, config *Config) (*Metrics, error) {
	if config == nil {
		config = &Config{
			MaxEndpoints:     2000,
			NoBucket:         false,
			MergePercentiles: "weighted",
		}
	}

	type fileResult struct {
		metrics *Metrics
		err     error
		file    string
	}

	results := make(chan fileResult, len(inputFiles))
	var wg sync.WaitGroup

	for _, file := range inputFiles {
		wg.Add(1)
		go func(filename string) {
			defer wg.Done()
			metrics, err := ReadMetricsFile(filename)
			results <- fileResult{metrics: metrics, err: err, file: filename}
		}(file)
	}

	wg.Wait()
	close(results)

	var allMetrics []*Metrics
	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("error reading %s: %w", result.file, result.err)
		}
		allMetrics = append(allMetrics, result.metrics)
	}

	return AggregateMetrics(allMetrics, config)
}

func ReadMetricsFile(filename string) (*Metrics, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var metrics Metrics
	err = json.Unmarshal(data, &metrics)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", filename, err)
	}

	return &metrics, nil
}

func AggregateMetrics(metricsList []*Metrics, config *Config) (*Metrics, error) {
	if len(metricsList) == 0 {
		return nil, fmt.Errorf("no metrics to aggregate")
	}

	result := &Metrics{
		RootGroup: &TestGroup{
			Name:   "",
			Path:   "",
			ID:     "d41d8cd98f00b204e9800998ecf8427e",
			Groups: make(map[string]*TestGroup),
			Checks: make(map[string]*TestCheck),
		},
		Metrics:   make(map[string]*Metric),
		SetupData: make(map[string]string),
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}

	endpointMap := make(map[string]string)
	endpointCount := 0
	endpointsBucketed := 0
	for i, metrics := range metricsList {
		if i == 0 || metrics.StartTime.Before(result.StartTime) {
			result.StartTime = metrics.StartTime
		}
		if metrics.EndTime.After(result.EndTime) {
			result.EndTime = metrics.EndTime
		}

		for checkName, check := range metrics.RootGroup.Checks {
			if existing, exists := result.RootGroup.Checks[checkName]; exists {
				existing.Passes += check.Passes
				existing.Fails += check.Fails
			} else {
				result.RootGroup.Checks[checkName] = &TestCheck{
					Name:   check.Name,
					Path:   check.Path,
					ID:     check.ID,
					Passes: check.Passes,
					Fails:  check.Fails,
				}
			}
		}

		for metricName, metric := range metrics.Metrics {
			if isGlobalMetric(metricName) {
				continue
			}

			normalizedName := normalizeEndpoint(metricName)

			if endpointCount >= config.MaxEndpoints {
				if config.NoBucket {
					continue
				} else {
					normalizedName = "other"
					endpointsBucketed++
				}
			} else {
				if existingOriginal, exists := endpointMap[normalizedName]; exists && existingOriginal != metricName {
					hash := fmt.Sprintf("%x", md5.Sum([]byte(metricName)))[:8]
					normalizedName = normalizedName + "_" + hash
				}
				endpointMap[normalizedName] = metricName
				endpointCount++
			}

			if existing, exists := result.Metrics[normalizedName]; exists {
				mergeMetric(existing, metric, config.MergePercentiles)
			} else {
				result.Metrics[normalizedName] = cloneMetric(metric)
			}
		}
	}

	addGlobalMetrics(result, metricsList)

	return result, nil
}

func isGlobalMetric(name string) bool {
	globalMetrics := []string{
		"checks", "data_received", "data_sent", "http_req_duration",
		"http_req_failed", "http_reqs", "iterations", "vus", "vus_max",
		"http_req_blocked", "http_req_connecting", "http_req_sending",
		"http_req_waiting", "http_req_receiving", "http_req_tls_handshaking",
	}

	for _, global := range globalMetrics {
		if name == global || strings.HasPrefix(name, global+"_") {
			return true
		}
	}
	return false
}

func normalizeEndpoint(endpoint string) string {
	return endpoint
}

func mergeMetric(target, source *Metric, mergeStrategy string) {
	if target.Type != source.Type {
		return
	}

	switch target.Type {
	case "trend":
		mergeTrendMetric(target, source, mergeStrategy)
	case "counter":
		mergeCounterMetric(target, source)
	case "rate":
		mergeRateMetric(target, source)
	case "gauge":
		mergeGaugeMetric(target, source)
	}
}

func mergeTrendMetric(target, source *Metric, mergeStrategy string) {
	targetValues := target.Values
	sourceValues := source.Values

	targetCount := getFloat64(targetValues, "count", 0)
	sourceCount := getFloat64(sourceValues, "count", 0)
	totalCount := targetCount + sourceCount

	if totalCount == 0 {
		return
	}

	targetValues["count"] = totalCount
	targetValues["min"] = math.Min(getFloat64(targetValues, "min", math.MaxFloat64), getFloat64(sourceValues, "min", math.MaxFloat64))
	targetValues["max"] = math.Max(getFloat64(targetValues, "max", 0), getFloat64(sourceValues, "max", 0))

	targetAvg := getFloat64(targetValues, "avg", 0)
	sourceAvg := getFloat64(sourceValues, "avg", 0)
	targetSum := targetAvg * targetCount
	sourceSum := sourceAvg * sourceCount
	totalSum := targetSum + sourceSum
	targetValues["avg"] = totalSum / totalCount

	percentiles := []string{"p(50)", "p(90)", "p(95)", "p(99)"}
	for _, p := range percentiles {
		if _, targetExists := targetValues[p]; targetExists {
			if _, sourceExists := sourceValues[p]; sourceExists {
				targetP := getFloat64(targetValues, p, 0)
				sourceP := getFloat64(sourceValues, p, 0)
				weightedP := (targetP*targetCount + sourceP*sourceCount) / totalCount
				targetValues[p] = weightedP
			}
		}
	}

	if totalCount > 0 {
		duration := getFloat64(targetValues, "duration", 1)
		targetValues["rate"] = totalCount / duration
	}
}

func mergeCounterMetric(target, source *Metric) {
	targetValues := target.Values
	sourceValues := source.Values

	targetCount := getFloat64(targetValues, "count", 0)
	sourceCount := getFloat64(sourceValues, "count", 0)
	targetValues["count"] = targetCount + sourceCount

	totalCount := targetCount + sourceCount
	if totalCount > 0 {
		duration := getFloat64(targetValues, "duration", 1)
		targetValues["rate"] = totalCount / duration
	}
}

func mergeRateMetric(target, source *Metric) {
	targetValues := target.Values
	sourceValues := source.Values

	targetPasses := getFloat64(targetValues, "passes", 0)
	sourcePasses := getFloat64(sourceValues, "passes", 0)
	targetFails := getFloat64(targetValues, "fails", 0)
	sourceFails := getFloat64(sourceValues, "fails", 0)

	targetValues["passes"] = targetPasses + sourcePasses
	targetValues["fails"] = targetFails + sourceFails

	totalPasses := targetPasses + sourcePasses
	totalFails := targetFails + sourceFails
	total := totalPasses + totalFails
	if total > 0 {
		targetValues["value"] = totalPasses / total
	}
}

func mergeGaugeMetric(target, source *Metric) {
	targetValues := target.Values
	sourceValues := source.Values

	for key, sourceVal := range sourceValues {
		if sourceFloat, ok := sourceVal.(float64); ok {
			if targetFloat, exists := targetValues[key]; exists {
				if targetFloatFloat, ok := targetFloat.(float64); ok {
					targetValues[key] = math.Max(targetFloatFloat, sourceFloat)
				}
			} else {
				targetValues[key] = sourceFloat
			}
		}
	}
}

func addGlobalMetrics(result *Metrics, metricsList []*Metrics) {
	globalMetrics := make(map[string]*Metric)

	for _, metrics := range metricsList {
		for metricName, metric := range metrics.Metrics {
			if isGlobalMetric(metricName) {
				if existing, exists := globalMetrics[metricName]; exists {
					mergeMetric(existing, metric, "weighted")
				} else {
					globalMetrics[metricName] = cloneMetric(metric)
				}
			}
		}
	}

	for name, metric := range globalMetrics {
		result.Metrics[name] = metric
	}
}

func cloneMetric(metric *Metric) *Metric {
	cloned := &Metric{
		Type:   metric.Type,
		Values: make(map[string]interface{}),
	}

	for k, v := range metric.Values {
		cloned.Values[k] = v
	}

	return cloned
}

func getFloat64(values map[string]interface{}, key string, defaultValue float64) float64 {
	if val, exists := values[key]; exists {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return defaultValue
}

func WriteOutput(metrics *Metrics, filename string) error {
	data, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling metrics: %w", err)
	}

	err = ioutil.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("error writing file %s: %w", filename, err)
	}

	return nil
}
