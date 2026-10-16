import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AnalysisPanel } from "./AnalysisPanel";
import type { Project } from "./api";

const project: Project = {
  id: "p1",
  name: "Signal Lab",
  canonicalUrl: "https://example.com/signal",
  summary: "帮助团队发现变化。",
  category: "DEVTOOLS",
  firstSeenAt: "2026-10-14T08:00:00Z",
  lastSeenAt: "2026-10-14T08:00:00Z",
  createdAt: "2026-10-14T08:00:00Z",
  updatedAt: "2026-10-14T08:00:00Z",
  revision: 1,
};

const evidence = {
  id: "e1",
  projectId: "p1",
  sourceRecordId: null,
  url: "https://example.com/founder",
  title: "创始人访谈",
  excerpt: "创始人称月收入达到 1 万美元。",
  kind: "FOUNDER_CLAIM",
  publisher: "Founder",
  publishedAt: null,
  collectedAt: "2026-10-14T08:00:00Z",
  isPublic: false,
  observationIds: [],
  supersedesId: null,
};

function page(data: unknown[]) {
  return {
    data,
    pagination: {
      page: 1,
      pageSize: 20,
      totalItems: data.length,
      totalPages: data.length ? 1 : 0,
    },
  };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AnalysisPanel", () => {
  it("generates a local rules version from explicitly selected private evidence", async () => {
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (options?.method === "POST" && path.endsWith("/analysis-runs")) {
        return new Response(
          JSON.stringify({
            data: {
              id: "run1",
              projectId: "p1",
              level: "SCREEN",
              mode: "RULES",
              status: "SUCCEEDED",
              revision: 2,
              projectRevision: 1,
              cutoffAt: "2026-10-14T09:00:00Z",
              analysisId: "a1",
              errorCode: null,
              usage: null,
              createdAt: "2026-10-14T09:00:00Z",
              finishedAt: "2026-10-14T09:00:00Z",
            },
          }),
          { status: 202 },
        );
      }
      if (path.includes("/evidence?"))
        return new Response(JSON.stringify(page([evidence])));
      if (path === "/api/v1/analysis-capabilities")
        return new Response(
          JSON.stringify({
            data: { rulesEnabled: true, modelEnabled: false },
          }),
        );
      if (path.includes("/analyses?")) {
        const hasRun = fetcher.mock.calls.some(
          ([called, init]) =>
            String(called).endsWith("/analysis-runs") &&
            (init as RequestInit | undefined)?.method === "POST",
        );
        return new Response(
          JSON.stringify(
            page(
              hasRun
                ? [
                    {
                      id: "a1",
                      projectId: "p1",
                      level: "SCREEN",
                      version: 1,
                      origin: "RULES",
                      projectRevision: 1,
                      basedOnAnalysisId: null,
                      createdAt: "2026-10-14T09:00:00Z",
                      cutoffAt: "2026-10-14T09:00:00Z",
                      rulesVersion: "rules-v1",
                      model: null,
                      evidenceIds: ["e1"],
                      evidenceSnapshots: [evidence],
                      trendSnapshot: null,
                      sections: [
                        {
                          key: "WHAT",
                          claims: [
                            {
                              kind: "HYPOTHESIS",
                              text: "项目目录中的自述。",
                              evidenceIds: [],
                            },
                          ],
                        },
                        {
                          key: "PROBLEM",
                          claims: [
                            {
                              kind: "UNKNOWN",
                              text: "尚无足够证据。",
                              evidenceIds: [],
                            },
                          ],
                        },
                        {
                          key: "ATTENTION",
                          claims: [
                            {
                              kind: "FACT",
                              text: "创始人自述证据（未独立核实）：月收入达到 1 万美元。",
                              evidenceIds: ["e1"],
                            },
                          ],
                        },
                        {
                          key: "FIT",
                          claims: [
                            {
                              kind: "UNKNOWN",
                              text: "尚无足够证据。",
                              evidenceIds: [],
                            },
                          ],
                        },
                      ],
                      openQuestions: ["收入是否可独立核实？"],
                      nextChecks: ["核对付款证据。"],
                      contextChanged: false,
                    },
                  ]
                : [],
            ),
          ),
        );
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    render(<AnalysisPanel project={project} evidenceRevision={0} />);
    await screen.findByText("还没有初筛版本");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "生成规则初筛" }));
    await user.click(screen.getByLabelText(/创始人访谈/));
    await user.click(screen.getByRole("button", { name: "生成并冻结版本" }));
    await screen.findByText(/创始人自述证据（未独立核实）/);
    expect(screen.getByText("事实")).toBeTruthy();
    expect(screen.getByText("私密证据 · 仅本地规则使用")).toBeTruthy();
    const post = fetcher.mock.calls.find(
      ([path, options]) =>
        String(path).endsWith("/analysis-runs") &&
        (options as RequestInit | undefined)?.method === "POST",
    );
    expect(JSON.parse(String((post?.[1] as RequestInit).body))).toMatchObject({
      level: "SCREEN",
      mode: "RULES",
      projectRevision: 1,
      evidenceIds: ["e1"],
    });
  });

  it("saves a manual fact as a new version with a stable retry key", async () => {
    const postKeys: string[] = [];
    let attempts = 0;
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (options?.method === "POST" && path.includes("/analyses")) {
        postKeys.push(
          new Headers(options.headers).get("Idempotency-Key") ?? "",
        );
        attempts += 1;
        if (attempts === 1) throw new Error("offline");
        return new Response(JSON.stringify({ data: { id: "a2" } }), {
          status: 201,
        });
      }
      if (path.includes("/evidence?"))
        return new Response(JSON.stringify(page([evidence])));
      if (path === "/api/v1/analysis-capabilities")
        return new Response(
          JSON.stringify({ data: { rulesEnabled: true, modelEnabled: false } }),
        );
      if (path.includes("/analyses?"))
        return new Response(JSON.stringify(page([])));
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    render(<AnalysisPanel project={project} evidenceRevision={0} />);
    await screen.findByText("还没有初筛版本");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "新建人工版本" }));
    await user.selectOptions(
      screen.getByLabelText("对象是什么 · 声明 1 类型"),
      "FACT",
    );
    await user.clear(screen.getByLabelText("对象是什么 · 声明 1 内容"));
    await user.type(
      screen.getByLabelText("对象是什么 · 声明 1 内容"),
      "创始人公开披露了收入数字。",
    );
    await user.click(
      screen.getByLabelText("对象是什么 · 声明 1 引用 创始人访谈"),
    );
    await user.click(screen.getByRole("button", { name: "保存人工版本" }));
    await screen.findByRole("alert");
    expect(
      screen.getByDisplayValue("创始人公开披露了收入数字。"),
    ).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "保存人工版本" }));
    await waitFor(() => expect(postKeys).toHaveLength(2));
    expect(postKeys[0]).toBeTruthy();
    expect(postKeys[0]).toBe(postKeys[1]);
  });
});
