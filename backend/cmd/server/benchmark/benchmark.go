package benchmark

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

type BenchmarkResult struct {
	Endpoint string
	Status   int
	Latency  time.Duration
	Error    error
}

func RegisterDebugRoutes(router *gin.Engine, env string) {
	if env != "production" {
		router.GET("/debug/benchmark", benchmarkHandler(router))
	}
}

func benchmarkHandler(router *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err != nil {
			c.JSON(401, gin.H{"error": "unauthorized"})
			return
		}
		results := RunBenchmark("http://localhost:8080", "session_id="+cookie, router, 8)
		var output []gin.H
		for _, r := range results {
			if r.Error != nil {
				output = append(output, gin.H{"endpoint": r.Endpoint, "error": r.Error.Error()})
			} else {
				output = append(output, gin.H{
					"endpoint": r.Endpoint,
					"status":   r.Status,
					"latency":  fmt.Sprintf("%.2fms", float64(r.Latency.Microseconds())/1000),
				})
			}
		}
		c.JSON(200, output)
	}
}

// RunBenchmark performs parallel benchmarking of all GET /system/* endpoints
func RunBenchmark(baseURL string, sessionCookie string, router *gin.Engine, concurrency int) []BenchmarkResult {
	endpoints := getBenchmarkableEndpoints(router)
	logger.Infof("📈 Running benchmark for %d /system/ endpoints...", len(endpoints))

	client := &http.Client{Timeout: 5 * time.Second}
	var wg sync.WaitGroup
	results := make([]BenchmarkResult, len(endpoints))
	resultChan := make(chan BenchmarkResult, len(endpoints))

	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(endpoint string) {
			defer wg.Done()

			req, err := http.NewRequest("GET", baseURL+endpoint, nil)
			if err != nil {
				logger.Errorf("❌ Failed to create request for %s: %v", endpoint, err)
				resultChan <- BenchmarkResult{Endpoint: endpoint, Error: err}
				return
			}
			req.Header.Set("Cookie", sessionCookie)

			start := time.Now()
			resp, err := client.Do(req)
			latency := time.Since(start)

			if err != nil {
				logger.Warnf("⚠️ Request to %s failed: %v", endpoint, err)
				resultChan <- BenchmarkResult{Endpoint: endpoint, Latency: latency, Error: err}
				return
			}
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					logger.Warnf("failed to close response body: %v", cerr)
				}
			}()
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				logger.Warnf("failed to discard response body: %v", err)
			}

			logger.Debugf("✅ %s -> %d in %.2fms", endpoint, resp.StatusCode, float64(latency.Microseconds())/1000)

			resultChan <- BenchmarkResult{
				Endpoint: endpoint,
				Status:   resp.StatusCode,
				Latency:  latency,
			}
		}(endpoint)
	}

	wg.Wait()
	close(resultChan)

	i := 0
	for res := range resultChan {
		results[i] = res
		i++
	}

	logger.Infof("✅ Benchmark completed.")
	return results
}

func getBenchmarkableEndpoints(router *gin.Engine) []string {
	var endpoints []string
	allowedPrefixes := []string{"/system/", "/docker/", "/wireguard/"}

	for _, route := range router.Routes() {
		if route.Method != "GET" {
			continue
		}
		for _, prefix := range allowedPrefixes {
			if len(route.Path) >= len(prefix) && route.Path[:len(prefix)] == prefix {
				endpoints = append(endpoints, route.Path)
				break
			}
		}
	}

	logger.Debugf("🔍 Found %d GET benchmarkable routes", len(endpoints))
	return endpoints
}
