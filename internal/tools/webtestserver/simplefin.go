package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// This fixture exercises the production TLS client and private session file;
// only claiming and time are controlled by the isolated browser-test server.
type syntheticSimpleFIN struct {
	server *httptest.Server
	claims atomic.Int32
	writes atomic.Int32
	failed atomic.Bool
	offset atomic.Int64
}

func newSyntheticSimpleFIN() *syntheticSimpleFIN {
	fixture := &syntheticSimpleFIN{}
	fixture.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.writes.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		user, mode, ok := r.BasicAuth()
		if !ok || user != "synthetic" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if mode == "retry" && fixture.failed.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mode == "empty" {
			_, _ = w.Write([]byte(`{"errlist":[],"connections":[],"accounts":[]}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"errlist":[],"connections":[],"accounts":[{"id":"account","conn_id":"connection","name":"Checking","currency":"USD","transactions":[{"id":"transaction","posted":%d,"amount":"-12.34","description":"Example Merchant"}]}]}`, time.Now().Add(-24*time.Hour).UTC().Truncate(24*time.Hour).Unix())
	}))
	return fixture
}

func (fixture *syntheticSimpleFIN) now() time.Time {
	return time.Now().Add(time.Duration(fixture.offset.Load()))
}

func (fixture *syntheticSimpleFIN) runtime(paths home.Paths) (simplefinonboarding.Runtime, error) {
	sessions, err := simplefin.NewSessionStore(paths)
	if err != nil {
		return simplefinonboarding.Runtime{}, err
	}
	return simplefinonboarding.Runtime{Sessions: sessions, InstanceID: "webtestserver-simplefin", Now: fixture.now,
		Claim: func(ctx context.Context, input string) (string, error) {
			mode := strings.TrimPrefix(input, "synthetic-simplefin-")
			if mode != "normal" && mode != "empty" && mode != "retry" {
				return "", errors.New("invalid synthetic connection")
			}
			fixture.claims.Add(1)
			access := strings.Replace(fixture.server.URL, "https://", "https://synthetic:"+mode+"@", 1) + "/access"
			return simplefin.Claim(ctx, access, fixture.server.Client())
		},
		NewSource: func(connection app.ProviderConnectionState, session simplefin.Session) (provider.ReaderSource, error) {
			remoteID := ""
			if connection.Bound {
				remoteID = connection.RemoteProfileID
			}
			return simplefin.NewSource(simplefin.SourceOptions{ExpectedRemoteID: remoteID, Currency: session.Import.Currency, Scale: session.Import.Scale, HTTPClient: fixture.server.Client()}, sessions)
		},
	}, nil
}

func (fixture *syntheticSimpleFIN) configure(ctx context.Context, paths home.Paths, service *app.Service) error {
	connection, err := service.ProviderConnection(ctx)
	if err != nil || !connection.Bound || connection.Kind != "simplefin" {
		return err
	}
	runtime, err := fixture.runtime(paths)
	if err != nil {
		return err
	}
	source, err := runtime.NewSource(connection, simplefin.Session{Import: simplefin.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}})
	if err != nil {
		return err
	}
	return service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, Provider: "simplefin", Currency: connection.Currency, Scale: connection.Scale, Renderer: "web", InstanceID: runtime.InstanceID, Now: fixture.now})
}

func (fixture *syntheticSimpleFIN) control(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/__moneyflow_test/simplefin" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int32{"claims": fixture.claims.Load(), "writes": fixture.writes.Load()})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/__moneyflow_test/simplefin/advance" {
			fixture.offset.Add(int64(time.Hour + time.Second))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
