import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const dataDir = mkdtempSync(join(tmpdir(), "tradar-browser-"));
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  use: {
    baseURL: "http://127.0.0.1:8099",
    browserName: "chromium",
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: "../bin/tradar",
    url: "http://127.0.0.1:8099/healthz",
    reuseExistingServer: false,
    env: {
      TRADAR_ADDR: "127.0.0.1:8099",
      TRADAR_DB_PATH: join(dataDir, "radar.db"),
      TRADAR_WEB_DIR: "dist",
    },
    timeout: 15000,
  },
});
