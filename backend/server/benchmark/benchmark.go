package benchmark

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type BenchmarkResult struct {
	Endpoint string
	Status   int
	Latency  time.Duration
	Error    error
}

type GroupedResults struct {
	System    []gin.H `json:"system"`
	Updates   []gin.H `json:"updates"`
	Docker    []gin.H `json:"docker"`
	Wireguard []gin.H `json:"wireguard"`
	Other     []gin.H `json:"other"`
}

// benchmarkHandler requires the session manager to read the real cookie name.
func benchmarkHandler(router *gin.Engine, sm *session.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// read auth cookie by the effective name from session manager
		cookieName := sm.CookieName()
		cookieVal, err := c.Cookie(cookieName)
		if err != nil || cookieVal == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// derive baseURL from the current request (works with TLS/reverse proxy)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		host := c.Request.Host
		baseURL := fmt.Sprintf("%s://%s", scheme, host)

		results := RunBenchmark(baseURL, cookieName, cookieVal, router)

		grouped := GroupedResults{
			System:    []gin.H{},
			Updates:   []gin.H{},
			Docker:    []gin.H{},
			Wireguard: []gin.H{},
			Other:     []gin.H{},
		}

		for _, r := range results {
			item := gin.H{"endpoint": r.Endpoint}
			if r.Error != nil {
				item["error"] = r.Error.Error()
			} else {
				item["status"] = r.Status
				item["latency"] = fmt.Sprintf("%.2fms", float64(r.Latency.Microseconds())/1000)
			}

			switch {
			case strings.HasPrefix(r.Endpoint, "/system/"):
				grouped.System = append(grouped.System, item)
			case strings.HasPrefix(r.Endpoint, "/updates/"):
				grouped.Updates = append(grouped.Updates, item)
			case strings.HasPrefix(r.Endpoint, "/docker/"):
				grouped.Docker = append(grouped.Docker, item)
			case strings.HasPrefix(r.Endpoint, "/wireguard/"):
				grouped.Wireguard = append(grouped.Wireguard, item)
			default:
				grouped.Other = append(grouped.Other, item)
			}
		}

		// sort each group by latency ascending; errors go last
		sortByLatency(grouped.System)
		sortByLatency(grouped.Updates)
		sortByLatency(grouped.Docker)
		sortByLatency(grouped.Wireguard)
		sortByLatency(grouped.Other)

		c.JSON(http.StatusOK, grouped)
	}
}

// RunBenchmark performs parallel benchmarking of all GET endpoints.
// It sets the Cookie header as "<cookieName>=<cookieVal>".
func RunBenchmark(baseURL, cookieName, cookieVal string, router *gin.Engine) []BenchmarkResult {
	endpoints := getBenchmarkableEndpoints(router)
	logger.Infof("📈 Running benchmark for %d endpoints...", len(endpoints))

	client := &http.Client{Timeout: 5 * time.Second}
	var wg sync.WaitGroup
	resultChan := make(chan BenchmarkResult, len(endpoints))

	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(endpoint string) {
			defer wg.Done()

			req, err := http.NewRequest("GET", baseURL+endpoint, nil)
			if err != nil {
				resultChan <- BenchmarkResult{Endpoint: endpoint, Error: err}
				return
			}
			req.Header.Set("Cookie", fmt.Sprintf("%s=%s", cookieName, cookieVal))

			start := time.Now()
			resp, err := client.Do(req)
			latency := time.Since(start)

			if err != nil {
				resultChan <- BenchmarkResult{Endpoint: endpoint, Latency: latency, Error: err}
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()

			resultChan <- BenchmarkResult{
				Endpoint: endpoint,
				Status:   resp.StatusCode,
				Latency:  latency,
			}
		}(endpoint)
	}

	wg.Wait()
	close(resultChan)

	var results []BenchmarkResult
	for res := range resultChan {
		results = append(results, res)
	}

	logger.Debugf("Benchmark completed.")
	return results
}

func getBenchmarkableEndpoints(router *gin.Engine) []string {
	var endpoints []string
	allowedPrefixes := []string{"/system/", "/updates/", "/docker/", "/wireguard/"}

	for _, route := range router.Routes() {
		if route.Method != "GET" {
			continue
		}
		// exclude parameterized paths like /system/:id
		if strings.Contains(route.Path, ":") {
			continue
		}
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(route.Path, prefix) {
				endpoints = append(endpoints, route.Path)
				break
			}
		}
	}
	return endpoints
}

// ---- helpers ----

func sortByLatency(arr []gin.H) {
	sort.SliceStable(arr, func(i, j int) bool {
		li, lok := latencyMs(arr[i])
		lj, rok := latencyMs(arr[j])

		// errors (no latency) go to the end
		if !lok && rok {
			return false
		}
		if lok && !rok {
			return true
		}
		if !lok && !rok {
			// both are errors → compare endpoints safely
			ie, iok := arr[i]["endpoint"].(string)
			je, jok := arr[j]["endpoint"].(string)
			if iok && jok {
				return ie < je
			}
			return false
		}
		return li < lj
	})
}

func latencyMs(m gin.H) (float64, bool) {
	val, ok := m["latency"]
	if !ok {
		return 0, false
	}
	s, ok := val.(string)
	if !ok {
		return 0, false
	}
	// strip trailing "ms"
	s = strings.TrimSuffix(s, "ms")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
