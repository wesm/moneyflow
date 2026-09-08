import { afterAll, beforeAll, expect, test } from "bun:test";
import { resolve } from "node:path";
import { chromium, type Browser } from "../../web/node_modules/playwright";

const root = resolve(import.meta.dir, "../../site");
const server = Bun.serve({
  hostname: "127.0.0.1",
  port: 0,
  fetch(request) {
    let path = new URL(request.url).pathname;
    if (path.endsWith("/")) path += "index.html";
    return new Response(Bun.file(resolve(root, `.${path}`)));
  },
});
let browser: Browser;
beforeAll(async () => {
  browser = await chromium.launch();
});
afterAll(async () => {
  await browser?.close();
  server.stop(true);
});

test("walkthrough captures open at full size and restore keyboard focus", async () => {
  const page = await browser.newPage();
  try {
    await page.goto(new URL("guide/", server.url).href);
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
