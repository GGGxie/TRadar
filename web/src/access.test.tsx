import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AccessGate } from "./AccessGate";
import {
  ApiError,
  UNAUTHORIZED_EVENT,
  clearAccessToken,
  getAccessToken,
  request,
  saveAccessToken,
} from "./api";

function mockFetch(status: number, body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

beforeEach(() => clearAccessToken());
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  clearAccessToken();
});

describe("access token in api.request", () => {
  it("leaves requests untouched when no token is stored", async () => {
    const fetchMock = mockFetch(200, { data: [] });
    await request("/projects");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/projects", {});
  });

  it("adds a Bearer header and keeps caller headers when a token is stored", async () => {
    saveAccessToken("secret-token");
    const fetchMock = mockFetch(200, { data: [] });
    await request("/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    });
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(headers.get("Authorization")).toBe("Bearer secret-token");
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(init.method).toBe("POST");
  });

  it("announces UNAUTHORIZED, drops the stale token and still throws", async () => {
    saveAccessToken("stale");
    mockFetch(401, { error: { code: "UNAUTHORIZED", message: "需要访问凭据" } });
    const listener = vi.fn();
    window.addEventListener(UNAUTHORIZED_EVENT, listener);
    await expect(request("/projects")).rejects.toBeInstanceOf(ApiError);
    window.removeEventListener(UNAUTHORIZED_EVENT, listener);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(getAccessToken()).toBeNull();
  });

  it("does not treat other errors as an access problem", async () => {
    saveAccessToken("keep-me");
    mockFetch(422, { error: { code: "VALIDATION_FAILED", message: "无效" } });
    const listener = vi.fn();
    window.addEventListener(UNAUTHORIZED_EVENT, listener);
    await expect(request("/projects")).rejects.toBeInstanceOf(ApiError);
    window.removeEventListener(UNAUTHORIZED_EVENT, listener);
    expect(listener).not.toHaveBeenCalled();
    expect(getAccessToken()).toBe("keep-me");
  });

  it("keeps the token in sessionStorage only", () => {
    saveAccessToken("only-session");
    expect(window.sessionStorage.getItem("tradar.accessToken")).toBe("only-session");
    expect(window.localStorage.getItem("tradar.accessToken")).toBeNull();
  });
});

describe("AccessGate", () => {
  it("renders children until an UNAUTHORIZED event arrives", async () => {
    render(
      <AccessGate onUnlock={vi.fn()}>
        <p>项目目录</p>
      </AccessGate>,
    );
    expect(screen.getByText("项目目录")).toBeTruthy();
    expect(screen.queryByLabelText("访问令牌")).toBeNull();

    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    expect(await screen.findByLabelText("访问令牌")).toBeTruthy();
  });

  it("stores the entered token and unlocks", async () => {
    const onUnlock = vi.fn();
    render(
      <AccessGate onUnlock={onUnlock}>
        <p>项目目录</p>
      </AccessGate>,
    );
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    const input = await screen.findByLabelText("访问令牌");
    expect((input as HTMLInputElement).type).toBe("password");

    await userEvent.type(input, "  typed-token  ");
    await userEvent.click(screen.getByRole("button", { name: "解锁" }));

    await waitFor(() => expect(onUnlock).toHaveBeenCalledTimes(1));
    expect(getAccessToken()).toBe("typed-token");
  });

  it("refuses an empty token without unlocking", async () => {
    const onUnlock = vi.fn();
    render(
      <AccessGate onUnlock={onUnlock}>
        <p>项目目录</p>
      </AccessGate>,
    );
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    await screen.findByLabelText("访问令牌");
    await userEvent.click(screen.getByRole("button", { name: "解锁" }));

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(onUnlock).not.toHaveBeenCalled();
    expect(getAccessToken()).toBeNull();
  });
});
