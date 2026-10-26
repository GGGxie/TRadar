import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProjectForm } from "./ProjectForm";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
describe("ProjectForm", () => {
  it("saves a project with a stable retry key and returns the stored identity", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: { id: "p1", name: "测试项目" } }), {
        status: 201,
      }),
    );
    vi.stubGlobal("fetch", fetcher);
    const saved = vi.fn();
    render(<ProjectForm onSaved={saved} onCancel={() => {}} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("项目名称"), "测试项目");
    await user.type(screen.getByLabelText("项目链接"), "https://example.com");
    await user.click(screen.getByRole("button", { name: "保存项目" }));
    await waitFor(() =>
      expect(saved).toHaveBeenCalledWith(
        expect.objectContaining({ id: "p1" }),
        false,
      ),
    );
    const options = fetcher.mock.calls[0][1] as RequestInit;
    expect(new Headers(options.headers).get("Idempotency-Key")).toBeTruthy();
    expect(JSON.parse(String(options.body))).toMatchObject({
      name: "测试项目",
      canonicalUrl: "https://example.com",
      category: "OTHER",
    });
  });
  it("reports when the link already exists in the catalog", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ data: { id: "old", name: "已有项目" } }),
          {
            status: 200,
          },
        ),
      ),
    );
    const saved = vi.fn();
    render(<ProjectForm onSaved={saved} onCancel={() => {}} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("项目名称"), "重复录入");
    await user.type(
      screen.getByLabelText("项目链接"),
      "https://GitHub.com/acme/tool/",
    );
    await user.click(screen.getByRole("button", { name: "保存项目" }));
    await waitFor(() =>
      expect(saved).toHaveBeenCalledWith(
        expect.objectContaining({ id: "old" }),
        true,
      ),
    );
  });
  it("preserves input and reuses the intent after a failed save", async () => {
    const fetcher = vi.fn().mockRejectedValue(new Error("offline"));
    vi.stubGlobal("fetch", fetcher);
    render(<ProjectForm onSaved={() => {}} onCancel={() => {}} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("项目名称"), "保留草稿");
    await user.type(screen.getByLabelText("项目链接"), "https://example.com");
    await user.click(screen.getByRole("button", { name: "保存项目" }));
    await screen.findByRole("alert");
    expect((screen.getByLabelText("项目名称") as HTMLInputElement).value).toBe(
      "保留草稿",
    );
    await user.click(screen.getByRole("button", { name: "保存项目" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    const key = (index: number) =>
      new Headers((fetcher.mock.calls[index][1] as RequestInit).headers).get(
        "Idempotency-Key",
      );
    expect(key(0)).toBe(key(1));
  });
});
