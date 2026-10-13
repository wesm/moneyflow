package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/uitest"
	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
	webserver "github.com/wesm/moneyflow/internal/web"
)

type scenarioDescriptor struct {
	Fixture   string `json:"fixture"`
	ProfileID string `json:"profile_id,omitempty"`
}

// runScenario is a separate composition root. No test routes or credentials enter
// the production command, and both stores belong to the marked synthetic root.
func runScenario(ctx context.Context, root, descriptorPath, listen, basePath string) error {
	if filepath.Clean(descriptorPath) != filepath.Join(root, "scenario.json") {
		return errors.New("scenario descriptor must be scenario.json in the isolated root")
	}
	data, err := home.ReadPrivateFile(descriptorPath, 4096)
	if err != nil {
		return err
	}
	var descriptor scenarioDescriptor
	if err = json.Unmarshal(data, &descriptor); err != nil {
		return err
	}
	if descriptor.Fixture != "editing-v1" {
		return errors.New("unknown scenario fixture")
	}
	paths, err := home.ResolveCatalogRoot(root, nil, "")
	if err != nil {
		return err
	}
	clock := func() time.Time { return uitest.Fixture().ObservedAt }
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: rand.Reader, Now: clock, Version: "ui-scenario",
		InspectSession: func(_, kind string) (bool, error) { return kind == "monarch", nil },
	})
	if err != nil {
		return err
	}
	providerRoot := filepath.Join(root, "provider")
	if descriptor.ProfileID == "" {
		entry, createErr := catalog.Create(ctx, profilecatalog.CreateRequest{
			DisplayName: "UI Scenario", ProviderKind: "monarch",
			Populate: func(populateContext context.Context, entry profilecatalog.Entry) error {
				run, openErr := scenario.OpenProfile(populateContext, entry.ProfilePaths(), providerRoot, "web")
				if openErr != nil {
					return openErr
				}
				return run.Close()
			},
		})
		if createErr != nil {
			return createErr
		}
		descriptor.ProfileID = entry.ID
		data, err = json.Marshal(descriptor)
		if err != nil {
			return err
		}
		if err = home.WritePrivateFile(descriptorPath, data); err != nil {
			return err
		}
	}
	var mutex sync.Mutex
	var active *scenario.Runtime
	registry, err := webserver.NewProfileRegistry(webserver.ProfileRegistryConfig{
		Now: clock,
		Open: func(openContext context.Context, profileID string) (webserver.RegistryProfile, error) {
			if profileID != descriptor.ProfileID {
				return webserver.RegistryProfile{}, errors.New("unknown scenario profile")
			}
			entry, resolveErr := catalog.Resolve(openContext, profileID)
			if resolveErr != nil {
				return webserver.RegistryProfile{}, resolveErr
			}
			lock, lockErr := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
			if lockErr != nil {
				return webserver.RegistryProfile{}, lockErr
			}
			if validateErr := catalog.ValidateEntry(entry); validateErr != nil {
				return webserver.RegistryProfile{}, errors.Join(validateErr, lock.Release())
			}
			run, openErr := scenario.OpenProfile(openContext, entry.ProfilePaths(), providerRoot, "web")
			if openErr != nil {
				return webserver.RegistryProfile{}, errors.Join(openErr, lock.Release())
			}
			mutex.Lock()
			active = run
			mutex.Unlock()
			return webserver.RegistryProfile{ID: profileID, Paths: entry.ProfilePaths(), Service: run.Service,
				Close: func() error { return errors.Join(run.Close(), lock.Release()) }}, nil
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = registry.Close(context.Background()) }()
	origin, err := api.ResolveOrigin(listen, basePath, "")
	if err != nil {
		return err
	}
	application, err := webserver.NewServer(webserver.ServerConfig{
		Resolver: registry, Catalog: catalog, Evictor: registry, PreselectedID: descriptor.ProfileID,
		BasePath: basePath, Origin: origin, Version: "ui-scenario",
		ErrorLog: log.New(os.Stderr, "moneyflow web: ", 0),
	})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", application.Handler())
	mux.HandleFunc("/__moneyflow_scenario/", func(w http.ResponseWriter, r *http.Request) {
		lease, acquireErr := registry.Acquire(r.Context(), descriptor.ProfileID)
		if acquireErr != nil {
			http.Error(w, acquireErr.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = lease.Release() }()
		mutex.Lock()
		run := active
		mutex.Unlock()
		result, found, controlErr := controlScenario(w, r, run)
		if !found {
			http.NotFound(w, r)
			return
		}
		if controlErr != nil {
			http.Error(w, controlErr.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	server := application.HTTPServer(listen, os.Stderr)
	server.Handler = mux
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServe() }()
	stopContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err = <-serveErrors:
		return fmt.Errorf("scenario server: %w", err)
	case <-stopContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(server.Shutdown(shutdown), registry.Close(shutdown))
	}
}

// controlScenario runs one test control request against the open profile and its synthetic
// provider. found is false for an unknown control.
func controlScenario(w http.ResponseWriter, r *http.Request, run *scenario.Runtime) (any, bool, error) {
	if r.Method == http.MethodGet && r.URL.Path == "/__moneyflow_scenario/observe" {
		result, err := run.Observe(r.Context())
		return result, true, err
	}
	if r.Method != http.MethodPost {
		return nil, false, nil
	}
	body := http.MaxBytesReader(w, r.Body, 1024)
	switch r.URL.Path {
	case "/__moneyflow_scenario/fault":
		var request struct {
			Fault uitest.Fault `json:"fault"`
		}
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			return nil, true, err
		}
		return nil, true, run.Provider.SetFault(request.Fault)
	case "/__moneyflow_scenario/refresh-fault":
		var request struct {
			Fault uitest.RefreshFault `json:"fault"`
		}
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			return nil, true, err
		}
		return nil, true, run.Provider.SetRefreshFault(request.Fault)
	case "/__moneyflow_scenario/release":
		run.Provider.Release()
		return nil, true, nil
	case "/__moneyflow_scenario/advance":
		return nil, true, advanceScenario(body, run)
	case "/__moneyflow_scenario/other-refresh":
		return nil, true, holdOtherRefresh(r.Context(), body, run)
	default:
		return nil, false, nil
	}
}

func advanceScenario(body io.Reader, run *scenario.Runtime) error {
	var request struct {
		Seconds int `json:"seconds"`
	}
	if err := json.NewDecoder(body).Decode(&request); err != nil {
		return err
	}
	if request.Seconds < 0 || request.Seconds > 86400 {
		return errors.New("clock advance out of bounds")
	}
	return run.Provider.Advance(time.Duration(request.Seconds) * time.Second)
}

// otherRefreshOwner is the lease owner that stands in for another interface's refresh.
const otherRefreshOwner = "scenario-other-interface"

// holdOtherRefresh takes or releases the profile's refresh lease as another interface would,
// so a browser test can watch a refresh it did not start.
func holdOtherRefresh(ctx context.Context, body io.Reader, run *scenario.Runtime) error {
	var request struct {
		Renderer string `json:"renderer"`
		Hold     bool   `json:"hold"`
	}
	if err := json.NewDecoder(body).Decode(&request); err != nil {
		return err
	}
	if !request.Hold {
		return run.Profile.ReleaseRefreshLease(ctx, otherRefreshOwner)
	}
	now := run.Provider.Now()
	_, acquired, err := run.Profile.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: otherRefreshOwner, Renderer: request.Renderer, ExpiresAt: now.Add(time.Hour),
	}, now)
	if err == nil && !acquired {
		err = errors.New("another refresh holds the lease")
	}
	return err
}
