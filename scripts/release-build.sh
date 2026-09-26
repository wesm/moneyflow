#!/usr/bin/env bash
# Build Go release archives locally or in CI. This never tags, pushes, or publishes.
# Usage: bash scripts/release-build.sh 1.2.3[-rc.1] [new-output-directory]
set -euo pipefail

version=${1:-}
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
    echo 'version must be X.Y.Z or X.Y.Z-rc.N (without v)' >&2
    exit 2
fi
output=${2:-dist/release}
if [[ -e "$output" ]]; then
    echo 'output directory must not exist; choose a fresh directory' >&2
    exit 2
fi

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$output"
output=$(cd "$output" && pwd)
cd "$repo_root"
test -f internal/web/dist/.moneyflow-production.json || {
    echo 'Build embedded assets first: make web-install web-embed' >&2
    exit 1
}
commit=$(git rev-parse --short=7 HEAD)
build_date=$(git show -s --format=%cI HEAD)
ldflags="-s -w -X github.com/wesm/moneyflow/internal/version.Version=v${version}"
ldflags+=" -X github.com/wesm/moneyflow/internal/version.Commit=${commit}"
ldflags+=" -X github.com/wesm/moneyflow/internal/version.BuildDate=${build_date}"

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
archives=()
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    os=${target%/*}
    arch=${target#*/}
    binary=moneyflow
    if [[ "$os" == windows ]]; then binary=moneyflow.exe; fi
    printf 'Building %s\n' "$target"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
        -ldflags "$ldflags" -o "$scratch/$binary" ./cmd/moneyflow
    name="moneyflow_${version}_${os}_${arch}"
    if [[ "$os" == windows ]]; then
        name+=".zip"
        (cd "$scratch" && zip -q "$output/$name" "$binary")
        [[ "$(unzip -Z1 "$output/$name")" == "$binary" ]]
    else
        name+=".tar.gz"
        tar -czf "$output/$name" -C "$scratch" "$binary"
        [[ "$(tar -tzf "$output/$name")" == "$binary" ]]
    fi
    archives+=("$name")
done
cp scripts/install.sh scripts/install.ps1 "$output/"
(
    cd "$output"
    sha256sum "${archives[@]}" install.sh install.ps1 > SHA256SUMS
    sha256sum -c SHA256SUMS
)
printf 'Release assets: %s\n' "$output"
