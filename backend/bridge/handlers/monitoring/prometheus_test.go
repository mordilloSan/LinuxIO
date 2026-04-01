package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCPUSeriesQueriesPrometheusAndNormalizesPoints(t *testing.T) {
	originalResolver := resolvePrometheusBaseURL
	originalClient := httpClient
	t.Cleanup(func() {
		resolvePrometheusBaseURL = originalResolver
		httpClient = originalClient
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, prometheusQueryPath, r.URL.Path)
		require.Equal(t, cpuUsageQuery, r.URL.Query().Get("query"))
		require.Equal(t, "5s", r.URL.Query().Get("step"))
		require.NotEmpty(t, r.URL.Query().Get("start"))
		require.NotEmpty(t, r.URL.Query().Get("end"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"status":"success",
			"data":{
				"resultType":"matrix",
				"result":[
					{"metric":{"instance":"a"},"values":[[1711710000,"10"],[1711710005,"20"]]},
					{"metric":{"instance":"b"},"values":[[1711710000,"30"],[1711710005,"50"]]}
				]
			}
		}`)
	}))
	defer srv.Close()

	resolvePrometheusBaseURL = func(context.Context) (string, error) {
		return srv.URL, nil
	}
	httpClient = srv.Client()

	result := GetCPUSeries(context.Background(), "5m")

	require.True(t, result.Available)
	require.Equal(t, "5m", result.Range)
	require.Equal(t, 5, result.StepSeconds)
	require.Len(t, result.Points, 2)
	require.Equal(t, int64(1711710000000), result.Points[0].TS)
	require.Equal(t, 20.0, result.Points[0].Value)
	require.Equal(t, 35.0, result.Points[1].Value)
}

func TestGetMemorySeriesReturnsUnavailableOnInvalidRange(t *testing.T) {
	result := GetMemorySeries(context.Background(), "2m")

	require.False(t, result.Available)
	require.Equal(t, "2m", result.Range)
	require.Equal(t, 0, result.StepSeconds)
	require.Equal(t, "unsupported monitoring range", result.Reason)
}

func TestGetGPUSeriesQueriesPrometheusAndNormalizesPoints(t *testing.T) {
	originalResolver := resolvePrometheusBaseURL
	originalClient := httpClient
	t.Cleanup(func() {
		resolvePrometheusBaseURL = originalResolver
		httpClient = originalClient
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, prometheusQueryPath, r.URL.Path)
		require.Equal(t, gpuUsageQuery, r.URL.Query().Get("query"))
		require.Equal(t, "5s", r.URL.Query().Get("step"))
		require.NotEmpty(t, r.URL.Query().Get("start"))
		require.NotEmpty(t, r.URL.Query().Get("end"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"status":"success",
			"data":{
				"resultType":"matrix",
				"result":[
					{"metric":{"card":"0","vendor":"nvidia"},"values":[[1711710000,"25"],[1711710005,"50"]]},
					{"metric":{"card":"1","vendor":"intel"},"values":[[1711710000,"75"],[1711710005,"25"]]}
				]
			}
		}`)
	}))
	defer srv.Close()

	resolvePrometheusBaseURL = func(context.Context) (string, error) {
		return srv.URL, nil
	}
	httpClient = srv.Client()

	result := GetGPUSeries(context.Background(), "1m")

	require.True(t, result.Available)
	require.Equal(t, "1m", result.Range)
	require.Equal(t, 5, result.StepSeconds)
	require.Len(t, result.Points, 2)
	require.Equal(t, int64(1711710000000), result.Points[0].TS)
	require.Equal(t, 50.0, result.Points[0].Value)
	require.Equal(t, 37.5, result.Points[1].Value)
}

func TestGetNetworkSeriesQueriesPrometheusAndAlignsPoints(t *testing.T) {
	originalResolver := resolvePrometheusBaseURL
	originalClient := httpClient
	t.Cleanup(func() {
		resolvePrometheusBaseURL = originalResolver
		httpClient = originalClient
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, prometheusQueryPath, r.URL.Path)
		require.Equal(t, "5s", r.URL.Query().Get("step"))
		require.NotEmpty(t, r.URL.Query().Get("start"))
		require.NotEmpty(t, r.URL.Query().Get("end"))

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("query") {
		case buildNetworkRateQuery(networkReceiveMetric, "eth0"):
			fmt.Fprint(w, `{
				"status":"success",
				"data":{
					"resultType":"matrix",
					"result":[
						{"metric":{"device":"eth0"},"values":[[1711710000,"100"],[1711710005,"200"]]}
					]
				}
			}`)
		case buildNetworkRateQuery(networkTransmitMetric, "eth0"):
			fmt.Fprint(w, `{
				"status":"success",
				"data":{
					"resultType":"matrix",
					"result":[
						{"metric":{"device":"eth0"},"values":[[1711710005,"50"],[1711710010,"75"]]}
					]
				}
			}`)
		default:
			t.Fatalf("unexpected query %q", r.URL.Query().Get("query"))
		}
	}))
	defer srv.Close()

	resolvePrometheusBaseURL = func(context.Context) (string, error) {
		return srv.URL, nil
	}
	httpClient = srv.Client()

	result := GetNetworkSeries(context.Background(), "1m", "eth0")

	require.True(t, result.Available)
	require.Equal(t, "1m", result.Range)
	require.Equal(t, 5, result.StepSeconds)
	require.Len(t, result.RXPoints, 3)
	require.Len(t, result.TXPoints, 3)

	require.Equal(t, int64(1711710000000), result.RXPoints[0].TS)
	require.Equal(t, 100.0, result.RXPoints[0].Value)
	require.Equal(t, 0.0, result.TXPoints[0].Value)

	require.Equal(t, int64(1711710005000), result.RXPoints[1].TS)
	require.Equal(t, 200.0, result.RXPoints[1].Value)
	require.Equal(t, 50.0, result.TXPoints[1].Value)

	require.Equal(t, int64(1711710010000), result.RXPoints[2].TS)
	require.Equal(t, 0.0, result.RXPoints[2].Value)
	require.Equal(t, 75.0, result.TXPoints[2].Value)
}
