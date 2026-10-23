import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LibraryPage } from "./LibraryPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const entry = (id: string, nextActionAt: string | null) => ({
  id,
  projectId: `p-${id}`,
  isStarred: true,
  tags: [],
  stage: "SCREENING",
  decision: "OBSERVE",
  rationale: "",
  notes: "",
  nextAction: id === "due" ? "复查定价" : "",
  nextActionAt,
  revision: 1,
  createdAt: "2026-10-20T01:00:00.000000000Z",
  updatedAt: "2026-10-20T01:00:00.000000000Z",
  starredAt: "2026-10-20T01:00:00.000000000Z",
});

describe("LibraryPage rhythm", () => {
  it("shows rhythm progress against suggestions and flags overdue next steps", async () => {
    const past = new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString();
    const future = new Date(Date.now() + 2 * 24 * 3600 * 1000).toISOString();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.includes("/research-rhythm"))
          return new Response(
            JSON.stringify({
              data: {
                timeZone: "Asia/Shanghai",
                dayStart: "",
                weekStart: "",
                monthStart: "",
                dailySaved: 4,
                weeklyDeep: 1,
                monthlyPractice: 0,
                dailySaveTarget: 3,
                weeklyDeepTarget: 2,
                monthlyPracticeTarget: 0,
              },
            }),
          );
        if (path.includes("/research?"))
          return new Response(
            JSON.stringify({
              data: [entry("due", past), entry("later", future)],
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 2,
                totalPages: 1,
              },
            }),
          );
        const id = path.split("/projects/")[1];
        return new Response(
          JSON.stringify({
            data: {
              id,
              name: `项目 ${id}`,
              canonicalUrl: "https://example.com",
              summary: "",
              category: "OTHER",
              firstSeenAt: "2026-10-20T00:00:00Z",
              lastSeenAt: "2026-10-20T00:00:00Z",
              createdAt: "2026-10-20T00:00:00Z",
              updatedAt: "2026-10-20T00:00:00Z",
              revision: 1,
            },
          }),
        );
      }),
    );
    render(<LibraryPage />);
    const rhythm = await screen.findByRole("region", { name: "研究节奏" });
    expect(within(rhythm).getByText("今日收藏")).toBeTruthy();
    expect(within(rhythm).getByText("4 / 3")).toBeTruthy();
    expect(within(rhythm).getByText("1 / 2")).toBeTruthy();
    // 建议为 0 的项不显示；超过建议也只是提示，不是限制。
    expect(within(rhythm).queryByText("本月实践")).toBeNull();
    expect(within(rhythm).getByText(/已超过建议/)).toBeTruthy();

    const due = (await screen.findByText("项目 p-due")).closest("article")!;
    expect(within(due).getByText(/已到期/)).toBeTruthy();
    const later = screen.getByText("项目 p-later").closest("article")!;
    expect(within(later).queryByText(/已到期/)).toBeNull();
    expect(within(later).getByText(/计划时间/)).toBeTruthy();
    // #57：有时间、无行动文字时不能再说「尚未安排下一步」。
    expect(within(later).getByText("已定时间，尚未写具体行动。")).toBeTruthy();
    expect(within(later).queryByText("尚未安排下一步。")).toBeNull();
  });
});
