# Venom Metrics & Performance Reporting

Venom now includes comprehensive metrics collection and performance reporting capabilities for API testing.

## Quick Start

### 1. Enable Metrics Collection
```bash
# Collect metrics during test execution
venom run --metrics-enabled --metrics-output=metrics.json tests/

# With parallel execution
venom run --metrics-enabled --metrics-output=parallel_output/metrics_{#}.json tests/ --parallel
```

### 2. Generate Performance Reports
```bash
# HTML report with default thresholds
venom metrics-report metrics.json --html-output=report.html

# Check thresholds and generate report
venom metrics-report metrics.json --check-thresholds --thresholds=thresholds.yml --html-output=report.html

# HTML-only generation (no threshold validation)
venom metrics-report metrics.json --html-only --html-output=report.html
```

## Features

- **Metrics Collection**: HTTP request timing, status codes, and test assertion results
- **Dynamic Path Normalization (DPN)**: Aggregates similar endpoints (e.g., `/users/123` → `/users/*`)
- **Performance Thresholds**: Configurable SLA validation with YAML configuration
- **Interactive HTML Reports**: Charts, tables, and filtering with Chart.js
- **CI Integration**: JUnit XML output for threshold violations
- **Soft Fail Mode**: Non-blocking CI runs (default behavior)

## Configuration

### Thresholds File (`thresholds.yml`)
```yaml
options:
  min_samples: 20        # Minimum samples for reliable metrics
  tolerance_percent: 10  # 10% headroom above thresholds
  soft_fail: false       # Exit with error on violations

defaults:
  p95: 500ms            # 95th percentile response time
  p99: 1000ms           # 99th percentile response time
  avg: 200ms            # Average response time

# Group-based thresholds
groups:
  "POST_groups/*":
    p95: 30000ms
    avg: 15000ms

# Endpoint-specific overrides
endpoints:
  "POST_specific_endpoint":
    p95: 30000ms
    avg: 25000ms
```

## CLI Options

### `venom run`
- `--metrics-enabled`: Enable metrics collection
- `--metrics-output=FILE`: Output file for metrics (supports `{#}` placeholder)

### `venom metrics-report`
- `--check-thresholds`: Validate metrics against thresholds
- `--thresholds=FILE`: Custom thresholds file (default: `thresholds.yml`)
- `--html-output=FILE`: Generate HTML report
- `--html-only`: Generate HTML without threshold validation
- `--fail-on-breaches`: Exit with error on threshold violations (default: soft fail)

## Output Files

- **Metrics JSON**: HTTP and test check metrics
- **HTML Report**: Interactive dashboard with charts, tables, and filtering
- **JUnit XML**: CI-compatible output for threshold breaches (when using `--check-thresholds`)