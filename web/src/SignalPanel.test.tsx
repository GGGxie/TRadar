import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SignalPanel } from "./SignalPanel";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const trend = {
  projectId: "p1",
  asOf: "2026-10-23T02:30:00.000000000Z",
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
  crossSource: { platforms: ["GITHUB"], sourceRecordIds: ["gh"] },
  attention: {
    index: 1.3,
    metrics: [
      {
        sourceRecordId: "gh",
        source: "GITHUB",
        metric: "STARS",
        value: 173610,
        observedAt: "2026-10-23T02:21:43.000000000Z",
        observationId: "o1",
      },
      {
        sourceRecordId: "gh",
        source: "GITHUB",
        metric: "FORKS",
        value: 820,
        observedAt: "2026-10-23T02:21:43.000000000Z",
        observationId: "o2",
      },
    ],
  },
  explanations: ["尚无新鲜且窗口完整的累计观测，暂不评分。"],
  alert: null,
};

const source = {
  id: "gh",
  projectId: "p1",
  source: "GITHUB",
  externalId: "1",
  recordType: "PROJECT",
  url: "https://api.github.com/repositories/1",
  targetUrl: "https://github.com/DietrichGebert/ponytail",
  title: "DietrichGebert/ponytail",
  excerpt: "Makes your AI agent think like the laziest senior dev.",
  publishedAt: "2026-03-26T06:52:18.000000000Z",
  fetchedAt: "2026-10-23T02:21:43.000000000Z",
  revision: 1,
};

function stubApi(sources: unknown[]) {
  const fetcher = vi.fn(async (path: string) => {
    if (path.includes("/trend"))
      return new Response(JSON.stringify({ data: trend }));
    return new Response(
      JSON.stringify({
        data: sources,
        pagination: {
          page: 1,
          pageSize: 100,
          totalItems: sources.length,
          totalPages: 1,
        },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("SignalPanel", () => {
  it("shows source records, latest observations and why the trend is unscored", async () => {
    const fetcher = stubApi([source]);
    render(<SignalPanel projectId="p1" />);
    const panel = (
      await screen.findByRole("heading", { name: "来源与当前关注" })
    ).closest("section")!;
    await within(panel).findByText("Stars");
    expect(within(panel).getByText("173,610")).toBeTruthy();
    expect(within(panel).getByText("820")).toBeTruthy();
    expect(
      within(panel).getByText("尚无新鲜且窗口完整的累计观测，暂不评分。"),
    ).toBeTruthy();
    expect(within(panel).getByText("增速待积累")).toBeTruthy();
    const link = within(panel).getByRole("link", {
      name: /DietrichGebert\/ponytail/,
    });
    expect(link.getAttribute("href")).toBe(
      "https://github.com/DietrichGebert/ponytail",
    );
    expect(
      within(panel).getByText(
        "Makes your AI agent think like the laziest senior dev.",
      ),
    ).toBeTruthy();
    const paths = fetcher.mock.calls.map(([path]) => String(path));
    expect(paths.some((path) => path.includes("/projects/p1/trend"))).toBe(
      true,
    );
    expect(paths.some((path) => path.includes("/projects/p1/sources"))).toBe(
      true,
    );
  });

  it("explains that manually recorded projects have no source observations", async () => {
    stubApi([]);
    render(<SignalPanel projectId="p1" />);
    expect(
      await screen.findByText(
        "这个项目还没有自动来源。可在信息源设置中采集，或在下方记录证据。",
      ),
    ).toBeTruthy();
  });
});
