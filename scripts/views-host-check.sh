#!/usr/bin/env bash
# Build a sample host against the views package of the committed HEAD, installed from a Git
# archive as a host installs a Moneyflow commit, and set up as the Embedding Moneyflow page tells a
# host to: the page's Vite config, style imports and workspace example, with a strict tsconfig.
# Needs jq, npm and network access to the npm registry and GitHub.
# Usage: bash scripts/views-host-check.sh
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
page=$repo_root/docs/development/embedding.md
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# block LANGUAGE TEXT prints the page's first LANGUAGE code block that contains TEXT.
block() {
    awk -v fence="\`\`\`$1" -v text="$2" '
        $0 == fence { inside = 1; code = ""; next }
        inside && $0 == "```" {
            inside = 0
            if (index(code, text) > 0) { printf "%s", code; found = 1; exit }
            next
        }
        inside { code = code $0 "\n" }
        END { if (!found) { print "embedding.md has no block with " text > "/dev/stderr"; exit 1 } }
    ' "$page"
}

# version NAME prints the version of NAME that moneyflow's web app uses.
version() {
    jq -er --arg name "$1" '.dependencies[$name] // .devDependencies[$name]' \
        "$repo_root/web/package.json"
}

# GitHub's tarball of a commit holds the same files under one top-level directory.
git -C "$repo_root" archive --format=tar.gz --prefix=moneyflow/ -o "$work/moneyflow.tgz" HEAD
host=$work/host
mkdir -p "$host/src"
printf '{ "name": "views-host", "private": true, "type": "module" }\n' >"$host/package.json"
cat >"$host/tsconfig.json" <<'JSON'
{
  "compilerOptions": {
    "exactOptionalPropertyTypes": true,
    "isolatedModules": true,
    "lib": ["ESNext", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "noEmit": true,
    "noImplicitOverride": true,
    "noImplicitReturns": true,
    "noUncheckedIndexedAccess": true,
    "skipLibCheck": true,
    "strict": true,
    "target": "ES2023"
  },
  "include": ["src/**/*.ts", "src/**/*.svelte", "vite.config.ts"]
}
JSON
block ts 'defineConfig(' >"$host/vite.config.ts"
block svelte '<MoneyflowWorkspace' >"$host/src/Workspace.svelte"
{
    block ts "import 'moneyflow-views/styles.css'"
    cat <<'TS'
import { mount } from 'svelte'

import Workspace from './Workspace.svelte'

const target = document.getElementById('app')
if (target === null) {
  throw new Error('The page has no #app element.')
}
mount(Workspace, {
  target,
  props: { profileID: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa', provider: 'monarch', hostFetch: fetch },
})
TS
} >"$host/src/main.ts"
printf '<!doctype html>\n<div id="app"></div>\n<script type="module" src="/src/main.ts"></script>\n' \
    >"$host/index.html"

cd "$host"
# Install as the page says, at the versions moneyflow's web app uses, without install scripts.
npm install --ignore-scripts --no-audit --no-fund \
    "$(version @kenn-io/kit-ui)" "$work/moneyflow.tgz" \
    "svelte@$(version svelte)" "@lucide/svelte@$(version @lucide/svelte)"
npm install --save-dev --ignore-scripts --no-audit --no-fund \
    "vite@$(version vite)" "@sveltejs/vite-plugin-svelte@$(version @sveltejs/vite-plugin-svelte)" \
    "svelte-check@$(version svelte-check)" "typescript@$(version typescript)"
node_modules/.bin/svelte-check --tsconfig ./tsconfig.json --fail-on-warnings --output human
node_modules/.bin/vite build
