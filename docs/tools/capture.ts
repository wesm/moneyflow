import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { chromium } from "../../web/node_modules/playwright";
import { expect } from "../../web/node_modules/@playwright/test";
import { startE2EServer } from "../../web/scripts/e2e-server";

// Build tooling only: both applications use their real --demo startup path.
// No profile path, credentials, provider connection, or fixture override is accepted.
const repository = resolve(import.meta.dir, "../..");
const output = resolve(repository, "docs/.screenshots");
const scratch = await mkdtemp(join(tmpdir(), "mf-shot-"));
const socket = join(scratch, "tmux");
const tmux = ["tmux", "-S", socket, "-f", "/dev/null"];

async function command(argv: string[], input?: string) {
  const process = Bun.spawn(argv, {
    cwd: repository,
    stdin: input === undefined ? "ignore" : new Blob([input]),
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr, code] = await Promise.all([
    new Response(process.stdout).text(),
    new Response(process.stderr).text(),
    process.exited,
  ]);
  if (code !== 0) throw new Error(`${argv[0]} failed: ${stderr}`);
  return stdout;
}

async function waitForText(text: string, occurrences = 1) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    const pane = await command([...tmux, "capture-pane", "-p", "-t", "demo"]);
    if (pane.split(text).length - 1 >= occurrences) return;
    await Bun.sleep(100);
  }
  throw new Error(
    `Demo did not reach ${text}:\n${await command([...tmux, "capture-pane", "-p", "-t", "demo"])}`,
  );
}

async function capture(name: string) {
  const ansi = await command([
    ...tmux,
    "capture-pane",
    "-p",
    "-e",
    "-t",
    "demo",
  ]);
  await command(
    [
      process.env.FREEZE_BIN || "freeze",
      "--language",
      "ansi",
      "--config",
      resolve(import.meta.dir, "freeze.json"),
      "--output",
      join(output, `${name}.svg`),
    ],
    ansi,
  );
  console.log(`Captured ${name}.svg`);
}

try {
  await mkdir(output, { recursive: true });
  await command([
    ...tmux,
    "new-session",
    "-d",
    "-x",
    "120",
    "-y",
    "24",
    "-s",
    "demo",
    "-e",
    "COLORTERM=truecolor",
    "-e",
    `MONEYFLOW_HOME=${join(scratch, "home")}`,
    "-e",
    `TMPDIR=${scratch}`,
    "env",
    "-u",
    "NO_COLOR",
    "TERM=xterm-256color",
    resolve(repository, "bin/moneyflow"),
    "tui",
    "--demo",
  ]);
  await command([...tmux, "set-option", "-g", "status", "off"]);
  await waitForText("Merchant");
  await capture("tui-browse");
  await command([...tmux, "send-keys", "-t", "demo", "Down", "Enter"]);
  await waitForText("Date");
  await capture("tui-detail");
  await command([
    ...tmux,
    "send-keys",
    "-t",
    "demo",
    "Space",
    "Down",
    "Space",
    "c",
  ]);
  await waitForText("Change Category");
  await command([...tmux, "send-keys", "-t", "demo", "-l", "Dining"]);
  // Wait for both the complete input and the selected result, not a partial
  // keystroke frame where Dining merely appears among the choices.
  await waitForText("Dining", 2);
  await capture("tui-category");
  await command([...tmux, "send-keys", "-t", "demo", "Enter"]);
  await waitForText("Pending:");
  await command([...tmux, "send-keys", "-t", "demo", "w"]);
  await waitForText("Enter=Commit");
  await waitForText("Active affected transactions: 2");
  await capture("tui-review");
  await command([...tmux, "send-keys", "-t", "demo", "Escape"]);
  await waitForText("g Group By");
  await command([...tmux, "send-keys", "-t", "demo", "E"]);
  await waitForText("Export Data");
  await capture("tui-export");
  // Exit normally so the application's own deferred demo cleanup runs.
  await command([...tmux, "send-keys", "-t", "demo", "Escape"]);
  await waitForText("g Group By");
  await command([...tmux, "send-keys", "-t", "demo", "q"]);
  await waitForText("Quit moneyflow?");
  await command([...tmux, "send-keys", "-t", "demo", "Enter"]);
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    try {
      await command([...tmux, "has-session", "-t", "demo"]);
    } catch {
      break;
    }
    await Bun.sleep(50);
  }
} finally {
  // The socket belongs exclusively to this invocation, never the user's tmux.
  await command([...tmux, "kill-server"]).catch(() => {});
  await rm(scratch, { recursive: true, force: true });
}

const server = await startE2EServer();
try {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({
      viewport: { width: 1440, height: 900 },
      deviceScaleFactor: 2,
      colorScheme: "dark",
    });
    await page.goto(server.url);
    await page.getByRole("grid", { name: "Financial results" }).waitFor();
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: join(output, "web-browse.png") });
    console.log("Captured web-browse.png");
    const grid = page.getByRole("grid", { name: "Financial results" });
    await grid.focus();
    await page.keyboard.press("ArrowDown");
    await expect(grid).toHaveAttribute("aria-activedescendant", /.+/);
    await page.keyboard.press("Enter");
    await page.getByRole("columnheader", { name: "Date" }).waitFor();
    await page.keyboard.press("Space");
    await expect(page.getByRole("row", { selected: true })).toHaveCount(1);
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Space");
    await expect(page.getByRole("row", { selected: true })).toHaveCount(2);
    await page.keyboard.press("c");
    await page.getByRole("dialog", { name: "Change category" }).waitFor();
    await page.getByRole("combobox", { name: /^Category:/ }).click();
    await page.getByRole("option", { name: "Dining", exact: true }).click();
    await page.getByRole("button", { name: "Save pending change" }).click();
    // The web editor asks for explicit re-invocation after refreshing a bulk
    // selection (the same interaction asserted by the editing browser tests).
    await expect(
      page.getByRole("dialog", { name: "Change category" }),
    ).toContainText("Selection refreshed. Invoke the action again.");
    await page.getByRole("button", { name: "Save pending change" }).click();
    await page
      .getByRole("dialog", { name: "Change category" })
      .waitFor({ state: "hidden", timeout: 5_000 })
      .catch(async () => {
        throw new Error(
          await page
            .getByRole("dialog", { name: "Change category" })
            .innerText(),
        );
      });
    await page.keyboard.press("w");
    await page
      .getByRole("dialog", { name: "Review pending changes" })
      .waitFor();
    await page.screenshot({ path: join(output, "web-review.png") });
    console.log("Captured web-review.png");
  } finally {
    await browser.close();
  }
} finally {
  await server.stop();
}
