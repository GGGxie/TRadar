import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
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
  omittedItemCount: 0,
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
  it("explains which version each regenerated brief is compared against", async () => {
    const first = { ...brief, id: "b1", version: 1, previousBriefId: null };
    const second = {
      ...brief,
      id: "b2",
      version: 2,
      previousBriefId: "b1",
      items: [],
      emptyReason: "NO_NEW_CHANGES" as const,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/brief-settings")) return json({ data: settings });
        if (path.includes("/briefs?page="))
          return json({
            data: [second, first],
            pagination: { page: 1, pageSize: 20, totalItems: 2, totalPages: 1 },
          });
        throw new Error(`unexpected request ${path}`);
      }),
    );
    render(<BriefPage />);
    const latest = (
      await screen.findByRole("heading", { name: "2026-10-16 · 版本 2" })
    ).closest("article")!;
    expect(
      within(latest).getByText(
        "本版只列出版本 1 之后的新变化；版本 1 中的项目仍保留在下方。",
      ),
    ).toBeTruthy();
    const original = screen
      .getByRole("heading", { name: "2026-10-16 · 版本 1" })
      .closest("article")!;
    expect(within(original).getByText(/首份日报/)).toBeTruthy();
  });

  it("shows the cutoff in each brief's own time zone", async () => {
    // #58：不显示 UTC 原始时间戳；按版本自己的时区换算，原值放在 dateTime。
    const pacific = {
      ...brief,
      id: "b-la",
      date: "2026-10-15",
      timeZone: "America/Los_Angeles",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/brief-settings")) return json({ data: settings });
        if (path.includes("/briefs?page="))
          return json({
            data: [brief, pacific],
            pagination: { page: 1, pageSize: 20, totalItems: 2, totalPages: 1 },
          });
        throw new Error(`unexpected request ${path}`);
      }),
    );
    render(<BriefPage />);
    const shanghai = (
      await screen.findByRole("heading", { name: "2026-10-16 · 版本 1" })
    ).closest("article")!;
    expect(shanghai.textContent).toContain("截止 10/16 08:30（Asia/Shanghai）");
    expect(shanghai.querySelector("time")?.getAttribute("datetime")).toBe(
      brief.cutoffAt,
    );
    const la = screen
      .getByRole("heading", { name: "2026-10-15 · 版本 1" })
      .closest("article")!;
    expect(la.textContent).toContain("截止 10/15 17:30（America/Los_Angeles）");
    expect(document.body.textContent).not.toContain("T00:30:00");
  });

  it("says how many qualifying projects the item limit left out", async () => {
    // #59：被最多项目上限截掉的候选要有说明，不列明细。
    const capped = { ...brief, id: "b-cap", omittedItemCount: 3 };
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/brief-settings")) return json({ data: settings });
        if (path.includes("/briefs?page="))
          return json({
            data: [capped],
            pagination: { page: 1, pageSize: 20, totalItems: 1, totalPages: 1 },
          });
        throw new Error(`unexpected request ${path}`);
      }),
    );
    render(<BriefPage />);
    const card = (
      await screen.findByRole("heading", { name: "2026-10-16 · 版本 1" })
    ).closest("article")!;
    expect(card.textContent).toContain(
      "另有 3 个符合条件的项目因上限 1 未收录",
    );
  });

  it("expands only the newest version on the page and lets older ones open", async () => {
    // #60：同页多个版本时只展开第一个，其余折叠为标题行。
    const older = { ...brief, id: "b-old", version: 1, previousBriefId: null };
    const newer = {
      ...brief,
      id: "b-new",
      version: 2,
      previousBriefId: "b-old",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/brief-settings")) return json({ data: settings });
        if (path.includes("/briefs?page="))
          return json({
            data: [newer, older],
            pagination: { page: 1, pageSize: 20, totalItems: 2, totalPages: 1 },
          });
        throw new Error(`unexpected request ${path}`);
      }),
    );
    render(<BriefPage />);
    const first = (
      await screen.findByRole("heading", { name: "2026-10-16 · 版本 2" })
    ).closest("article")!;
    const second = screen
      .getByRole("heading", { name: "2026-10-16 · 版本 1" })
      .closest("article")!;
    expect(
      within(first).getByRole("heading", { name: "Frozen Signal" }),
    ).toBeTruthy();
    expect(
      within(second).queryByRole("heading", { name: "Frozen Signal" }),
    ).toBeNull();
    const toggle = within(second).getByRole("button", { name: /展开本版/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await userEvent.setup().click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(
      within(second).getByRole("heading", { name: "Frozen Signal" }),
    ).toBeTruthy();
  });

  it("downloads Markdown with the access token and the server filename", async () => {
    window.sessionStorage.setItem("tradar.accessToken", "secret-token");
    const fetcher = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/brief-settings")) return json({ data: settings });
      if (path.includes("/briefs?page="))
        return json({
          data: [brief],
          pagination: { page: 1, pageSize: 20, totalItems: 1, totalPages: 1 },
        });
      if (path.endsWith("/markdown"))
        return Promise.resolve(
          new Response("# TRadar 日报", {
            headers: {
              "Content-Type": "text/markdown; charset=utf-8",
              "Content-Disposition":
                'attachment; filename="tradar-brief-2026-10-16-v1.md"',
            },
          }),
        );
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    const createObjectURL = vi.fn(() => "blob:brief");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal(
      "URL",
      Object.assign(URL, { createObjectURL, revokeObjectURL }),
    );
    const clicked: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      clicked.push(`${this.download}|${this.href}`);
    });
    render(<BriefPage />);
    await userEvent.click(
      await screen.findByRole("link", { name: "下载 Markdown" }),
    );
    await waitFor(() =>
      expect(clicked).toEqual(["tradar-brief-2026-10-16-v1.md|blob:brief"]),
    );
    const call = fetcher.mock.calls.find(([input]) =>
      String(input).endsWith("/markdown"),
    ) as unknown as [string, RequestInit];
    expect(new Headers(call[1].headers).get("Authorization")).toBe(
      "Bearer secret-token",
    );
    window.sessionStorage.clear();
  });

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
