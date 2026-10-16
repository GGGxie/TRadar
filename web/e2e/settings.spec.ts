import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { SourceView } from "../src/api";

const observedAt = "2026-10-15T05:00:00Z";
const sources: SourceView[] = [
  {
    source: "GITHUB",
    config: {
      source: "GITHUB",
      isEnabled: true,
      isScheduleEnabled: false,
      intervalMinutes: 60,
      keywords: ["agent"],
      discoveryDays: 180,
      candidatesPerKeyword: 10,
      trackedSourceRecordIds: [],
      requestBudgetPerHour: 60,
    },
    revision: 1,
    hasCredentials: false,
    availability: "AVAILABLE",
    lastRunId: null,
    lastSuccessfulAt: null,
    lastObservationAt: observedAt,
    nextScheduledAt: null,
    nextRetryAt: null,
    budget: { remainingRequests: 54, resetAt: "2026-10-15T06:00:00Z" },
  },
  {
    source: "HACKER_NEWS",
    config: {
      source: "HACKER_NEWS",
      isEnabled: true,
      isScheduleEnabled: false,
      intervalMinutes: 60,
      feeds: ["TOP", "NEW", "SHOW"],
      itemsPerFeed: 20,
      trackedSourceRecordIds: [],
    },
    revision: 1,
    hasCredentials: false,
    availability: "AVAILABLE",
    lastRunId: null,
    lastSuccessfulAt: null,
    lastObservationAt: null,
    nextScheduledAt: null,
    nextRetryAt: null,
    budget: { remainingRequests: null, resetAt: null },
  },
  {
    source: "PRODUCT_HUNT",
    config: {
      source: "PRODUCT_HUNT",
      isEnabled: false,
      isScheduleEnabled: false,
      intervalMinutes: 60,
      discoveryDays: 7,
      maxPages: 3,
    },
    revision: 1,
    hasCredentials: false,
    availability: "DISABLED",
    lastRunId: null,
    lastSuccessfulAt: null,
    lastObservationAt: null,
    nextScheduledAt: null,
    nextRetryAt: null,
    budget: { remainingRequests: null, resetAt: null },
  },
];

test("source settings explain availability and safely queue a collection", async ({
  page,
}) => {
  let github = structuredClone(sources[0]);
  let patchHeaders: Record<string, string> = {};
  let patchBody: Record<string, unknown> = {};

  await page.route("**/api/v1/sources?page=1&pageSize=20", async (route) => {
    await route.fulfill({
      json: { data: [github, sources[1], sources[2]], pagination: {} },
    });
  });
  await page.route("**/api/v1/sources/GITHUB", async (route) => {
    patchHeaders = await route.request().allHeaders();
    patchBody = route.request().postDataJSON();
    github = {
      ...github,
      revision: 2,
      config: { ...github.config, intervalMinutes: 30 },
    };
    await route.fulfill({ json: { data: github } });
  });
  await page.route(
    "**/api/v1/ingestion-runs?page=1&pageSize=20",
    async (route) => {
      await route.fulfill({ json: { data: [], pagination: {} } });
    },
  );
  await page.route("**/api/v1/ingestion-runs", async (route) => {
    await route.fulfill({
      status: 202,
      json: {
        data: {
          id: "run-browser",
          source: "GITHUB",
          trigger: "MANUAL",
          status: "QUEUED",
          createdAt: observedAt,
          startedAt: null,
          finishedAt: null,
          fetchedItems: 0,
          acceptedItems: 0,
          failedItems: 0,
          observationsWritten: 0,
          requestsMade: 0,
          isTruncated: false,
          nextRetryAt: null,
          errors: [],
        },
      },
    });
  });

  await page.goto("/settings");
  await expect(
    page.getByRole("heading", { name: "信息源与采集" }),
  ).toBeVisible();
  await expect(page.getByLabel("每日收藏建议")).toHaveValue("3");
  await page.getByLabel("每日收藏建议").fill("0");
  await page.getByRole("button", { name: "保存研究偏好" }).click();
  await expect(page.getByText("研究偏好已保存")).toBeVisible();
  await expect(page.getByText("剩余 54 次")).toBeVisible();
  await expect(
    page.getByText("需要 Product Hunt Token 才能采集"),
  ).toBeVisible();

  const interval = page.getByLabel("GitHub 采集间隔（分钟）");
  await interval.fill("30");
  await expect(
    page.getByRole("button", { name: "请先保存设置" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "保存 GitHub 设置" }).click();
  await expect(page.getByText("设置已保存")).toBeVisible();
  expect(patchHeaders["if-match"]).toBe('"1"');
  expect(patchBody.intervalMinutes).toBe(30);

  await page.getByRole("button", { name: "立即采集 GitHub" }).click();
  await expect(page.getByText("采集任务已进入队列")).toBeVisible();
  await expect(page.getByText("等待执行").first()).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});
