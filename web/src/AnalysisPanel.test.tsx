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

const publicEvidence = {
  ...evidence,
  id: "e-public",
  title: "公开产品说明",
  excerpt: "公开页面记录了首批用户。",
  isPublic: true,
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

  it("selects or clears all selectable evidence at once", async () => {
    const bodies: Record<string, unknown>[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (path: string, options?: RequestInit) => {
        if (path.includes("/analysis-capabilities"))
          return new Response(
            JSON.stringify({
              data: { rulesEnabled: true, modelEnabled: false },
            }),
          );
        if (path.includes("/evidence?"))
          return new Response(
            JSON.stringify({
              data: [
                {
                  ...evidence,
                  id: "e-public",
                  title: "公开说明",
                  isPublic: true,
                },
                {
                  ...evidence,
                  id: "e-private",
                  title: "私密摘录",
                  isPublic: false,
                },
              ],
              pagination: {
                page: 1,
                pageSize: 100,
                totalItems: 2,
                totalPages: 1,
              },
            }),
          );
        if (path.includes("/analysis-runs") && options?.method === "POST") {
          bodies.push(JSON.parse(String(options.body)));
          return new Response(
            JSON.stringify({ error: { code: "STOP", message: "停止" } }),
            { status: 500 },
          );
        }
        return new Response(
          JSON.stringify({
            data: [],
            pagination: { page: 1, pageSize: 20, totalItems: 0, totalPages: 0 },
          }),
        );
      }),
    );
    const user = userEvent.setup();
    render(<AnalysisPanel project={project} evidenceRevision={0} />);
    await user.click(
      await screen.findByRole("button", { name: "生成规则初筛" }),
    );
    await user.click(await screen.findByRole("button", { name: "全选" }));
    expect(
      screen
        .getAllByRole("checkbox")
        .every((box) => (box as HTMLInputElement).checked),
    ).toBe(true);
    await user.click(screen.getByRole("button", { name: "清空" }));
    expect(
      screen
        .getAllByRole("checkbox")
        .some((box) => (box as HTMLInputElement).checked),
    ).toBe(false);
    await user.click(screen.getByRole("button", { name: "全选" }));
    await user.click(screen.getByRole("button", { name: "生成并冻结版本" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0].evidenceIds).toEqual(["e-public", "e-private"]);
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
    expect(screen.getByDisplayValue("创始人公开披露了收入数字。")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "保存人工版本" }));
    await waitFor(() => expect(postKeys).toHaveLength(2));
    expect(postKeys[0]).toBeTruthy();
    expect(postKeys[0]).toBe(postKeys[1]);
  });

  it("blocks a blank manual version until something is written", async () => {
    // #56：表单预置的「尚未形成判断。」不算内容，空白人工版本不发请求。
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (options?.method === "POST")
        return new Response("{}", { status: 500 });
      if (path.includes("/evidence?"))
        return new Response(JSON.stringify(page([])));
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
    await user.click(screen.getByRole("button", { name: "保存人工版本" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "至少写下一条声明、待验证问题或下一步检查",
    );
    expect(
      fetcher.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === "POST",
      ),
    ).toBe(false);
  });
  it("runs an enabled model with public evidence only and polls its frozen version", async () => {
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (options?.method === "POST" && path.endsWith("/analysis-runs"))
        return new Response(
          JSON.stringify({
            data: {
              id: "model-run",
              projectId: "p1",
              level: "SCREEN",
              mode: "MODEL",
              status: "PENDING",
              revision: 1,
              projectRevision: 1,
              cutoffAt: "2026-10-19T02:00:00Z",
              analysisId: null,
              errorCode: null,
              usage: null,
              createdAt: "2026-10-19T02:00:00Z",
              finishedAt: null,
            },
          }),
          { status: 202 },
        );
      if (path === "/api/v1/analysis-runs/model-run")
        return new Response(
          JSON.stringify({
            data: {
              id: "model-run",
              status: "SUCCEEDED",
              analysisId: "model-version",
              usage: { inputTokens: 40, outputTokens: 15 },
            },
          }),
        );
      if (path.includes("/evidence?"))
        return new Response(JSON.stringify(page([evidence, publicEvidence])));
      if (path === "/api/v1/analysis-capabilities")
        return new Response(
          JSON.stringify({
            data: {
              rulesEnabled: true,
              modelEnabled: true,
              provider: "openai",
              model: "configured-model",
              maxOutputTokens: 800,
              dailyTokenBudget: 100000,
            },
          }),
        );
      if (path.includes("/analyses?")) {
        const generated = fetcher.mock.calls.some(
          ([called, init]) =>
            String(called).endsWith("/analysis-runs") &&
            (init as RequestInit | undefined)?.method === "POST",
        );
        return new Response(
          JSON.stringify(
            page(
              generated
                ? [
                    {
                      id: "model-version",
                      projectId: "p1",
                      level: "SCREEN",
                      version: 1,
                      origin: "MODEL",
                      projectRevision: 1,
                      basedOnAnalysisId: null,
                      createdAt: "2026-10-19T02:00:01Z",
                      cutoffAt: "2026-10-19T02:00:00Z",
                      rulesVersion: null,
                      model: {
                        provider: "openai",
                        name: "configured-model",
                        promptVersion: "model-analysis-v1",
                      },
                      evidenceIds: ["e-public"],
                      evidenceSnapshots: [publicEvidence],
                      trendSnapshot: null,
                      sections: [
                        {
                          key: "WHAT",
                          claims: [
                            {
                              kind: "FACT",
                              text: "公开页面记录了首批用户。",
                              evidenceIds: ["e-public"],
                            },
                          ],
                        },
                        {
                          key: "PROBLEM",
                          claims: [
                            {
                              kind: "UNKNOWN",
                              text: "需求强度未知。",
                              evidenceIds: [],
                            },
                          ],
                        },
                        {
                          key: "ATTENTION",
                          claims: [
                            {
                              kind: "HYPOTHESIS",
                              text: "可能值得观察。",
                              evidenceIds: [],
                            },
                          ],
                        },
                        {
                          key: "FIT",
                          claims: [
                            {
                              kind: "UNKNOWN",
                              text: "适配度待人工判断。",
                              evidenceIds: [],
                            },
                          ],
                        },
                      ],
                      openQuestions: ["是否留存？"],
                      nextChecks: ["人工核对第二来源。"],
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
    await user.click(screen.getByRole("button", { name: "生成模型初筛" }));
    expect(
      (screen.getByLabelText(/创始人访谈/) as HTMLInputElement).disabled,
    ).toBe(true);
    await user.click(screen.getByLabelText(/公开产品说明/));
    await user.click(
      screen.getByRole("button", { name: "调用模型并冻结版本" }),
    );
    expect(
      (await screen.findAllByText("公开页面记录了首批用户。")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("模型生成")).toBeTruthy();
    const post = fetcher.mock.calls.find(
      ([path, options]) =>
        String(path).endsWith("/analysis-runs") &&
        (options as RequestInit | undefined)?.method === "POST",
    );
    expect(JSON.parse(String((post?.[1] as RequestInit).body))).toMatchObject({
      level: "SCREEN",
      mode: "MODEL",
      projectRevision: 1,
      evidenceIds: ["e-public"],
    });
  });
});
