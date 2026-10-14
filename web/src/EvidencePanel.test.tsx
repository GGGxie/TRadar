import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EvidencePanel } from "./EvidencePanel";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("EvidencePanel", () => {
  it("stores a manual claim privately with one retry identity", async () => {
    const fetcher = vi.fn(async (_path: string, options?: RequestInit) => {
      if (options?.method === "POST") {
        const input = JSON.parse(String(options.body));
        return new Response(
          JSON.stringify({
            data: {
              id: "e1",
              projectId: "p1",
              ...input,
              publisher: null,
              publishedAt: null,
              collectedAt: "2026-10-14T08:00:00.000000000Z",
              isPublic: false,
              observationIds: [],
              sourceRecordId: null,
              supersedesId: null,
            },
          }),
          { status: 201 },
        );
      }
      return new Response(
        JSON.stringify({
          data: [],
          pagination: {
            page: 1,
            pageSize: 20,
            totalItems: 0,
            totalPages: 0,
          },
        }),
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(<EvidencePanel projectId="p1" />);
    await screen.findByText("还没有证据记录");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "记录证据" }));
    await user.type(
      screen.getByLabelText("证据链接"),
      "https://example.com/pricing",
    );
    await user.type(screen.getByLabelText("证据标题"), "官方定价");
    await user.type(
      screen.getByLabelText("原文摘录"),
      "基础套餐每月 20 美元。",
    );
    await user.selectOptions(
      screen.getByLabelText("证据类型"),
      "OFFICIAL_PRICING",
    );
    await user.click(screen.getByRole("button", { name: "保存证据" }));
    await screen.findByRole("heading", { name: "官方定价" });
    const post = fetcher.mock.calls.find(
      ([, options]) => options?.method === "POST",
    );
    expect(post).toBeDefined();
    const headers = new Headers(post?.[1]?.headers);
    expect(headers.get("Idempotency-Key")).toBeTruthy();
    expect(JSON.parse(String(post?.[1]?.body))).toMatchObject({
      kind: "OFFICIAL_PRICING",
      isPublic: false,
    });
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  });

  it("preserves the evidence draft and retry key when saving fails", async () => {
    const fetcher = vi.fn(async (_path: string, options?: RequestInit) => {
      if (options?.method === "POST") throw new Error("offline");
      return new Response(
        JSON.stringify({
          data: [],
          pagination: {
            page: 1,
            pageSize: 20,
            totalItems: 0,
            totalPages: 0,
          },
        }),
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(<EvidencePanel projectId="p1" />);
    await screen.findByText("还没有证据记录");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "记录证据" }));
    await user.type(
      screen.getByLabelText("证据链接"),
      "https://example.com/story",
    );
    await user.type(screen.getByLabelText("证据标题"), "保留的证据");
    await user.type(screen.getByLabelText("原文摘录"), "网络失败也不能丢失。");
    await user.click(screen.getByRole("button", { name: "保存证据" }));
    await screen.findByRole("alert");
    expect((screen.getByLabelText("证据标题") as HTMLInputElement).value).toBe(
      "保留的证据",
    );
    await user.click(screen.getByRole("button", { name: "保存证据" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
    const posts = fetcher.mock.calls.filter(
      ([, options]) => options?.method === "POST",
    );
    const retryKey = (index: number) =>
      new Headers(posts[index][1]?.headers).get("Idempotency-Key");
    expect(retryKey(0)).toBe(retryKey(1));
  });
});
