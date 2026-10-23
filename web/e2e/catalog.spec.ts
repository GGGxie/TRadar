import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("create, read, edit and reload a project with no unexpected dialog", async ({
  page,
}, testInfo) => {
  const dialogs: string[] = [];
  const errors: string[] = [];
  page.on("dialog", async (dialog) => {
    dialogs.push(dialog.message());
    await dialog.accept();
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/radar");
  await expect(
    page.getByText("先采集一次，看看最近受关注的项目"),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "前往信息源设置" }),
  ).toHaveAttribute("href", "/settings");
  await page.getByRole("button", { name: "记录项目", exact: true }).click();
  await page.getByLabel("项目名称", { exact: true }).fill("Edge Signal");
  await page
    .getByLabel("项目链接", { exact: true })
    .fill("https://example.com/edge?utm_source=browser-test");
  await page.getByLabel("项目分类", { exact: true }).selectOption("DEVTOOLS");
  await page
    .getByLabel("一句话介绍", { exact: true })
    .fill("浏览器测试样本：帮助开发者追踪有价值的变化。");
  await page.getByRole("button", { name: "保存项目", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Edge Signal", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "example.com" })).toHaveAttribute(
    "href",
    "https://example.com/edge",
  );
  expect(dialogs).toEqual([]);
  await expect(page.getByText("还没有证据记录")).toBeVisible();
  await page.getByRole("button", { name: "记录证据" }).click();
  await page
    .getByLabel("证据链接", { exact: true })
    .fill("https://example.com/edge/pricing?utm_source=browser-test");
  await page.getByLabel("证据标题", { exact: true }).fill("官方定价说明");
  await page
    .getByLabel("原文摘录", { exact: true })
    .fill("基础套餐标价为每月 20 美元；这是页面原文的测试摘录。");
  await page
    .getByLabel("证据类型", { exact: true })
    .selectOption("OFFICIAL_PRICING");
  await page.getByRole("button", { name: "保存证据" }).click();
  await expect(
    page.getByRole("heading", { name: "官方定价说明", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("私密", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("link", { name: "查看原始来源" }),
  ).toHaveAttribute("href", "https://example.com/edge/pricing");
  await page.getByRole("button", { name: "补充修订" }).click();
  await page
    .getByLabel("证据标题", { exact: true })
    .fill("官方定价说明（补充期间）");
  await page.getByRole("button", { name: "保存证据" }).click();
  await expect(
    page.getByRole("heading", {
      name: "官方定价说明（补充期间）",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "官方定价说明", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "生成规则初筛" }).click();
  const rulesBuilder = page.locator(".analysis-builder");
  await rulesBuilder.getByLabel(/官方定价说明（补充期间）/).check();
  await rulesBuilder.getByRole("button", { name: "生成并冻结版本" }).click();
  await expect(
    page.getByText("规则版本已生成，引用和当前趋势已冻结。"),
  ).toBeVisible();
  await expect(page.getByText("规则模板", { exact: true })).toBeVisible();
  await expect(page.getByText(/官方定价说明（补充期间）.*记录/)).toBeVisible();
  await page.getByText(/冻结证据快照/).click();
  await expect(
    page.getByText("私密证据 · 仅本地规则使用", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "基于此版修订" }).click();
  const manualBuilder = page.locator(".manual-analysis");
  await manualBuilder
    .getByLabel("对象是什么 · 声明 1 类型")
    .selectOption("FACT");
  await manualBuilder
    .getByLabel("对象是什么 · 声明 1 内容")
    .fill("定价页公开列出了基础套餐价格。");
  await manualBuilder
    .getByLabel("对象是什么 · 声明 1 引用 官方定价说明（补充期间）")
    .check();
  await manualBuilder.getByRole("button", { name: "保存人工版本" }).click();
  await expect(
    page.getByText("人工版本已保存，旧版本继续保留。"),
  ).toBeVisible();
  await expect(page.getByText("人工版本", { exact: true })).toBeVisible();
  await expect(page.getByText("事实", { exact: true }).first()).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  await page.evaluate(() => {
    window.scrollTo(0, 0);
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });
  await page.screenshot({
    path: testInfo.outputPath("analysis-desktop.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "编辑信息", exact: true }).click();
  await page
    .getByLabel("一句话介绍", { exact: true })
    .fill("已更新的项目介绍，刷新后应保留。");
  await page.getByRole("button", { name: "保存项目", exact: true }).click();
  await expect(
    page.getByText("已更新的项目介绍，刷新后应保留。", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/项目信息或证据关联已变化/).first(),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByText("已更新的项目介绍，刷新后应保留。", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "返回项目目录" }).click();
  await page.getByLabel("搜索项目", { exact: true }).fill("Edge");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Edge Signal", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("1 个匹配项目", { exact: true })).toBeVisible();
  await page.getByLabel("趋势窗口").selectOption("7d");
  await expect(
    page.getByRole("heading", { name: "Edge Signal", exact: true }),
  ).toBeVisible();
  await page.getByLabel("筛选来源").selectOption("GITHUB");
  await expect(page.getByText("没有匹配的项目", { exact: true })).toBeVisible();
  await page.getByLabel("筛选来源").selectOption("");
  await expect(
    page.getByRole("heading", { name: "Edge Signal", exact: true }),
  ).toBeVisible();
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
  expect(errors).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("catalog-desktop.png"),
    fullPage: true,
  });
});

test("mobile catalog stays within the viewport and remains accessible", async ({
  page,
  request,
}, testInfo) => {
  const created = await request.post("/api/v1/projects", {
    headers: { "Idempotency-Key": "mobile-seed" },
    data: {
      name: "Mobile Signal",
      canonicalUrl: "https://example.com/mobile",
      category: "AI_AGENT",
      summary: "独立的手机布局测试样本。",
    },
  });
  expect(created.status()).toBe(201);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/radar");
  await expect(
    page.getByRole("heading", { name: "项目目录", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Mobile Signal", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("catalog-mobile.png"),
    fullPage: true,
  });
});
