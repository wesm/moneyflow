#!/usr/bin/env bash
# Seeds the synthetic `make dev` profile at HOME_DIR if nothing exists there yet.
#
# Usage: scripts/dev-profile.sh HOME_DIR
#
# Seeding happens in a private staging directory next to HOME_DIR, which is renamed to HOME_DIR
# only after seeding succeeds. A failed or interrupted run therefore leaves no partial profile
# that would stop later runs from seeding. An existing HOME_DIR is never changed.
set -euo pipefail

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "usage: scripts/dev-profile.sh HOME_DIR" >&2
  exit 2
fi
home_dir=$1
if [ -e "$home_dir" ]; then
  exit 0
fi

parent=$(dirname "$home_dir")
if [ ! -d "$parent" ]; then
  mkdir -m 700 "$parent"
fi
home_dir="$(cd "$parent" && pwd)/$(basename "$home_dir")"

cd "$(dirname "$0")/.."
staging=$(mktemp -d "$home_dir.seeding.XXXXXX")
trap 'rm -rf -- "$staging"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if ! go run ./internal/tools/seedprofile "$staging"; then
  echo "dev-profile.sh: seeding failed; $home_dir was not created" >&2
  exit 1
fi
if [ -e "$home_dir" ]; then
  echo "dev-profile.sh: $home_dir appeared while seeding; leaving it unchanged" >&2
  exit 1
fi
mv -- "$staging" "$home_dir"
trap - EXIT
echo "Seeded $home_dir"
