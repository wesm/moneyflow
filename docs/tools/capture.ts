import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { chromium } from "../../web/node_modules/playwright";
import { startE2EServer } from "../../web/scripts/e2e-server";
import prepareE2EBinaries from "../../web/scripts/e2e-binaries";

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

async function waitForText(text: string) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    const pane = await command([...tmux, "capture-pane", "-p", "-t", "demo"]);
    if (pane.includes(text)) return;
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
  // Exit normally so the application's own deferred demo cleanup runs.
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

const cleanupBinaries = await prepareE2EBinaries();
try {
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
    } finally {
      await browser.close();
    }
  } finally {
    await server.stop();
  }
} finally {
  await cleanupBinaries();
}
