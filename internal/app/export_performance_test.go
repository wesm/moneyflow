package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

func BenchmarkCaptureExport100K(b *testing.B) {
	committed, err := fixture.CommittedProfile(fixture.Generate(20260819, 100_000))
	if err != nil {
		b.Fatal(err)
	}
	profile := &memoryProfile{snapshot: domain.ProfileSnapshot{Revision: 1, Committed: committed}}
	service, err := app.NewProfileService(context.Background(), profile)
	if err != nil {
		b.Fatal(err)
	}
	request := app.ExportRequest{
		Scope: app.ExportScopeFull, State: detailViewState(), AppVersion: "benchmark",
		ExportedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err = service.CaptureExport(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}
