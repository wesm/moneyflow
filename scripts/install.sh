#!/bin/sh
# Install a moneyflow Go release on Linux or macOS.
# MONEYFLOW_VERSION pins vX.Y.Z or vX.Y.Z-rc.N; otherwise use the latest release.
# MONEYFLOW_RELEASE_BASE_URL overrides the releases root, including /latest and /download.

set -eu

fail() { printf 'ERROR: %s\n' "$1" >&2; exit 1; }

case "$(uname -s)" in
    Linux) platform=linux ;;
    Darwin) platform=darwin ;;
    *) fail "supported operating systems are Linux and macOS" ;;
esac
case "$(uname -m)" in
    x86_64|amd64) architecture=amd64 ;;
    aarch64|arm64) architecture=arm64 ;;
    *) fail "supported architectures are amd64 and arm64" ;;
esac
command -v curl >/dev/null 2>&1 || fail "curl is required"

release_root=${MONEYFLOW_RELEASE_BASE_URL:-https://github.com/wesm/moneyflow/releases}
release_root=${release_root%/}
version=${MONEYFLOW_VERSION:-}
if [ -z "$version" ]; then
    final_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$release_root/latest") ||
        fail "could not resolve the latest release; set MONEYFLOW_VERSION to a release tag"
    case "$final_url" in
        */tag/*) version=${final_url##*/tag/} ;;
        *) fail "latest release did not resolve to a tag" ;;
    esac
fi
printf '%s\n' "$version" | awk '
    NR == 1 && /^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$/ { valid = 1 }
    END { exit !(valid && NR == 1) }
' || fail "MONEYFLOW_VERSION must be vX.Y.Z or vX.Y.Z-rc.N"

filename="moneyflow_${version#v}_${platform}_${architecture}.tar.gz"
download_url="$release_root/download/$version"
download_dir=$(mktemp -d "${TMPDIR:-/tmp}/moneyflow-install.XXXXXX")
staged=
trap 'rm -rf "$download_dir"; if [ -n "$staged" ]; then rm -f "$staged"; fi' EXIT
trap 'exit 1' HUP INT TERM
archive="$download_dir/$filename"
printf 'Installing moneyflow %s for %s/%s...\n' "$version" "$platform" "$architecture"
curl -fsSL "$download_url/$filename" -o "$archive" ||
    fail "could not download binary archive $filename; older Python releases have no binaries, so choose a Go release with MONEYFLOW_VERSION"
curl -fsSL "$download_url/SHA256SUMS" -o "$download_dir/SHA256SUMS" ||
    fail "could not download checksum manifest SHA256SUMS; installation unchanged"
expected=$(awk -v wanted="$filename" '
    NF == 2 { name = $2; sub(/^\*/, "", name); if (name == wanted) { count++; hash = $1 } }
    END { if (count != 1) exit 1; print hash }
' "$download_dir/SHA256SUMS") || fail "checksum manifest must contain exactly one entry for $filename"
case "$expected" in
    *[!0-9a-fA-F]*|'') fail "invalid checksum for $filename" ;;
esac
[ "${#expected}" -eq 64 ] || fail "invalid checksum for $filename"
case "$platform" in
    linux) actual=$(sha256sum "$archive") ;;
    darwin) actual=$(shasum -a 256 "$archive") ;;
esac
actual=${actual%% *}
expected=$(printf '%s' "$expected" | tr 'A-F' 'a-f')
[ "$actual" = "$expected" ] || fail "checksum mismatch for $filename; installation unchanged"

entries=$(tar -tzf "$archive") || fail "could not read binary archive $filename"
[ "$entries" = moneyflow ] || fail "binary archive must contain only moneyflow at its root"
tar -xzf "$archive" -C "$download_dir"
if [ ! -f "$download_dir/moneyflow" ] || [ -L "$download_dir/moneyflow" ]; then
    fail "binary archive must contain a regular moneyflow file"
fi

install_dir=${MONEYFLOW_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
[ ! -d "$install_dir/moneyflow" ] || fail "install destination is a directory: $install_dir/moneyflow"
staged=$(mktemp "$install_dir/.moneyflow-install.XXXXXX")
cp "$download_dir/moneyflow" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$install_dir/moneyflow"
staged=
printf 'Installed %s/moneyflow\n' "$install_dir"
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) printf 'Add %s to PATH, then run moneyflow.\n' "$install_dir" ;;
esac
