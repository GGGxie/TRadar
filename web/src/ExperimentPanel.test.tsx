import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ExperimentPanel } from "./ExperimentPanel";
import type { Evidence, Experiment, Project } from "./api";

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

const evidence: Evidence = {
  id: "ev1",
  projectId: "p1",
  sourceRecordId: null,
  url: "https://example.com/evidence",
  title: "首次使用记录",
  excerpt: "用户需要手工修改配置。",
  kind: "PUBLIC_EXCERPT",
  publisher: null,
  publishedAt: null,
  collectedAt: "2026-10-16T02:00:00Z",
  isPublic: false,
  observationIds: [],
  supersedesId: null,
};

const emptyPage = {
  data: [],
  pagination: { page: 1, pageSize: 20, totalItems: 0, totalPages: 0 },
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ExperimentPanel", () => {
  it("keeps a failed create draft and retries with one stable intent", async () => {
    const posts: Array<RequestInit> = [];
    let postCount = 0;
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (path.includes("/experiments?") && !options?.method)
        return new Response(JSON.stringify(emptyPage));
      if (path.includes("/evidence?") && !options?.method)
        return new Response(
          JSON.stringify({
            data: [evidence],
            pagination: {
              page: 1,
              pageSize: 100,
              totalItems: 1,
              totalPages: 1,
            },
          }),
        );
      if (options?.method === "POST") {
        posts.push(options);
        postCount += 1;
        if (postCount === 1)
          return new Response(
            JSON.stringify({
              error: { code: "TEMPORARY", message: "暂时保存失败" },
            }),
            { status: 503 },
          );
        const input = JSON.parse(String(options.body));
        return new Response(
          JSON.stringify({
            data: {
              id: "ex1",
              projectId: "p1",
              revision: 1,
              createdAt: "2026-10-16T03:00:00Z",
              updatedAt: "2026-10-16T03:00:00Z",
              ...input,
            },
          }),
          { status: 201 },
        );
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    render(<ExperimentPanel project={project} evidenceRevision={0} />);
    await screen.findByText("还没有实践记录");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "新建实践" }));
    await user.type(screen.getByLabelText("实践标题"), "验证首次使用路径");
    await user.type(screen.getByLabelText("假设"), "无需文档即可完成配置");
    await user.type(
      screen.getByLabelText("验证方法"),
      "在隔离环境中记录阻塞点",
    );
    await user.selectOptions(screen.getByLabelText("实践状态"), "RUNNING");
    await user.click(screen.getByLabelText("引用证据：首次使用记录"));
    await user.type(
      screen.getByLabelText("产物链接（每行一个）"),
      "https://example.com/demo",
    );
    const beforeUnload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(beforeUnload);
    expect(beforeUnload.defaultPrevented).toBe(true);
    await user.click(screen.getByRole("button", { name: "保存实践" }));
    await screen.findByRole("alert");
    expect((screen.getByLabelText("实践标题") as HTMLInputElement).value).toBe(
      "验证首次使用路径",
    );
    await user.click(screen.getByRole("button", { name: "保存实践" }));
    await screen.findByRole("heading", { name: "验证首次使用路径" });
    expect(posts).toHaveLength(2);
    expect(new Headers(posts[0].headers).get("Idempotency-Key")).toBeTruthy();
    expect(new Headers(posts[1].headers).get("Idempotency-Key")).toBe(
      new Headers(posts[0].headers).get("Idempotency-Key"),
    );
    expect(JSON.parse(String(posts[1].body))).toMatchObject({
      status: "RUNNING",
      evidenceIds: ["ev1"],
      artifactUrls: ["https://example.com/demo"],
    });
  });

  it("shows the latest server version, retains local text and retries with its revision", async () => {
    const original: Experiment = {
      id: "ex1",
      projectId: "p1",
      title: "验证安装体验",
      hypothesis: "十分钟可完成",
      method: "在隔离环境安装",
      status: "RUNNING",
      timeBudgetMinutes: 180,
      actualMinutes: null,
      result: "",
      outcome: null,
      evidenceIds: [],
      artifactUrls: [],
      nextAction: "",
      revision: 1,
      createdAt: "2026-10-16T03:00:00Z",
      updatedAt: "2026-10-16T03:00:00Z",
    };
    const latest = {
      ...original,
      nextAction: "服务器刚保存的下一步",
      revision: 2,
      updatedAt: "2026-10-16T04:00:00Z",
    };
    const patchRevisions: Array<string | null> = [];
    let patchCount = 0;
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      if (path.includes("/experiments?") && !options?.method)
        return new Response(
          JSON.stringify({
            data: [original],
            pagination: {
              page: 1,
              pageSize: 20,
              totalItems: 1,
              totalPages: 1,
            },
          }),
        );
      if (path.includes("/evidence?") && !options?.method)
        return new Response(JSON.stringify(emptyPage));
      if (path.endsWith("/experiments/ex1") && !options?.method)
        return new Response(JSON.stringify({ data: latest }));
      if (options?.method === "PATCH") {
        patchRevisions.push(new Headers(options.headers).get("If-Match"));
        patchCount += 1;
        if (patchCount === 1)
          return new Response(
            JSON.stringify({
              error: {
                code: "VERSION_CONFLICT",
                message: "实践记录已被修改",
              },
            }),
            { status: 412 },
          );
        const input = JSON.parse(String(options.body));
        return new Response(
          JSON.stringify({ data: { ...latest, ...input, revision: 3 } }),
        );
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetcher);
    render(<ExperimentPanel project={project} evidenceRevision={0} />);
    await screen.findByRole("heading", { name: "验证安装体验" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑实践" }));
    await user.type(screen.getByLabelText("结果记录"), "我的本地验证结果");
    await user.click(screen.getByRole("button", { name: "保存实践" }));
    await screen.findByText("服务器刚保存的下一步");
    expect(
      (screen.getByLabelText("结果记录") as HTMLTextAreaElement).value,
    ).toBe("我的本地验证结果");
    await user.click(screen.getByRole("button", { name: "保存实践" }));
    await screen.findByText("我的本地验证结果");
    expect(patchRevisions).toEqual(['"1"', '"2"']);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(5));
  });
});
