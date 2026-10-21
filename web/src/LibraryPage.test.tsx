import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LibraryPage } from "./LibraryPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("LibraryPage", () => {
  it("shows saved research with its project and applies explicit filters", async () => {
    const paths: string[] = [];
    const fetcher = vi.fn(async (path: string) => {
      paths.push(path);
      if (path.includes("/research?")) {
        return new Response(
          JSON.stringify({
            data: [
              {
                id: "r1",
                projectId: "p1",
                isStarred: true,
                tags: ["Agent", "商业"],
                stage: "DEEP_RESEARCH",
                decision: "OBSERVE",
                rationale: "需求存在，付费仍待验证。",
                notes: "保留自己的判断。",
                nextAction: "访谈三个用户",
                nextActionAt: null,
                revision: 2,
                createdAt: "2026-10-16T01:00:00Z",
                updatedAt: "2026-10-16T02:00:00Z",
              },
            ],
            pagination: {
              page: 1,
              pageSize: 20,
              totalItems: 1,
              totalPages: 1,
            },
          }),
        );
      }
      if (path.endsWith("/projects/p1")) {
        return new Response(
          JSON.stringify({
            data: {
              id: "p1",
              name: "Signal Lab",
              canonicalUrl: "https://example.com/signal",
              summary: "帮助团队发现变化。",
              category: "DEVTOOLS",
              firstSeenAt: "2026-10-16T01:00:00Z",
              lastSeenAt: "2026-10-16T01:00:00Z",
              createdAt: "2026-10-16T01:00:00Z",
              updatedAt: "2026-10-16T01:00:00Z",
              revision: 1,
            },
          }),
        );
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    render(<LibraryPage />);
    await screen.findByRole("heading", { name: "Signal Lab" });
    expect(screen.getByText("保留自己的判断。")).toBeTruthy();
    expect(screen.getAllByText("深入研究").length).toBeGreaterThan(0);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("搜索认知库"), "付费");
    await user.selectOptions(
      screen.getByLabelText("筛选研究阶段"),
      "DEEP_RESEARCH",
    );
    await user.click(screen.getByLabelText("只看收藏"));
    await user.click(screen.getByRole("button", { name: "筛选" }));
    await waitFor(() =>
      expect(
        paths.some(
          (path) =>
            path.includes("q=%E4%BB%98%E8%B4%B9") &&
            path.includes("stage=DEEP_RESEARCH") &&
            path.includes("isStarred=true"),
        ),
      ).toBe(true),
    );
  });
});
