// Command embedconsumer builds moneyflow's embedding package from a separate module, as a host
// application does, and serves one request through it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	moneyflow "github.com/wesm/moneyflow"
)

const basePath = "/embedded/moneyflow/"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	parent, err := os.MkdirTemp("", "embedconsumer-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(parent)) }()
	instance, err := moneyflow.Open(context.Background(), moneyflow.Options{
		Root: filepath.Join(parent, "data"), APIBasePath: basePath,
	})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, instance.Close()) }()
	server := mcp.NewServer(&mcp.Implementation{Name: "host", Version: "1"}, nil)
	err = instance.RegisterTools(server, moneyflow.ToolOptions{
		Classes: []moneyflow.Class{moneyflow.ClassRead}, Prefix: "moneyflow_",
	})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle(basePath, instance.API())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath+"api/v1/profiles", nil))
	if response.Code != http.StatusOK {
		return fmt.Errorf("list profiles: status %d: %s", response.Code, response.Body)
	}
	fmt.Printf("moneyflow embedded: %d operations\n", len(instance.Operations()))
	return nil
}
