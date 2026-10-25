package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"tenant-service/backend/internal/biz"
)

// Client queries the cluster-wide Prometheus HTTP API — read-only, per the
// GPU platform spec's decision that Tenant Service queries Prometheus
// rather than owning metrics storage.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns nil if baseURL is empty — callers use the nil-safe QueryScalar
// below so utilization KPIs degrade to "unavailable" rather than needing a
// separate "is Prometheus configured" check everywhere.
func New(baseURL string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// QueryScalar runs an instant PromQL query and returns its first result's
// value. ok=false (not an error) covers every "can't answer this" case —
// Prometheus unconfigured/unreachable, or the query returning no series
// (e.g. DCGM exporter not deployed yet) — so callers render "unavailable"
// instead of failing the whole dashboard request.
func (c *Client) QueryScalar(ctx context.Context, promql string) (value float64, ok bool) {
	if c == nil {
		return 0, false
	}
	u := fmt.Sprintf("%s/api/v1/query?query=%s", c.baseURL, url.QueryEscape(promql))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var qr queryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return 0, false
	}
	if qr.Status != "success" || len(qr.Data.Result) == 0 {
		return 0, false
	}
	str, isStr := qr.Data.Result[0].Value[1].(string)
	if !isStr {
		return 0, false
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// ClusterUtilization runs the three fixed PromQL queries behind the
// dashboard's utilization tiles. GPU assumes the DCGM exporter (per the GPU
// platform spec); CPU/memory assume node-exporter. Returns biz.Utilization
// directly — this method is what makes *Client satisfy
// biz.UtilizationProvider.
func (c *Client) ClusterUtilization(ctx context.Context) biz.Utilization {
	var u biz.Utilization
	if cpu, ok := c.QueryScalar(ctx, `(1 - avg(irate(node_cpu_seconds_total{mode="idle"}[5m]))) * 100`); ok {
		u.CPUPct, u.CPUOk = cpu, true
	}
	if mem, ok := c.QueryScalar(ctx, `(1 - (sum(node_memory_MemAvailable_bytes) / sum(node_memory_MemTotal_bytes))) * 100`); ok {
		u.MemoryPct, u.MemoryOk = mem, true
	}
	if gpu, ok := c.QueryScalar(ctx, `avg(DCGM_FI_DEV_GPU_UTIL)`); ok {
		u.GPUPct, u.GPUOk = gpu, true
	}
	return u
}
