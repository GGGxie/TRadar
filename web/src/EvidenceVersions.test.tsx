import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EvidencePanel } from "./EvidencePanel";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const base = {
  projectId: "p1",
  sourceRecordId: null,
  url: "https://example.com/a",
  title: "定价说明",
  kind: "OFFICIAL_PRICING",
  publisher: null,
  publishedAt: null,
  isPublic: false,
  observationIds: [],
};

describe("EvidencePanel versions", () => {
  it("marks superseded evidence and only offers revision on the latest version", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [
                {
                  ...base,
                  id: "new",
                  excerpt: "第二版",
                  collectedAt: "2026-10-23T08:00:00.000000000Z",
                  supersedesId: "old",
                },
                {
                  ...base,
                  id: "old",
                  excerpt: "第一版",
                  collectedAt: "2026-10-23T07:00:00.000000000Z",
                  supersedesId: null,
                  supersededBy: "new",
                },
              ],
              pagination: {
                page: 1,
                pageSize: 20,
                totalItems: 2,
                totalPages: 1,
              },
            }),
          ),
      ),
    );
    render(<EvidencePanel projectId="p1" />);
    const latest = (await screen.findByText("第二版")).closest("article")!;
    const old = screen.getByText("第一版").closest("article")!;
    expect(within(latest).getByText("修订版")).toBeTruthy();
    expect(
      within(latest).getByRole("button", { name: "补充修订" }),
    ).toBeTruthy();
    expect(within(old).getByText("已被新版本替代")).toBeTruthy();
    expect(within(old).queryByRole("button", { name: "补充修订" })).toBeNull();
    expect(old.className).toContain("superseded");
  });
});
