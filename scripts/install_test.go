package scripts

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinaryInstallers(t *testing.T) {
	for _, installer := range []string{"shell", "powershell"} {
		t.Run(installer, func(t *testing.T) {
			t.Parallel()
			platform := runtime.GOOS
			program, script, binary := "sh", "install.sh", "moneyflow"
			arguments := []string{}
			if installer == "powershell" {
				platform = "windows"
				program, script, binary = "pwsh", "install.ps1", "moneyflow.exe"
				arguments = []string{"-NoProfile", "-NonInteractive", "-File"}
			} else if platform != "linux" && platform != "darwin" {
				t.Skip("shell installer targets Linux and macOS")
			}
			if _, err := exec.LookPath(program); err != nil {
				t.Skipf("%s is not installed", program)
			}
			script, err := filepath.Abs(script)
			require.NoError(t, err)
			arguments = append(arguments, script)
			run := func(t *testing.T, directory, releaseURL, version, location string) ([]byte, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				runArguments := arguments
				if location != "" {
					runArguments = []string{"-NoProfile", "-NonInteractive", "-Command",
						"Set-Location -LiteralPath $env:MONEYFLOW_TEST_LOCATION; & $env:MONEYFLOW_TEST_INSTALLER"}
				}
				// #nosec G204 -- fixed shell executables run repository scripts against temporary fixtures.
				command := exec.CommandContext(ctx, program, runArguments...)
				command.Dir = t.TempDir()
				for _, variable := range os.Environ() {
					if !strings.HasPrefix(variable, "MONEYFLOW_") &&
						!strings.HasPrefix(variable, "PROCESSOR_ARCHITECTURE=") &&
						!strings.HasPrefix(variable, "PROCESSOR_ARCHITEW6432=") {
						command.Env = append(command.Env, variable)
					}
				}
				architecture := "AMD64"
				if runtime.GOARCH == "arm64" {
					architecture = "ARM64"
				}
				command.Env = append(command.Env,
					"MONEYFLOW_INSTALL_DIR="+directory,
					"MONEYFLOW_RELEASE_BASE_URL="+releaseURL,
					"MONEYFLOW_VERSION="+version,
					"MONEYFLOW_TEST_LOCATION="+location,
					"MONEYFLOW_TEST_INSTALLER="+script,
					"PROCESSOR_ARCHITECTURE="+architecture,
				)
				return command.CombinedOutput()
			}

			t.Run("latest install and pinned prerelease update", func(t *testing.T) {
				directory := filepath.Join(t.TempDir(), "install directory")
				for index, version := range []string{"v1.2.3", "v1.2.4-rc.1"} {
					contents := []byte("fixture binary " + version)
					server := installerRelease(t, platform, version, binary, contents, "")
					pin := version
					if index == 0 {
						pin = ""
					}
					installDirectory, location := directory, ""
					if installer == "powershell" && index == 1 {
						installDirectory, location = filepath.Base(directory), filepath.Dir(directory)
					}
					output, err := run(t, installDirectory, server.URL, pin, location)
					require.NoError(t, err, "%s", output)
					// #nosec G304 -- installer destination belongs to this test's temporary directory.
					installed, err := os.ReadFile(filepath.Join(directory, binary))
					require.NoError(t, err)
					assert.Equal(t, contents, installed)
					if platform != "windows" {
						info, statErr := os.Stat(filepath.Join(directory, binary))
						require.NoError(t, statErr)
						assert.NotZero(t, info.Mode().Perm()&0o111)
					}
				}
			})

			for _, failure := range []string{"corrupt checksum", "missing checksum", "missing entry", "old release"} {
				t.Run(failure+" preserves installed binary", func(t *testing.T) {
					directory := t.TempDir()
					destination := filepath.Join(directory, binary)
					require.NoError(t, os.WriteFile(destination, []byte("previous binary"), 0o600))
					server := installerRelease(t, platform, "v1.2.3", binary, []byte("new binary"), failure)
					output, err := run(t, directory, server.URL, "", "")
					require.Error(t, err, "%s", output)
					if failure == "old release" {
						assert.Contains(t, string(output), "binary archive")
					} else {
						assert.Contains(t, strings.ToLower(string(output)), "checksum")
					}
					// #nosec G304 -- destination is the synthetic binary created directly above.
					installed, err := os.ReadFile(destination)
					require.NoError(t, err)
					assert.Equal(t, "previous binary", string(installed))
				})
			}
		})
	}
}

func installerRelease(t *testing.T, platform, version, binary string, contents []byte, failure string) *httptest.Server {
	t.Helper()
	var buffer bytes.Buffer
	extension := ".tar.gz"
	if platform == "windows" {
		extension = ".zip"
		archive := zip.NewWriter(&buffer)
		member, err := archive.Create(binary)
		require.NoError(t, err)
		_, err = member.Write(contents)
		require.NoError(t, err)
		require.NoError(t, archive.Close())
	} else {
		compressed := gzip.NewWriter(&buffer)
		archive := tar.NewWriter(compressed)
		require.NoError(t, archive.WriteHeader(&tar.Header{Name: binary, Mode: 0o755, Size: int64(len(contents))}))
		_, err := archive.Write(contents)
		require.NoError(t, err)
		require.NoError(t, archive.Close())
		require.NoError(t, compressed.Close())
	}
	name := fmt.Sprintf("moneyflow_%s_%s_%s%s", strings.TrimPrefix(version, "v"), platform, runtime.GOARCH, extension)
	digest := fmt.Sprintf("%x", sha256.Sum256(buffer.Bytes()))
	if failure == "corrupt checksum" {
		digest = strings.Repeat("0", 64)
	}
	manifest := digest + "  install.sh\n"
	if failure != "missing entry" {
		manifest += digest + "  " + name + "\n"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/tag/"+version, http.StatusFound)
	})
	mux.HandleFunc("/tag/"+version, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/download/"+version+"/"+name, func(response http.ResponseWriter, request *http.Request) {
		if failure == "old release" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(buffer.Bytes())
	})
	mux.HandleFunc("/download/"+version+"/SHA256SUMS", func(response http.ResponseWriter, request *http.Request) {
		if failure == "missing checksum" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(manifest))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
