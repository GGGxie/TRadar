import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectList } from "./ProjectList";
import type { ProjectTrend } from "./api";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function radarItem(
  id: string,
  name: string,
  attention: ProjectTrend["trend"]["attention"],
  platforms: ProjectTrend["trend"]["crossSource"]["platforms"],
): ProjectTrend {
  return {
    project: {
      id,
      name,
      canonicalUrl: `https://github.com/acme/${id}`,
      summary: `${name} 的介绍`,
      category: "OTHER",
      firstSeenAt: "2026-10-23T02:21:43.000000000Z",
      lastSeenAt: "2026-10-23T02:21:43.000000000Z",
      createdAt: "2026-10-23T02:21:43.000000000Z",
      updatedAt: "2026-10-23T02:21:43.000000000Z",
      revision: 1,
    },
    trend: {
      projectId: id,
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
      crossSource: { platforms, sourceRecordIds: [] },
      attention,
      explanations: ["尚无新鲜且窗口完整的累计观测，暂不评分。"],
      alert: null,
    },
  };
}

describe("ProjectList", () => {
  it("shows per-platform current attention separately from growth", async () => {
    const items = [
      radarItem(
        "ponytail",
        "DietrichGebert/ponytail",
        {
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
              sourceRecordId: "hn",
              source: "HACKER_NEWS",
              metric: "POINTS",
              value: 412,
              observedAt: "2026-10-23T02:21:53.000000000Z",
              observationId: "o2",
            },
          ],
        },
        ["GITHUB", "HACKER_NEWS"],
      ),
      radarItem("manual", "人工项目", { index: null, metrics: [] }, []),
    ];
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: items,
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 2,
                totalPages: 1,
              },
            }),
          ),
      ),
    );
    render(<ProjectList />);
    const popular = (
      await screen.findByText("DietrichGebert/ponytail")
    ).closest("a")!;
    expect(within(popular).getByText("GitHub ★ 17.4 万")).toBeTruthy();
    expect(within(popular).getByText("HN 412 分")).toBeTruthy();
    expect(within(popular).getByText("增速待积累")).toBeTruthy();
    const manual = screen.getByText("人工项目").closest("a")!;
    expect(within(manual).getByText("暂无来源观测")).toBeTruthy();
    expect(screen.getByText("当前关注 / 趋势")).toBeTruthy();
    // 本页全部未评分：说明增速如何产生，并指向信息源设置。
    const hint = screen.getByRole("note");
    expect(hint.textContent).toContain(
      "增速需要同一项目相隔 24–26 小时的两次观测",
    );
    expect(
      within(hint)
        .getByRole("link", { name: "开启定时采集" })
        .getAttribute("href"),
    ).toBe("/settings");
  });

  it("restores filters from the address and writes changes back", async () => {
    window.history.replaceState(
      null,
      "",
      "/radar?q=agent&category=AI_AGENT&window=7d&page=2",
    );
    const fetcher = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            data: [],
            pagination: { page: 2, pageSize: 20, totalItems: 0, totalPages: 0 },
          }),
        ),
    );
    vi.stubGlobal("fetch", fetcher);
    render(<ProjectList />);
    await screen.findByText("没有匹配的项目");
    const firstPath = String((fetcher.mock.calls[0] as unknown[])[0]);
    const params = new URL(firstPath, "http://local").searchParams;
    expect(params.get("q")).toBe("agent");
    expect(params.get("category")).toBe("AI_AGENT");
    expect(params.get("window")).toBe("7d");
    expect(params.get("page")).toBe("2");
    expect((screen.getByLabelText("搜索项目") as HTMLInputElement).value).toBe(
      "agent",
    );
    await userEvent.selectOptions(screen.getByLabelText("筛选来源"), "GITHUB");
    const address = new URLSearchParams(window.location.search);
    expect(address.get("source")).toBe("GITHUB");
    expect(address.get("q")).toBe("agent");
    expect(address.get("page")).toBeNull();
    window.history.replaceState(null, "", "/");
  });

  it("labels the trend score in scored rows", async () => {
    const item = radarItem("scored", "acme/scored", { index: 1, metrics: [] }, [
      "GITHUB",
    ]);
    item.trend.score = 50;
    item.trend.status = "READY";
    item.trend.growth.delta = 7476;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [item],
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 1,
                totalPages: 1,
              },
            }),
          ),
      ),
    );
    render(<ProjectList />);
    const row = (await screen.findByText("acme/scored")).closest("a")!;
    expect(within(row).getByText("趋势分 50.0")).toBeTruthy();
    expect(within(row).getByText("24 小时净增 +7476")).toBeTruthy();
  });

  it("guides an empty workspace to collect from sources first", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [],
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 0,
                totalPages: 0,
              },
            }),
          ),
      ),
    );
    render(<ProjectList />);
    expect(
      await screen.findByText("先采集一次，看看最近受关注的项目"),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "前往信息源设置" }).getAttribute("href"),
    ).toBe("/settings");
    expect(screen.getByRole("button", { name: "手动记录项目" })).toBeTruthy();
  });

  it("shows this week's source stars next to the cumulative total", async () => {
    const item = radarItem(
      "ponytail",
      "DietrichGebert/ponytail",
      {
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
        ],
        sourceWeeks: [
          {
            sourceRecordId: "gh",
            source: "GITHUB",
            metric: "STARS",
            weekStart: "2026-10-18T00:00:00.000000000Z",
            timeZone: "UNSPECIFIED",
            days: [890, 1129, 1939, 1933, 1552, 0, 0],
            total: 7443,
            observedAt: "2026-10-23T02:21:43.000000000Z",
          },
        ],
      },
      ["GITHUB"],
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [item],
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 1,
                totalPages: 1,
              },
            }),
          ),
      ),
    );
    render(<ProjectList />);
    const row = (await screen.findByText("DietrichGebert/ponytail")).closest(
      "a",
    )!;
    expect(within(row).getByText("来源本周 +7443")).toBeTruthy();
  });
});
