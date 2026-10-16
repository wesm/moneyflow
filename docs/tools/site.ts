import { cp, lstat, mkdir, realpath, rm, writeFile } from "node:fs/promises";
import { dirname, extname, isAbsolute, relative, resolve } from "node:path";
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
<title>Page moved · Moneyflow</title><link rel="canonical" href="${origin}${destination}">
<script>location.replace(${JSON.stringify(destination)} + location.search + location.hash)</script>
</head><body><h1>Page moved</h1><p><a href="${destination}">Continue to the documentation</a></p>
</body></html>`;
}

type Page = {
  ids: Set<string>;
  links: string[];
  canonical?: string;
  robots?: string;
  markdown?: string;
  markdownLink?: string;
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
        if (
          element.tagName === "link" &&
          element.getAttribute("rel") === "alternate" &&
          element.getAttribute("type") === "text/markdown"
        ) {
          page.markdown = element.getAttribute("href") ?? undefined;
        }
        if (
          element.tagName === "a" &&
          element
            .getAttribute("class")
            ?.split(/\s+/)
            .includes("md-markdown-link")
        ) {
          page.markdownLink = element.getAttribute("href") ?? undefined;
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
      const path = resolveLink(root, base, link);
      if (!path) continue;
      const url = new URL(link, base);
      const target = relative(root, path);
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

// A page's Markdown twin keeps its source path, so relative `.md` links in the
// copied file resolve exactly as they do in the repository.
export async function publishMarkdown(
  pages: string,
  destination: string,
  paths: string[],
) {
  await stagePages(
    pages,
    destination,
    paths.filter((path) => path.endsWith(".md")),
  );
}

type Nav = (string | Record<string, string | Nav>)[];
type ZensicalConfig = {
  project: { site_description: string; nav: Nav };
};

function navEntries(nav: Nav, section = ""): [string, string][] {
  return nav.flatMap((item) =>
    typeof item === "string"
      ? [[section, item] as [string, string]]
      : Object.entries(item).flatMap(([title, value]) =>
          typeof value === "string"
            ? [[section || title, value] as [string, string]]
            : navEntries(value, section || title),
        ),
  );
}

export function navGaps(current: string[], nav: Nav): string[] {
  const listed = new Set(navEntries(nav).map(([, path]) => path));
  return current
    .filter((path) => path.endsWith(".md") && !listed.has(path))
    .map((path) => `${path}: published but missing from the nav`);
}

function pageSummary(path: string, markdown: string) {
  const blocks = markdown.split(/\n\s*\n/).map((block) => block.trim());
  const heading = blocks.findIndex((block) => /^# \S/.test(block));
  const paragraph = blocks
    .slice(heading + 1)
    .find((block) => /^[A-Za-z0-9*_`[]/.test(block) && !/^[-*] /.test(block));
  if (heading < 0 || !paragraph)
    throw new Error(`${path}: needs an H1 and an opening paragraph`);
  return {
    title: blocks[heading].split("\n")[0].slice(2).trim(),
    description: paragraph
      .replace(/\s*\n\s*/g, " ")
      .replace(/\[([^\]]+)\](?:\([^)]*\)|\[[^\]]*\])/g, "$1")
      .replace(/\*\*([^*]+)\*\*/g, "$1"),
  };
}

export async function llmsText(
  config: ZensicalConfig,
  pages: string,
): Promise<string> {
  const sections = new Map<string, string[]>();
  for (const [section, path] of navEntries(config.project.nav)) {
    const file = Bun.file(contained(pages, path));
    if (!(await file.exists()))
      throw new Error(`Nav page is not published: ${path}`);
    const { title, description } = pageSummary(path, await file.text());
    if (!sections.has(section)) sections.set(section, []);
    sections
      .get(section)!
      .push(`- [${title}](${origin}/docs/${path}): ${description}`);
  }
  return (
    [
      "# Moneyflow",
      `> ${config.project.site_description}`,
      `Every documentation page has a Markdown twin at its source path. ` +
        `${origin}/docs/guide/web/ is ${origin}/docs/guide/web.md, ` +
        `${origin}/docs/architecture/ is ${origin}/docs/architecture/index.md, ` +
        `and ${origin}/docs/ is ${origin}/docs/index.md. ` +
        "The links below point to the Markdown. Relative links inside each file resolve " +
        "against its own URL. For the HTML page, drop `.md` (or `index.md`) and end the path with `/`.",
      ...[...sections].map(
        ([section, entries]) => `## ${section}\n\n${entries.join("\n")}`,
      ),
    ].join("\n\n") + "\n"
  );
}

function resolveLink(root: string, base: string, link: string) {
  const url = new URL(link, base);
  if (url.origin !== origin) return undefined;
  let target = decodeURIComponent(url.pathname).slice(1);
  if (!target || target.endsWith("/")) target += "index.html";
  return contained(root, target);
}

export async function checkAgentDocs(root: string): Promise<string[]> {
  const errors: string[] = [];
  for await (const name of new Bun.Glob("docs/**/*.html").scan(root)) {
    if (name === "docs/404.html") continue;
    const route = name.replace(/index\.html$/, "");
    const twins = [`${route}index.md`, `${route.replace(/\/$/, "")}.md`].map(
      (twin) => resolve(root, twin),
    );
    const twin = (
      await Promise.all(
        twins.map(async (path) =>
          (await Bun.file(path).exists()) ? path : undefined,
        ),
      )
    ).find(Boolean);
    if (!twin) {
      errors.push(`${name}: missing Markdown twin`);
      continue;
    }
    const page = await inspectHTML(resolve(root, name));
    const base = `${origin}/${route}`;
    if (!page.markdown) errors.push(`${name}: missing Markdown alternate link`);
    if (!page.markdownLink)
      errors.push(`${name}: missing visible Markdown link`);
    for (const link of [page.markdown, page.markdownLink]) {
      if (link && resolveLink(root, base, link) !== twin)
        errors.push(`${name}: Markdown link does not point to its twin`);
    }
  }
  const llms = Bun.file(resolve(root, "llms.txt"));
  if (!(await llms.exists())) return [...errors, "Site contains no llms.txt"];
  for (const [, link] of (await llms.text()).matchAll(/\]\(([^)\s]+)\)/g)) {
    const path = resolveLink(root, `${origin}/`, link);
    if (!path || !(await Bun.file(path).exists()))
      errors.push(`llms.txt: missing ${link}`);
  }
  return [...new Set(errors)].sort();
}

export async function checkMetadata(
  root: string,
  redirects: Set<string>,
): Promise<string[]> {
  const errors: string[] = [];
  for await (const name of new Bun.Glob("**/*.html").scan(root)) {
    if (name.endsWith("404.html") || redirects.has(name)) continue;
    const page = await inspectHTML(resolve(root, name));
    const expected = `${origin}/${name.replace(/index\.html$/, "")}`;
    if (page.canonical !== expected)
      errors.push(`${name}: incorrect canonical URL`);
  }
  return errors.sort();
}

const textTypes: Record<string, string> = {
  ".md": "text/markdown; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
};

// Serve only assembled public output; no repository or financial profile routes.
export async function previewResponse(
  root: string,
  request: Request,
): Promise<Response> {
  let name = decodeURIComponent(new URL(request.url).pathname).slice(1);
  if (!name || name.endsWith("/")) name += "index.html";
  try {
    const file = Bun.file(contained(root, name));
    if (!(await file.exists())) throw new Error("missing");
    const type = textTypes[extname(name)];
    return new Response(
      file,
      type ? { headers: { "content-type": type } } : {},
    );
  } catch {
    return new Response("Not found", { status: 404 });
  }
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

async function zensicalConfig(): Promise<ZensicalConfig> {
  return Bun.TOML.parse(
    await Bun.file(resolve(docs, "zensical.toml")).text(),
  ) as ZensicalConfig;
}

async function build() {
  const manifest: Manifest = await Bun.file(
    resolve(docs, "site-manifest.json"),
  ).json();
  await cleanOwned(work);
  await stagePages(docs, resolve(work, "current/pages"), manifest.current);
  await command([
    "uv",
    "run",
    "--project",
    docs,
    "--frozen",
    "zensical",
    "build",
    "-f",
    resolve(docs, "zensical.toml"),
  ]);
  await cleanOwned(output);
  await cp(resolve(work, "current/site"), resolve(output, "docs"), {
    recursive: true,
  });
  const pages = resolve(work, "current/pages");
  await publishMarkdown(pages, resolve(output, "docs"), manifest.current);
  await writeFile(
    resolve(output, "llms.txt"),
    await llmsText(await zensicalConfig(), pages),
  );
  await stagePages(resolve(docs, "website"), output, manifest.static);
  await command(["make", "docs-screenshots"]);
  await stagePages(
    resolve(docs, ".screenshots"),
    resolve(output, "screenshots"),
    ["tui-browse.svg", "web-browse.png"],
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
  const manifest: Manifest = await Bun.file(
    resolve(docs, "site-manifest.json"),
  ).json();
  const redirects = new Set(
    Object.keys(manifest.redirects).map((route) => `${route}/index.html`),
  );
  const errors = [
    ...(await checkSite(output)),
    ...(await checkMetadata(output, redirects)),
    ...(await checkAgentDocs(output)),
    ...navGaps(manifest.current, (await zensicalConfig()).project.nav),
  ];
  if (errors.length) throw new Error(errors.join("\n"));
  console.log(
    "Combined-site links, fragments, assets, canonical/robots metadata, Markdown twins, llms.txt and nav coverage passed",
  );
}

if (import.meta.main) {
  const action = process.argv[2];
  if (action === "build") await build();
  else if (action === "check") await check();
  else if (action === "serve") {
    await build();
    await check();
    const server = Bun.serve({
      hostname: "127.0.0.1",
      port: 8000,
      fetch: (request) => previewResponse(output, request),
    });
    console.log(`Preview: ${server.url} (rebuild after editing source)`);
  } else throw new Error("Usage: bun docs/tools/site.ts build|check|serve");
}
