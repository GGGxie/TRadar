import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ResearchPanel } from "./ResearchPanel";
import type { Project } from "./api";

const project: Project = {
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
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ResearchPanel", () => {
  it("creates a personal research entry with one stable intent", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      calls.push([path, options]);
      if (!options?.method) {
        return new Response(
          JSON.stringify({
            error: { code: "NOT_FOUND", message: "研究记录不存在" },
          }),
          { status: 404 },
        );
      }
      const input = JSON.parse(String(options.body));
      return new Response(
        JSON.stringify({
          data: {
            id: "r1",
            projectId: "p1",
            revision: 1,
            createdAt: "2026-10-16T02:00:00Z",
            updatedAt: "2026-10-16T02:00:00Z",
            ...input,
          },
        }),
        { status: 201 },
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(<ResearchPanel project={project} />);
    await screen.findByText("还没有个人研究记录");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "开始研究记录" }));
    await user.click(screen.getByLabelText("收藏到认知库"));
    await user.type(screen.getByLabelText("标签"), "Agent, 商业");
    await user.selectOptions(
      screen.getByLabelText("研究阶段"),
      "DEEP_RESEARCH",
    );
    await user.selectOptions(screen.getByLabelText("当前判断"), "OBSERVE");
    await user.type(
      screen.getByLabelText("个人笔记"),
      "这段文字只能由我保存。",
    );
    await user.click(screen.getByRole("button", { name: "保存研究记录" }));
    await screen.findByText("这段文字只能由我保存。");
    const post = calls.find(([, options]) => options?.method === "POST");
    expect(post).toBeDefined();
    expect(new Headers(post?.[1]?.headers).get("Idempotency-Key")).toBeTruthy();
    expect(JSON.parse(String(post?.[1]?.body))).toMatchObject({
      isStarred: true,
      tags: ["Agent", "商业"],
      stage: "DEEP_RESEARCH",
      decision: "OBSERVE",
      notes: "这段文字只能由我保存。",
    });
  });

  it("loads the server version, keeps local text and retries with its revision after 412", async () => {
    const entry = {
      id: "r1",
      projectId: "p1",
      isStarred: true,
      tags: ["Agent"],
      stage: "SCREENING",
      decision: "UNDECIDED",
      rationale: "",
      notes: "服务器旧笔记",
      nextAction: "",
      nextActionAt: null,
      revision: 1,
      createdAt: "2026-10-16T02:00:00Z",
      updatedAt: "2026-10-16T02:00:00Z",
    };
    const latest = {
      ...entry,
      notes: "服务器刚保存的新笔记",
      revision: 2,
      updatedAt: "2026-10-16T03:00:00Z",
    };
    let getCount = 0;
    let patchCount = 0;
    const fetcher = vi.fn(async (_path: string, options?: RequestInit) => {
      if (!options?.method) {
        getCount += 1;
        return new Response(
          JSON.stringify({ data: getCount === 1 ? entry : latest }),
          { status: 200 },
        );
      }
      patchCount += 1;
      if (patchCount === 1) {
        return new Response(
          JSON.stringify({
            error: {
              code: "VERSION_CONFLICT",
              message: "研究记录已被修改，请加载最新版本并手工合并",
            },
          }),
          { status: 412 },
        );
      }
      const input = JSON.parse(String(options.body));
      return new Response(
        JSON.stringify({ data: { ...latest, ...input, revision: 3 } }),
        { status: 200 },
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(<ResearchPanel project={project} />);
    await screen.findByText("服务器旧笔记");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑个人研究" }));
    await user.clear(screen.getByLabelText("个人笔记"));
    await user.type(screen.getByLabelText("个人笔记"), "我的本地新笔记");
    await user.click(screen.getByRole("button", { name: "保存研究记录" }));
    await screen.findByRole("alert");
    expect(screen.getByDisplayValue("我的本地新笔记")).toBeTruthy();
    await screen.findByText("服务器刚保存的新笔记");
    await user.click(screen.getByRole("button", { name: "保存研究记录" }));
    await screen.findByText("个人研究已保存");
    expect(screen.getByText("我的本地新笔记")).toBeTruthy();
    const revisions = fetcher.mock.calls
      .filter(
        ([, options]) =>
          (options as RequestInit | undefined)?.method === "PATCH",
      )
      .map(([, options]) =>
        new Headers((options as RequestInit).headers).get("If-Match"),
      );
    expect(revisions).toEqual(['"1"', '"2"']);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
  });
});
