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
import { join } from "node:path";
import { checkSite, redirectHTML, stagePages } from "./site";

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
  const html = redirectHTML("/legacy/v1/guide/ynab/");
  expect(html).toContain('href="/legacy/v1/guide/ynab/"');
  const script = html.match(/<script>(.*?)<\/script>/s)![1];
  let redirected = "";
  new Function("location", script)({
    search: "?source=bookmark",
    hash: "#setup",
    replace: (url: string) => {
      redirected = url;
    },
  });
  expect(redirected).toBe("/legacy/v1/guide/ynab/?source=bookmark#setup");
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
