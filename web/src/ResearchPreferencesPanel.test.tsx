import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ResearchPreferencesPanel } from "./ResearchPreferencesPanel";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ResearchPreferencesPanel", () => {
  it("updates focus topics and advisory targets with a revision", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const preferences = {
      revision: 1,
      focusTopics: ["AI Agent", "开发者工具", "SaaS"],
      dailySaveTarget: 3,
      weeklyDeepTarget: 2,
      monthlyPracticeTarget: 1,
    };
    const fetcher = vi.fn(async (path: string, options?: RequestInit) => {
      calls.push([path, options]);
      if (options?.method === "PATCH") {
        const patch = JSON.parse(String(options.body));
        return new Response(
          JSON.stringify({ data: { ...preferences, ...patch, revision: 2 } }),
        );
      }
      return new Response(JSON.stringify({ data: preferences }));
    });
    vi.stubGlobal("fetch", fetcher);
    render(<ResearchPreferencesPanel />);
    const daily = await screen.findByLabelText("每日收藏建议");
    const user = userEvent.setup();
    await user.clear(daily);
    await user.type(daily, "0");
    await user.clear(screen.getByLabelText("关注方向"));
    await user.type(screen.getByLabelText("关注方向"), "AI Agent\n独立开发");
    await user.click(screen.getByRole("button", { name: "保存研究偏好" }));
    await screen.findByText("研究偏好已保存");
    const patch = calls.find(([, options]) => options?.method === "PATCH");
    expect(new Headers(patch?.[1]?.headers).get("If-Match")).toBe('"1"');
    expect(JSON.parse(String(patch?.[1]?.body))).toMatchObject({
      focusTopics: ["AI Agent", "独立开发"],
      dailySaveTarget: 0,
    });
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  });
});
