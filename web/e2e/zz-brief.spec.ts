import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("daily brief freezes changes, keeps history, and exports safe Markdown", async ({
  page,
  request,
}, testInfo) => {
  const settingsResponse = await request.get("/api/v1/brief-settings");
  expect(settingsResponse.status()).toBe(200);
  const settingsRevision = settingsResponse.headers().etag;
  expect(settingsRevision).toBeTruthy();
  const disabled = await request.patch("/api/v1/brief-settings", {
    headers: { "If-Match": settingsRevision! },
    data: { isEnabled: false },
  });
  expect(disabled.status()).toBe(200);
  const initialBriefs = await request.get("/api/v1/briefs?page=1&pageSize=20");
  const initialTotal = (await initialBriefs.json()).pagination
    .totalItems as number;

  const created = await request.post("/api/v1/projects", {
    headers: { "Idempotency-Key": "brief-browser-project" },
    data: {
      name: "Daily <Signal>",
      canonicalUrl: "https://example.com/daily-signal",
      category: "AI_AGENT",
      summary: "用于验证不可变日报。",
    },
  });
  expect(created.status()).toBe(201);
  const project = (await created.json()).data as { id: string };
  const evidence = await request.post(
    `/api/v1/projects/${project.id}/evidence`,
    {
      headers: { "Idempotency-Key": "brief-browser-evidence" },
      data: {
        url: "https://example.com/daily-signal/founder",
        title: "创始人说明 # 非标题",
        excerpt: "创始人称已有 12 位付费用户。\n<script>alert(1)</script>",
        kind: "FOUNDER_CLAIM",
        isPublic: false,
      },
    },
  );
  expect(evidence.status()).toBe(201);

  await page.goto("/briefs");
  await expect(page.getByRole("heading", { name: "每日简报" })).toBeVisible();
  await page.getByRole("button", { name: "生成今日简报" }).click();
  await expect(page.getByText(/日报已生成/)).toBeVisible();
  await expect(page.getByRole("link", { name: "下载 Markdown" })).toHaveCount(
    initialTotal + 1,
  );
  const dailyHeading = page.getByRole("heading", { name: "Daily <Signal>" });
  await expect(dailyHeading).toBeVisible();
  await expect(
    page.getByText(
      "创始人自述：创始人称已有 12 位付费用户。 <script>alert(1)</script>",
    ),
  ).toBeVisible();
  const dailyItem = page.locator(".brief-item").filter({ has: dailyHeading });
  await dailyItem.getByText("1 条冻结证据").click();
  await expect(page.getByRole("link", { name: /创始人说明/ })).toBeVisible();

  await page.getByRole("button", { name: "生成今日简报" }).click();
  await expect(page.getByText(/日报已生成/)).toBeVisible();
  await expect(page.getByRole("link", { name: "下载 Markdown" })).toHaveCount(
    initialTotal + 2,
  );

  const download = dailyItem
    .locator("xpath=ancestor::article[contains(@class, 'brief-card')]")
    .getByRole("link", { name: "下载 Markdown" });
  const href = await download.getAttribute("href");
  expect(href).toBeTruthy();
  const markdown = await request.get(href!);
  expect(markdown.status()).toBe(200);
  expect(markdown.headers()["content-type"]).toContain("text/markdown");
  expect(markdown.headers()["content-disposition"]).toContain("tradar-brief-");
  const markdownBody = await markdown.text();
  expect(markdownBody).toContain("创始人自述");
  expect(markdownBody).not.toContain("<script>");
  expect(markdownBody).not.toContain("\n# 非标题");

  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("brief-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 360, height: 800 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});
