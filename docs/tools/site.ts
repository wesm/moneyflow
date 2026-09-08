import {
  cp,
  lstat,
  mkdir,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const docs = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repository = resolve(docs, "..");
const work = resolve(docs, ".site-work");
const output = resolve(repository, "site");
const origin = "https://moneyflow.dev";

function contained(root: string, name: string): string {
  const path = resolve(root, name);
  const rel = relative(root, path);
  if (!rel || rel.startsWith("..") || isAbsolute(rel))
    throw new Error(`Invalid manifest path: ${name}`);
  return path;
}

export async function stagePages(
  source: string,
  destination: string,
  paths: string[],
) {
  const sourceRoot = await realpath(source);
  for (const name of paths) {
    const input = contained(source, name);
    contained(sourceRoot, relative(sourceRoot, await realpath(input)));
    const target = contained(destination, name);
    await mkdir(dirname(target), { recursive: true });
    await cp(input, target);
  }
}

export function redirectHTML(destination: string): string {
  if (
    !/^\/[a-zA-Z0-9/_-]+\/$/.test(destination) ||
    destination.startsWith("//")
  ) {
    throw new Error(`Invalid redirect destination: ${destination}`);
  }
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="robots" content="noindex"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Documentation moved · Moneyflow</title><link rel="canonical" href="${origin}${destination}">
<script>location.replace(${JSON.stringify(destination)} + location.search + location.hash)</script>
</head><body><h1>Documentation moved</h1><p>This page describes the legacy Python application.</p>
<p><a href="${destination}">Continue to the archived documentation</a></p></body></html>`;
}

type Page = {
  ids: Set<string>;
  links: string[];
  canonical?: string;
  robots?: string;
};

async function inspectHTML(path: string): Promise<Page> {
  const page: Page = { ids: new Set(), links: [] };
  await new HTMLRewriter()
    .on("*", {
      element(element) {
        const id = element.getAttribute("id");
        if (id) page.ids.add(id);
        for (const attr of ["href", "src"]) {
          const value = element.getAttribute(attr);
          if (value) page.links.push(value);
        }
        if (
          element.tagName === "link" &&
          element.getAttribute("rel") === "canonical"
        ) {
          page.canonical = element.getAttribute("href") ?? undefined;
        }
        if (
          element.tagName === "meta" &&
          element.getAttribute("name") === "robots"
        ) {
          page.robots = element.getAttribute("content") ?? undefined;
        }
      },
    })
    .transform(new Response(Bun.file(path)))
    .text();
  return page;
}

export async function checkSite(root: string): Promise<string[]> {
  const pages = new Map<string, Page>();
  for await (const name of new Bun.Glob("**/*.html").scan(root)) {
    pages.set(name, await inspectHTML(resolve(root, name)));
  }
  if (!pages.size) return ["Site contains no HTML pages"];
  const errors: string[] = [];
  for (const [name, page] of pages) {
    const base = `${origin}/${name.replace(/index\.html$/, "")}`;
    for (const link of page.links) {
      const url = new URL(link, base);
      if (url.origin !== origin) continue;
      let target = decodeURIComponent(url.pathname).slice(1);
      if (!target || target.endsWith("/")) target += "index.html";
      const path = contained(root, target);
      if (!(await Bun.file(path).exists()))
        errors.push(`${name}: missing ${link}`);
      else if (
        url.hash &&
        pages.has(target) &&
        !pages.get(target)!.ids.has(decodeURIComponent(url.hash.slice(1)))
      ) {
        errors.push(`${name}: missing fragment ${link}`);
      }
    }
  }
  return [...new Set(errors)].sort();
}

async function command(argv: string[], cwd = repository) {
  const result = Bun.spawn(argv, { cwd, stdout: "inherit", stderr: "inherit" });
  if ((await result.exited) !== 0)
    throw new Error(`Command failed: ${argv.join(" ")}`);
}

async function cleanOwned(path: string) {
  if (path !== work && path !== output)
    throw new Error("Not an owned site output");
  const info = await lstat(path).catch((error) => {
    if (error.code !== "ENOENT") throw error;
    return null;
  });
  if (info?.isSymbolicLink()) throw new Error("Refusing a symlink site output");
  await rm(path, { recursive: true, force: true });
  await mkdir(path, { recursive: true });
}

type Manifest = {
  current: string[];
  static: string[];
  redirects: Record<string, string>;
};

async function build() {
  const manifest: Manifest = await Bun.file(
    resolve(docs, "site-manifest.json"),
  ).json();
  const archive: { files: { path: string }[] } = await Bun.file(
    resolve(docs, "legacy/v1/manifest.json"),
  ).json();
  await cleanOwned(work);
  for (const [tier, paths, source] of [
    ["current", manifest.current, docs],
    [
      "legacy",
      archive.files.map((file) => file.path),
      resolve(docs, "legacy/v1"),
    ],
  ] as const) {
    const staging = resolve(work, tier, "pages");
    await stagePages(source, staging, paths);
    if (tier === "legacy")
      await stagePages(docs, staging, ["stylesheets/site.css"]);
    if (tier === "current") {
      // Historical design records are source evidence, not current public pages.
      for (const name of paths.filter((name) => name.endsWith(".md"))) {
        const path = resolve(staging, name);
        const text = await readFile(path, "utf8");
        await writeFile(
          path,
          text.replace(
            /(\]\(|\]:\s+)((?:\.\.\/)+superpowers\/[^\s)]+)(\)?)/g,
            (_match, before, link, after) =>
              `${before}https://github.com/wesm/moneyflow/blob/go-port/docs/${link.replace(/^(\.\.\/)+/, "")}${after}`,
          ),
        );
      }
    }
    const config = resolve(
      docs,
      tier === "current" ? "zensical.toml" : "zensical-legacy.toml",
    );
    await command([
      "uv",
      "run",
      "--project",
      docs,
      "--frozen",
      "zensical",
      "build",
      "-f",
      config,
    ]);
  }
  await cleanOwned(output);
  await cp(resolve(work, "current/site"), resolve(output, "docs"), {
    recursive: true,
  });
  await cp(resolve(work, "legacy/site"), resolve(output, "legacy/v1"), {
    recursive: true,
  });
  await stagePages(resolve(docs, "website"), output, manifest.static);
  await command(["make", "docs-screenshots"]);
  await stagePages(
    resolve(docs, ".screenshots"),
    resolve(output, "screenshots"),
    [
      "tui-browse.svg",
      "tui-detail.svg",
      "tui-category.svg",
      "tui-review.svg",
      "tui-export.svg",
      "web-browse.png",
      "web-review.png",
    ],
  );
  await cp(resolve(docs, "CNAME"), resolve(output, "CNAME"));
  await writeFile(resolve(output, ".nojekyll"), "");
  for (const [route, destination] of Object.entries(manifest.redirects)) {
    const target = contained(output, `${route}/index.html`);
    if (await Bun.file(target).exists())
      throw new Error(`Redirect overwrites a page: ${route}`);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, redirectHTML(destination));
  }
  console.log(`Combined site built: ${output}`);
}

async function check() {
  const errors = await checkSite(output);
  const manifest: Manifest = await Bun.file(
    resolve(docs, "site-manifest.json"),
  ).json();
  const redirects = new Set(
    Object.keys(manifest.redirects).map((route) => `${route}/index.html`),
  );
  for await (const name of new Bun.Glob("**/*.html").scan(output)) {
    if (name.endsWith("404.html") || redirects.has(name)) continue;
    const page = await inspectHTML(resolve(output, name));
    const expected = `${origin}/${name.replace(/index\.html$/, "")}`;
    if (page.canonical !== expected)
      errors.push(`${name}: incorrect canonical URL`);
    if (name.startsWith("legacy/v1/") && !page.robots?.includes("noindex")) {
      errors.push(`${name}: archive missing noindex`);
    }
  }
  if (errors.length) throw new Error(errors.join("\n"));
  console.log(
    "Combined-site links, fragments, assets and canonical/robots metadata passed",
  );
}

if (import.meta.main) {
  const action = process.argv[2];
  if (action === "build") await build();
  else if (action === "check") await check();
  else if (action === "serve") {
    await build();
    await check();
    // Serve only assembled public output; no repository or financial profile routes.
    const server = Bun.serve({
      hostname: "127.0.0.1",
      port: 8000,
      async fetch(request) {
        const url = new URL(request.url);
        let name = decodeURIComponent(url.pathname).slice(1);
        if (!name || name.endsWith("/")) name += "index.html";
        try {
          const file = Bun.file(contained(output, name));
          return (await file.exists())
            ? new Response(file)
            : new Response("Not found", { status: 404 });
        } catch {
          return new Response("Not found", { status: 404 });
        }
      },
    });
    console.log(`Preview: ${server.url} (rebuild after editing source)`);
  } else throw new Error("Usage: bun docs/tools/site.ts build|check|serve");
}
