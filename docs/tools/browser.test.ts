import { afterAll, beforeAll, expect, test } from "bun:test";
import { resolve } from "node:path";
import { chromium, type Browser } from "../../web/node_modules/playwright";
import { previewResponse } from "./site";

const root = resolve(import.meta.dir, "../../site");
const server = Bun.serve({
  hostname: "127.0.0.1",
  port: 0,
  fetch: (request) => previewResponse(root, request),
});
let browser: Browser;
beforeAll(async () => {
  browser = await chromium.launch();
});
afterAll(async () => {
  await browser?.close();
  server.stop(true);
});

test("homepage captures open at full size and restore keyboard focus", async () => {
  const page = await browser.newPage();
  try {
    await page.goto(server.url.href);
    const capture = page.locator("a[data-lightbox]").first();
    expect(await capture.count()).toBe(1);
    await capture.focus();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Application screenshot" });
    await dialog.waitFor();
    expect(await dialog.locator("img").getAttribute("src")).toBe(
      await capture.getAttribute("href"),
    );
    expect(
      await dialog.locator("img").evaluate(async (image: HTMLImageElement) => {
        await image.decode();
        return image.naturalWidth > 0;
      }),
    ).toBe(true);
    expect(
      await page
        .getByRole("button", { name: "Close screenshot" })
        .evaluate((button) => button === document.activeElement),
    ).toBe(true);
    await page.keyboard.press("Escape");
    expect(await dialog.isVisible()).toBe(false);
    expect(
      await capture.evaluate((link) => link === document.activeElement),
    ).toBe(true);
    await capture.click();
    await page.getByRole("button", { name: "Close screenshot" }).click();
    expect(await dialog.isVisible()).toBe(false);
    const terminal = page.locator('a[data-lightbox][href$=".svg"]').first();
    await terminal.click();
    expect(await dialog.locator("img").getAttribute("src")).toBe(
      await terminal.getAttribute("href"),
    );
    await dialog
      .locator("img")
      .evaluate((image: HTMLImageElement) => image.decode());
    await page.keyboard.press("Escape");
    expect(
      await terminal.evaluate((link) => link === document.activeElement),
    ).toBe(true);
  } finally {
    await page.close();
  }
});

test("a docs page's Markdown link opens its source as text", async () => {
  const page = await browser.newPage();
  try {
    await page.goto(new URL("docs/guide/web/", server.url).href);
    await page.getByRole("link", { name: "View as Markdown" }).click();
    await page.waitForURL(/\/docs\/guide\/web\.md$/);
    expect((await page.locator("body").innerText()).trimEnd()).toBe(
      (await Bun.file(resolve(root, "docs/guide/web.md")).text()).trimEnd(),
    );
  } finally {
    await page.close();
  }
});

test("homepage and docs fit a phone screen in both color schemes", async () => {
  for (const colorScheme of ["light", "dark"] as const) {
    const page = await browser.newPage({
      viewport: { width: 390, height: 844 },
      colorScheme,
    });
    try {
      for (const path of ["", "docs/"]) {
        await page.goto(new URL(path, server.url).href);
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth - window.innerWidth,
          ),
        ).toBeLessThanOrEqual(0);
      }
    } finally {
      await page.close();
    }
  }
});
