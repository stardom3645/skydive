package server

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

const (
	defaultWallPrometheusURL = "http://127.0.0.1:3001"
	defaultTrendRange        = 3 * time.Hour
	defaultTrendStep         = 60 * time.Second
)

type wallHostTrendResponse struct {
	Host      string                 `json:"host"`
	Range     string                 `json:"range"`
	Step      string                 `json:"step"`
	Start     int64                  `json:"start"`
	End       int64                  `json:"end"`
	PromURL   string                 `json:"promUrl,omitempty"`
	Series    []wallHostTrendSeries  `json:"series"`
	Warnings  []string               `json:"warnings,omitempty"`
	RawLabels map[string]interface{} `json:"rawLabels,omitempty"`
}

type wallHostTrendSeries struct {
	Key       string                 `json:"key"`
	Label     string                 `json:"label"`
	Unit      string                 `json:"unit"`
	Query     string                 `json:"query,omitempty"`
	Values    []wallHostTrendPoint   `json:"values"`
	LastValue *float64               `json:"lastValue,omitempty"`
	Labels    map[string]interface{} `json:"labels,omitempty"`
}

type wallHostTrendPoint struct {
	Timestamp int64    `json:"timestamp"`
	Value     *float64 `json:"value"`
}

type prometheusQueryRangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]interface{} `json:"metric"`
			Values [][]interface{}        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func RegisterWallHostTrendAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/wall/hosts/trend", handleWallHostTrend).Methods("GET")
}

func handleWallHostTrend(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	managementIP := strings.TrimSpace(r.URL.Query().Get("managementIp"))
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))

	matchValues := uniqueNonEmptyStrings(host, name, managementIP, ip)
	if len(matchValues) == 0 {
		writeWallHostTrendError(w, http.StatusBadRequest, "host, name, managementIp or ip is required.")
		return
	}

	// Wall 대시보드의 $host, $job 변수는 Prometheus API에서 자동 치환되지 않습니다.
	// API에서는 실제 라벨 값으로 변환하여 query_range를 호출합니다.
	prometheusHost := firstNonEmptyString(managementIP, ip, host, name)
	prometheusJob := firstNonEmptyString(strings.TrimSpace(r.URL.Query().Get("job")), "cube")
	prometheusPort := firstNonEmptyString(strings.TrimSpace(r.URL.Query().Get("port")), "3003")
	prometheusInstance := prometheusHost + ":" + prometheusPort

	trendRange := parseDurationOrDefault(r.URL.Query().Get("range"), defaultTrendRange)
	step := parseDurationOrDefault(r.URL.Query().Get("step"), defaultTrendStep)
	if step < 30*time.Second {
		step = 30 * time.Second
	}

	end := time.Now()
	start := end.Add(-trendRange)

	queries := []wallHostTrendSeries{
		{
			Key:   "cpu",
			Label: "CPU",
			Unit:  "percent",
			Query: fmt.Sprintf(
				`sum (sum by (mode) (irate(node_cpu_seconds_total{instance="%s", job="%s", mode=~"(irq|nice|softirq|steal|system|user|iowait)"}[1m])) / scalar(sum(irate(node_cpu_seconds_total{instance="%s", job="%s"}[1m]))) * 100)`,
				prometheusInstance,
				prometheusJob,
				prometheusInstance,
				prometheusJob,
			),
		},
		{
			Key:   "memory",
			Label: "Memory",
			Unit:  "percent",
			Query: fmt.Sprintf(
				`(1 - (node_memory_MemAvailable_bytes{instance="%s", job="%s"} / node_memory_MemTotal_bytes{instance="%s", job="%s"})) * 100`,
				prometheusInstance,
				prometheusJob,
				prometheusInstance,
				prometheusJob,
			),
		},
		{
			Key:   "disk",
			Label: "Disk",
			Unit:  "percent",
			Query: fmt.Sprintf(
				`max ((1 - (node_filesystem_avail_bytes{instance="%s", job="%s", fstype!~"tmpfs|overlay|squashfs|autofs|proc|sysfs", mountpoint!~"/run.*|/var/lib/docker/.*|/var/lib/containers/.*"} / node_filesystem_size_bytes{instance="%s", job="%s", fstype!~"tmpfs|overlay|squashfs|autofs|proc|sysfs", mountpoint!~"/run.*|/var/lib/docker/.*|/var/lib/containers/.*"})) * 100)`,
				prometheusInstance,
				prometheusJob,
				prometheusInstance,
				prometheusJob,
			),
		},
		{
			Key:   "networkRx",
			Label: "Network RX",
			Unit:  "bps",
			Query: fmt.Sprintf(
				`sum (irate(node_network_receive_bytes_total{instance="%s", job="%s", device!~"lo|veth.*|docker.*|br.*|virbr.*|tap.*"}[1m])) * 8`,
				prometheusInstance,
				prometheusJob,
			),
		},
		{
			Key:   "networkTx",
			Label: "Network TX",
			Unit:  "bps",
			Query: fmt.Sprintf(
				`sum (irate(node_network_transmit_bytes_total{instance="%s", job="%s", device!~"lo|veth.*|docker.*|br.*|virbr.*|tap.*"}[1m])) * 8`,
				prometheusInstance,
				prometheusJob,
			),
		},
	}

	prometheusURL := wallPrometheusURL()
	client := &http.Client{Timeout: 10 * time.Second}
	series := make([]wallHostTrendSeries, 0, len(queries))
	warnings := make([]string, 0)

	for _, item := range queries {
		result, err := queryPrometheusRange(client, prometheusURL, item.Query, start, end, step)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s query failed: %s", item.Key, err.Error()))
			item.Values = []wallHostTrendPoint{}
			series = append(series, item)
			continue
		}

		points, labels := pickPrometheusSeries(result)
		item.Values = points
		item.Labels = labels
		item.LastValue = lastTrendValue(points)
		series = append(series, item)
	}

	writeWallHostTrendJSON(w, http.StatusOK, wallHostTrendResponse{
		Host:     firstNonEmptyString(host, name, managementIP, ip),
		Range:    trendRange.String(),
		Step:     step.String(),
		Start:    start.Unix(),
		End:      end.Unix(),
		PromURL:  prometheusURL,
		Series:   series,
		Warnings: warnings,
	})
}

func queryPrometheusRange(client *http.Client, prometheusURL, query string, start, end time.Time, step time.Duration) (*prometheusQueryRangeResponse, error) {
	baseURL, err := url.Parse(strings.TrimRight(prometheusURL, "/") + "/api/v1/query_range")
	if err != nil {
		return nil, err
	}

	params := baseURL.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.Itoa(int(step.Seconds())))
	baseURL.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if token := strings.TrimSpace(os.Getenv("WALL_PROMETHEUS_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}

	var parsed prometheusQueryRangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return nil, fmt.Errorf(parsed.Error)
		}
		return nil, fmt.Errorf("prometheus query was not successful")
	}

	return &parsed, nil
}

func pickPrometheusSeries(result *prometheusQueryRangeResponse) ([]wallHostTrendPoint, map[string]interface{}) {
	if result == nil || len(result.Data.Result) == 0 {
		return []wallHostTrendPoint{}, nil
	}

	bestIndex := 0
	bestLength := 0
	for i, item := range result.Data.Result {
		if len(item.Values) > bestLength {
			bestIndex = i
			bestLength = len(item.Values)
		}
	}

	item := result.Data.Result[bestIndex]
	points := make([]wallHostTrendPoint, 0, len(item.Values))
	for _, raw := range item.Values {
		if len(raw) < 2 {
			continue
		}

		timestamp := parsePrometheusTimestamp(raw[0])
		value := parsePrometheusFloat(raw[1])
		points = append(points, wallHostTrendPoint{
			Timestamp: timestamp,
			Value:     value,
		})
	}

	return points, item.Metric
}

func parsePrometheusTimestamp(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return int64(f)
	default:
		return 0
	}
}

func parsePrometheusFloat(value interface{}) *float64 {
	var parsed float64
	var err error

	switch v := value.(type) {
	case string:
		parsed, err = strconv.ParseFloat(v, 64)
	case float64:
		parsed = v
	case json.Number:
		parsed, err = v.Float64()
	default:
		return nil
	}

	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}

	return &parsed
}

func lastTrendValue(points []wallHostTrendPoint) *float64 {
	for i := len(points) - 1; i >= 0; i-- {
		if points[i].Value != nil {
			return points[i].Value
		}
	}
	return nil
}

func wallPrometheusURL() string {
	for _, key := range []string{"NETDIVE_WALL_PROMETHEUS_URL", "WALL_PROMETHEUS_URL", "PROMETHEUS_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	return defaultWallPrometheusURL
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}

	return duration
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}

	return result
}

func writeWallHostTrendJSON(w http.ResponseWriter, status int, response wallHostTrendResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func writeWallHostTrendError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": message,
	})
}
