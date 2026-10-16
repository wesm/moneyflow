import { afterEach, expect, test } from "bun:test";
import {
  mkdtemp,
  mkdir,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import {
  checkAgentDocs,
  checkMetadata,
  checkSite,
  llmsText,
  navGaps,
  previewResponse,
  publishMarkdown,
  redirectHTML,
  stagePages,
} from "./site";

const roots: string[] = [];
afterEach(async () => {
  for (const root of roots.splice(0))
    await rm(root, { recursive: true, force: true });
});

test("staging copies only selected public sources and rejects escaping paths", async () => {
  const root = await mkdtemp(join(tmpdir(), "moneyflow-site-"));
  roots.push(root);
  const source = join(root, "source");
  const output = join(root, "output");
  await mkdir(source);
  await writeFile(join(source, "index.md"), "# Public");
  await writeFile(join(source, "private.md"), "# Not selected");
  await stagePages(source, output, ["index.md"]);
  expect(await readdir(output)).toEqual(["index.md"]);
  expect(await readFile(join(output, "index.md"), "utf8")).toBe("# Public");
  await expect(stagePages(source, output, ["../outside.md"])).rejects.toThrow(
    "manifest path",
  );
});

test("static redirects offer a fallback and preserve incoming query and fragment", () => {
  const html = redirectHTML("/docs/guide/ynab/");
  expect(html).toContain('href="/docs/guide/ynab/"');
  const script = html.match(/<script>(.*?)<\/script>/s)![1];
  let redirected = "";
  new Function("location", script)({
    search: "?source=bookmark",
    hash: "#setup",
    replace: (url: string) => {
      redirected = url;
    },
  });
  expect(redirected).toBe("/docs/guide/ynab/?source=bookmark#setup");
  expect(() => redirectHTML("//external.example/")).toThrow("redirect");
});

test("built checker resolves directories, encoded fragments, and same-site absolute links", async () => {
  const root = await mkdtemp(join(tmpdir(), "moneyflow-site-"));
  roots.push(root);
  expect(await checkSite(root)).toEqual(["Site contains no HTML pages"]);
  await mkdir(join(root, "docs"));
  await writeFile(
    join(root, "index.html"),
    '<a href="https://moneyflow.dev/docs/#a%20b">Docs</a>',
  );
  await writeFile(
    join(root, "docs/index.html"),
    '<h1 id="a b">Docs</h1><a href="/">Home</a>',
  );
  expect(await checkSite(root)).toEqual([]);
  await writeFile(
    join(root, "docs/index.html"),
    '<a href="/missing/">Bad</a><a href="/#absent">Bad anchor</a>',
  );
  const errors = await checkSite(root);
  expect(errors.some((error) => error.includes("/missing/"))).toBe(true);
  expect(errors.some((error) => error.includes("#absent"))).toBe(true);
});

async function fixture(files: Record<string, string>): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "moneyflow-site-"));
  roots.push(root);
  for (const [name, content] of Object.entries(files)) {
    await mkdir(dirname(join(root, name)), { recursive: true });
    await writeFile(join(root, name), content);
  }
  return root;
}

const docsPage = (twin: string, alternate = twin, visible = twin) =>
  `<link rel="alternate" type="text/markdown" href="${alternate}">` +
  `<a class="md-markdown-link" href="${visible}">Markdown</a>`;

test("each page's Markdown is published at its source path beside the HTML route", async () => {
  const pages = await fixture({
    "index.md": "# Overview\n\nSee [the web guide](guide/web.md).\n",
    "guide/web.md": "# Web\n\nBack to [overview](../index.md).\n",
    "architecture/index.md": "# Architecture\n\nRead [storage](storage.md).\n",
    "stylesheets/site.css": "body {}",
  });
  const site = await fixture({});
  await publishMarkdown(pages, site, [
    "index.md",
    "guide/web.md",
    "architecture/index.md",
    "stylesheets/site.css",
  ]);
  expect(await readFile(join(site, "guide/web.md"), "utf8")).toBe(
    "# Web\n\nBack to [overview](../index.md).\n",
  );
  expect(await readFile(join(site, "architecture/index.md"), "utf8")).toBe(
    "# Architecture\n\nRead [storage](storage.md).\n",
  );
  expect(await readFile(join(site, "index.md"), "utf8")).toContain(
    "guide/web.md",
  );
  expect(await Bun.file(join(site, "stylesheets/site.css")).exists()).toBe(
    false,
  );
});

test("llms.txt lists every nav section with each page's title and opening summary", async () => {
  const pages = await fixture({
    "index.md":
      "# Moneyflow overview\n\nReview transactions in a\n[terminal](guide/tui.md) or **browser**.\n\nMore.\n",
    "guide/web.md":
      "# Web interface\n\n!!! note\n    Aside.\n\nServe the `web` command.\n",
    "architecture/index.md":
      "# Architecture\n\nOne [service][source-1] owns accounting.\n\n[source-1]: https://example.com\n",
  });
  const text = await llmsText(
    {
      project: {
        site_description: "Local-first transaction review.",
        nav: [
          { Start: [{ Overview: "index.md" }] },
          {
            Interfaces: [
              { Web: "guide/web.md" },
              { Deeper: [{ Architecture: "architecture/index.md" }] },
            ],
          },
        ],
      },
    },
    pages,
  );
  expect(text).toStartWith(
    "# Moneyflow\n\n> Local-first transaction review.\n\n",
  );
  expect(text).toContain(
    "## Start\n\n- [Moneyflow overview](https://moneyflow.dev/docs/index.md): Review transactions in a terminal or browser.\n",
  );
  expect(text).toContain(
    "## Interfaces\n\n- [Web interface](https://moneyflow.dev/docs/guide/web.md): Serve the `web` command.\n" +
      "- [Architecture](https://moneyflow.dev/docs/architecture/index.md): One service owns accounting.\n",
  );
  expect(text).toMatch(/\.md`? twin|Markdown/);
  await expect(
    llmsText(
      {
        project: { site_description: "x", nav: [{ S: [{ Gone: "gone.md" }] }] },
      },
      pages,
    ),
  ).rejects.toThrow("gone.md");
});

test("agent checks require a Markdown twin, matching links and resolvable llms.txt entries", async () => {
  const good = {
    "docs/index.html": docsPage("./index.md"),
    "docs/index.md": "# Overview",
    "docs/guide/web/index.html": docsPage("../../guide/web.md"),
    "docs/guide/web.md": "# Web",
    "docs/architecture/index.html": docsPage("../architecture/index.md"),
    "docs/architecture/index.md": "# Architecture",
    "docs/404.html": "<h1>Not found</h1>",
    "llms.txt":
      "# Moneyflow\n\n- [Web](https://moneyflow.dev/docs/guide/web.md): Web.\n- [Home](https://moneyflow.dev/): Home.\n",
    "index.html": "<h1>Home</h1>",
  };
  expect(await checkAgentDocs(await fixture(good))).toEqual([]);

  const missing = await fixture({ ...good });
  await rm(join(missing, "docs/guide/web.md"));
  expect(await checkAgentDocs(missing)).toContain(
    "docs/guide/web/index.html: missing Markdown twin",
  );

  const errors = await checkAgentDocs(
    await fixture({
      ...good,
      "docs/index.html": "<h1>No links</h1>",
      "docs/guide/web/index.html": docsPage(
        "../../guide/web.md",
        "../../guide/web.md",
        "../../index.md",
      ),
      "llms.txt":
        "# Moneyflow\n\n- [Gone](https://moneyflow.dev/docs/gone.md): No.\n",
    }),
  );
  expect(errors).toEqual([
    "docs/guide/web/index.html: Markdown link does not point to its twin",
    "docs/index.html: missing Markdown alternate link",
    "docs/index.html: missing visible Markdown link",
    "llms.txt: missing https://moneyflow.dev/docs/gone.md",
  ]);
  expect(await checkAgentDocs(await fixture({ "docs/index.md": "#" }))).toEqual(
    ["Site contains no llms.txt"],
  );
});

test("every current Markdown page must appear in the nav", () => {
  const nav = [
    { Start: [{ Overview: "index.md" }] },
    { Deep: [{ Inner: [{ Web: "guide/web.md" }] }] },
  ];
  expect(
    navGaps(["index.md", "guide/web.md", "stylesheets/site.css"], nav),
  ).toEqual([]);
  expect(navGaps(["index.md", "guide/orphan.md"], nav)).toEqual([
    "guide/orphan.md: published but missing from the nav",
  ]);
});

test("metadata checks require canonical URLs outside redirects", async () => {
  const canonical = (url: string) => `<link rel="canonical" href="${url}">`;
  const site = await fixture({
    "index.html": canonical("https://moneyflow.dev/"),
    "docs/index.html": canonical("https://moneyflow.dev/docs/"),
    "old/index.html": "<p>Redirect</p>",
  });
  expect(await checkMetadata(site, new Set(["old/index.html"]))).toEqual([]);
  await writeFile(join(site, "docs/index.html"), canonical("/docs/"));
  expect(await checkMetadata(site, new Set(["old/index.html"]))).toEqual([
    "docs/index.html: incorrect canonical URL",
  ]);
});

test("preview serves Markdown and llms.txt as UTF-8 text", async () => {
  const site = await fixture({
    "llms.txt": "# Moneyflow",
    "docs/guide/web.md": "# Web",
    "docs/guide/web/index.html": "<h1>Web</h1>",
  });
  const get = (path: string) =>
    previewResponse(site, new Request(`http://127.0.0.1${path}`));
  expect((await get("/llms.txt")).headers.get("content-type")).toBe(
    "text/plain; charset=utf-8",
  );
  const markdown = await get("/docs/guide/web.md");
  expect(markdown.headers.get("content-type")).toBe(
    "text/markdown; charset=utf-8",
  );
  expect(await markdown.text()).toBe("# Web");
  expect(await (await get("/docs/guide/web/")).text()).toBe("<h1>Web</h1>");
  expect((await get("/docs/missing.md")).status).toBe(404);
  expect((await get("/../outside")).status).toBe(404);
});
