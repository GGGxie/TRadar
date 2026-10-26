import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BriefPage } from "./BriefPage";
import type { DailyBrief } from "./api";

const settings = {
  revision: 1,
  isEnabled: true,
  timeZone: "Asia/Shanghai",
  localTime: "08:30",
  maxItems: 5,
};

const brief: DailyBrief = {
  id: "brief-1",
  date: "2026-10-16",
  timeZone: "Asia/Shanghai",
  version: 1,
  cutoffAt: "2026-10-16T00:30:00.000000000Z",
  generatedAt: "2026-10-16T00:31:00.000000000Z",
  previousBriefId: null,
  selectionVersion: "brief-selection-v1",
  rankingVersion: "trend-v1",
  isDemo: false,
  emptyReason: null,
  coverage: [
    { source: "GITHUB", latestCollectedAt: null, state: "NO_DATA" },
    { source: "HACKER_NEWS", latestCollectedAt: null, state: "NO_DATA" },
    { source: "PRODUCT_HUNT", latestCollectedAt: null, state: "NO_DATA" },
  ],
  items: [
    {
      projectId: "p1",
      projectRevision: 1,
      title: "Frozen Signal",
      url: "https://example.com/frozen",
      reasons: ["FIRST_COVERAGE"],
      summary: "帮助开发者追踪有价值的变化",
      factSummary: "创始人自述：已有付费用户",
      hypothesisSummary: null,
      personalDecision: "OBSERVE",
      nextAction: "核对留存",
      evidenceIds: [],
      evidenceSnapshots: [],
      trendSnapshot: {
        projectId: "p1",
        asOf: "2026-10-16T00:30:00.000000000Z",
        algorithmVersion: "trend-v1",
        status: "INSUFFICIENT_HISTORY",
        score: null,
        isPartial: true,
        primarySignal: null,
        growth: {
          status: "INSUFFICIENT_HISTORY",
          from: null,
          to: null,
          durationHours: null,
          delta: null,
          ratePerDay: null,
          growthRatio: null,
          observationIds: [],
        },
        persistence: {
          status: "INSUFFICIENT_HISTORY",
          positiveWindows: null,
          validWindows: 0,
          totalWindows: 7,
        },
        crossSource: { platforms: [], sourceRecordIds: [] },
        explanations: [],
        alert: null,
      },
      analysisId: null,
      researchRevision: 1,
      changeFingerprint: "fingerprint",
    },
  ],
};

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

describe("BriefPage", () => {
  it("reads frozen versions, generates today, and saves versioned settings", async () => {
    const calls: Array<{ path: string; options?: RequestInit }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, options?: RequestInit) => {
        const path = String(input);
        calls.push({ path, options });
        if (path.endsWith("/brief-settings") && options?.method === "PATCH")
          return json({ data: { ...settings, revision: 2, maxItems: 8 } });
        if (path.endsWith("/brief-settings")) return json({ data: settings });
        if (path.includes("/briefs?page="))
          return json({
            data: [brief],
            pagination: { page: 1, pageSize: 20, totalItems: 1, totalPages: 1 },
          });
        if (path.endsWith("/briefs") && options?.method === "POST")
          return json(
            {
              data: {
                id: "run-1",
                status: "PENDING",
                revision: 1,
                date: "2026-10-16",
                timeZone: "Asia/Shanghai",
                cutoffAt: "2026-10-16T00:30:00Z",
                briefId: null,
                errorCode: null,
                createdAt: "2026-10-16T00:30:00Z",
                finishedAt: null,
              },
            },
            202,
          );
        if (path.endsWith("/brief-runs/run-1"))
          return json({
            data: { id: "run-1", status: "SUCCEEDED", briefId: "brief-2" },
          });
        throw new Error(`unexpected request ${path}`);
      }),
    );
    const user = userEvent.setup();
    render(<BriefPage />);
    expect(
      await screen.findByRole("heading", { name: "Frozen Signal" }),
    ).toBeTruthy();
    expect(screen.getByText("创始人自述：已有付费用户")).toBeTruthy();
    expect(screen.getByText("帮助开发者追踪有价值的变化")).toBeTruthy();
    // 个人判断显示中文标签，而不是枚举值。
    expect(screen.getByText("继续观察")).toBeTruthy();
    expect(screen.queryByText("OBSERVE")).toBeNull();
    expect(
      screen.getByRole("link", { name: "下载 Markdown" }).getAttribute("href"),
    ).toContain("/markdown");

    await user.click(screen.getByRole("button", { name: "生成今日简报" }));
    expect(await screen.findByText(/日报已生成/)).toBeTruthy();
    const createCall = calls.find(
      (call) =>
        call.path.endsWith("/briefs") && call.options?.method === "POST",
    );
    expect(
      new Headers(createCall?.options?.headers).get("Idempotency-Key"),
    ).toBeTruthy();
    expect(String(createCall?.options?.body)).toContain(
      '"timeZone":"Asia/Shanghai"',
    );

    const maxItems = screen.getByLabelText("日报最多项目");
    await user.clear(maxItems);
    await user.type(maxItems, "8");
    await user.click(screen.getByRole("button", { name: "保存设置" }));
    await waitFor(() =>
      expect(
        calls.some(
          (call) =>
            call.path.endsWith("/brief-settings") &&
            call.options?.method === "PATCH" &&
            new Headers(call.options.headers).get("If-Match") === '"1"',
        ),
      ).toBe(true),
    );
  });
});
