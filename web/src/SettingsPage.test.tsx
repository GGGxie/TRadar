import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SettingsPage } from "./SettingsPage";
import type { IngestionRun, SourceView } from "./api";

const now = "2026-10-15T15:00:00.000000000Z";
const sources: SourceView[] = [
  {
    source: "GITHUB",
    config: {
      source: "GITHUB",
      isEnabled: true,
      isScheduleEnabled: false,
      intervalMinutes: 60,
      keywords: ["agent", "developer tools"],
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
    lastObservationAt: null,
    nextScheduledAt: null,
    nextRetryAt: null,
    budget: { remainingRequests: 54, resetAt: now },
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

function json(data: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(data), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("SettingsPage", () => {
  it("shows live source availability, budgets, and credential requirements", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/research-preferences"))
          return json({
            data: {
              revision: 1,
              focusTopics: ["AI Agent", "开发者工具", "SaaS"],
              dailySaveTarget: 3,
              weeklyDeepTarget: 2,
              monthlyPracticeTarget: 1,
            },
          });
        if (path.includes("/sources?"))
          return json({ data: sources, pagination: {} });
        return json({ data: [], pagination: {} });
      }),
    );
    render(<SettingsPage />);
    expect(await screen.findByText("GitHub API 候选发现")).toBeTruthy();
    expect(screen.getByText("剩余 54 次")).toBeTruthy();
    expect(screen.getByText("需要 Product Hunt Token 才能采集")).toBeTruthy();
  });

  it("saves with a revision and queues one manual run", async () => {
    const calls: { path: string; options?: RequestInit }[] = [];
    const queued: IngestionRun = {
      id: "run-1",
      source: "GITHUB",
      trigger: "MANUAL",
      status: "QUEUED",
      createdAt: now,
      startedAt: null,
      finishedAt: null,
      fetchedItems: 0,
      acceptedItems: 0,
      skippedItems: 0,
      failedItems: 0,
      observationsWritten: 0,
      requestsMade: 0,
      isTruncated: false,
      nextRetryAt: null,
      errors: [],
    };
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, options?: RequestInit) => {
        const path = String(input);
        calls.push({ path, options });
        if (path.endsWith("/research-preferences"))
          return json({
            data: {
              revision: 1,
              focusTopics: ["AI Agent", "开发者工具", "SaaS"],
              dailySaveTarget: 3,
              weeklyDeepTarget: 2,
              monthlyPracticeTarget: 1,
            },
          });
        if (path.includes("/sources?") && !options?.method)
          return json({ data: sources, pagination: {} });
        if (path.endsWith("/sources/GITHUB") && options?.method === "PATCH")
          return json({
            data: {
              ...sources[0],
              revision: 2,
              config: { ...sources[0].config, intervalMinutes: 30 },
            },
          });
        if (path.endsWith("/ingestion-runs") && options?.method === "POST")
          return json({ data: queued }, 202);
        return json({ data: [], pagination: {} });
      }),
    );
    const user = userEvent.setup();
    render(<SettingsPage />);
    const interval = await screen.findByLabelText("GitHub 采集间隔（分钟）");
    await user.clear(interval);
    await user.type(interval, "30");
    await user.click(screen.getByRole("button", { name: "保存 GitHub 设置" }));
    await waitFor(() =>
      expect(
        calls.some(
          (call) =>
            call.path.endsWith("/sources/GITHUB") &&
            new Headers(call.options?.headers).get("If-Match") === '"1"',
        ),
      ).toBe(true),
    );
    await user.click(screen.getByRole("button", { name: "立即采集 GitHub" }));
    expect((await screen.findByRole("status")).textContent).toBe(
      "采集任务已进入队列",
    );
    expect(screen.getAllByText("等待执行")).toHaveLength(2);
    const trigger = calls.find(
      (call) =>
        call.path.endsWith("/ingestion-runs") &&
        call.options?.method === "POST",
    );
    expect(
      new Headers(trigger?.options?.headers).get("Idempotency-Key"),
    ).toBeTruthy();
  });

  it("edits quality thresholds for GitHub and Hacker News", async () => {
    const patches: Record<string, unknown> = {};
    const withThresholds = sources.map((source) =>
      source.source === "GITHUB"
        ? { ...source, config: { ...source.config, minStars: 20 } }
        : source.source === "HACKER_NEWS"
          ? { ...source, config: { ...source.config, minPoints: 10 } }
          : source,
    );
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, options?: RequestInit) => {
        const path = String(input);
        if (path.endsWith("/research-preferences"))
          return json({
            data: {
              revision: 1,
              focusTopics: ["AI Agent"],
              dailySaveTarget: 3,
              weeklyDeepTarget: 2,
              monthlyPracticeTarget: 1,
            },
          });
        if (path.includes("/sources?"))
          return json({ data: withThresholds, pagination: {} });
        if (options?.method === "PATCH") {
          const source = path.endsWith("/GITHUB") ? 0 : 1;
          patches[path.split("/").pop()!] = JSON.parse(String(options.body));
          return json({ data: { ...withThresholds[source], revision: 2 } });
        }
        return json({ data: [], pagination: {} });
      }),
    );
    const user = userEvent.setup();
    render(<SettingsPage />);
    const minStars = await screen.findByLabelText("最低 Stars（新候选）");
    expect((minStars as HTMLInputElement).value).toBe("20");
    await user.clear(minStars);
    await user.type(minStars, "100");
    await user.click(screen.getByRole("button", { name: "保存 GitHub 设置" }));
    const minPoints = screen.getByLabelText("最低分数（新条目）");
    expect((minPoints as HTMLInputElement).value).toBe("10");
    await user.clear(minPoints);
    await user.type(minPoints, "25");
    await user.click(
      screen.getByRole("button", { name: "保存 Hacker News 设置" }),
    );
    await waitFor(() => {
      expect(patches.GITHUB).toMatchObject({ minStars: 100 });
      expect(patches.HACKER_NEWS).toMatchObject({ minPoints: 25 });
    });
    expect(
      screen.getAllByText(/已入库或跟踪中的项目不受影响/).length,
    ).toBeGreaterThan(0);
  });
});
