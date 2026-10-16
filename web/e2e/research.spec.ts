import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("personal research persists and preserves a local draft on conflict", async ({
  page,
  request,
}) => {
  const created = await request.post("/api/v1/projects", {
    headers: { "Idempotency-Key": "research-browser-project" },
    data: {
      name: "Research Signal",
      canonicalUrl: "https://example.com/research-signal",
      category: "SAAS",
      summary: "用于验证个人研究记录。",
    },
  });
  expect(created.status()).toBe(201);
  const project = (await created.json()).data as { id: string };
  await page.goto(`/projects/${project.id}`);
  await expect(page.getByText("还没有个人研究记录")).toBeVisible();
  await page.getByRole("button", { name: "开始研究记录" }).click();
  await page.getByLabel("收藏到认知库").check();
  await page.getByLabel("标签").fill("Agent, 商业");
  await page.getByLabel("研究阶段").selectOption("DEEP_RESEARCH");
  await page.getByLabel("当前判断").selectOption("VALIDATE");
  await page.getByLabel("判断依据").fill("需求证据存在，付费意愿仍需验证。");
  await page.getByLabel("个人笔记").fill("这是用户自己的研究正文。");
  await page.getByLabel("下一步行动").fill("访谈三个目标用户。");
  await page.getByRole("button", { name: "保存研究记录" }).click();
  await expect(page.getByText("个人研究已保存")).toBeVisible();
  await expect(page.getByText("这是用户自己的研究正文。")).toBeVisible();
  await page.getByRole("button", { name: "编辑个人研究" }).click();
  await page.getByLabel("个人笔记").fill("尚未提交的本地新正文。");
  const concurrent = await request.patch(
    `/api/v1/projects/${project.id}/research`,
    {
      headers: { "If-Match": '"1"' },
      data: { notes: "另一页面刚刚保存的正文。" },
    },
  );
  expect(concurrent.status()).toBe(200);
  await page.getByRole("button", { name: "保存研究记录" }).click();
  await expect(page.getByRole("alert")).toContainText("本地内容仍保留");
  await expect(page.getByLabel("个人笔记")).toHaveValue(
    "尚未提交的本地新正文。",
  );
  await expect(page.getByText("另一页面刚刚保存的正文。")).toBeVisible();
  await page.getByRole("button", { name: "保存研究记录" }).click();
  await expect(page.getByText("个人研究已保存")).toBeVisible();
  await expect(page.getByText("尚未提交的本地新正文。")).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test("library filters saved research and fits a mobile viewport", async ({
  page,
  request,
}, testInfo) => {
  const created = await request.post("/api/v1/projects", {
    headers: { "Idempotency-Key": "library-browser-project" },
    data: {
      name: "Library Signal",
      canonicalUrl: "https://example.com/library-signal",
      category: "AI_AGENT",
      summary: "用于验证认知库。",
    },
  });
  const project = (await created.json()).data as { id: string };
  const research = await request.post(
    `/api/v1/projects/${project.id}/research`,
    {
      headers: { "Idempotency-Key": "library-browser-research" },
      data: {
        isStarred: true,
        tags: ["Agent", "增长"],
        stage: "SCREENING",
        decision: "OBSERVE",
        rationale: "先观察持续增长。",
        notes: "认知库中的个人笔记。",
        nextAction: "下周复查趋势。",
        nextActionAt: null,
      },
    },
  );
  expect(research.status()).toBe(201);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/library");
  const navigationItems = await page
    .getByRole("navigation", { name: "主导航" })
    .getByRole("link")
    .all();
  for (const item of navigationItems) {
    const box = await item.boundingBox();
    expect(box?.height).toBeLessThanOrEqual(48);
  }
  await expect(
    page.getByRole("heading", { name: "Library Signal" }),
  ).toBeVisible();
  await expect(page.getByText("认知库中的个人笔记。")).toBeVisible();
  await page.getByLabel("搜索认知库").fill("持续增长");
  await page.getByLabel("只看收藏").check();
  await page.getByRole("button", { name: "筛选" }).click();
  await expect(
    page.getByRole("heading", { name: "Library Signal" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("library-mobile.png"),
    fullPage: true,
  });
});
