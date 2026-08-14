package api

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
)

const performanceTransactionCount = 100_000

var performanceProjectionBytes []byte

func TestProjectionPerformance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}

	service, err := app.NewService(fixture.Generate(20260813, performanceTransactionCount))
	require.NoError(t, err)
	_, err = projectAndEncode100K(service)
	require.NoError(t, err)

	start := time.Now()
	encoded, err := projectAndEncode100K(service)
	duration := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	require.Less(t, duration, time.Second,
		"URL decode, query, 200-row/chart projection, and JSON encoding took %s", duration)
}

func BenchmarkProjection100K(b *testing.B) {
	service, err := app.NewService(fixture.Generate(20260813, performanceTransactionCount))
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		performanceProjectionBytes, err = projectAndEncode100K(service)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func projectAndEncode100K(service *app.Service) ([]byte, error) {
	state, canonical, err := DecodeViewQuery("v=1")
	if err != nil {
		return nil, err
	}
	projection, err := service.ProjectView(
		state,
		app.EmptySelection(),
		app.WindowRequest{Limit: app.DefaultWindowLimit},
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(projectionToWire(projection, canonical, nil))
}
