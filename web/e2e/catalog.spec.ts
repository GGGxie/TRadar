import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("create, read, edit and reload a project with no unexpected dialog", async ({
  page,
}) => {
  const dialogs: string[] = [];
  const errors: string[] = [];
  page.on("dialog", async (dialog) => {
    dialogs.push(dialog.message());
    await dialog.accept();
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/radar");
  await expect(page.getByText("给好奇心一个起点")).toBeVisible();
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
  await page.getByRole("button", { name: "编辑信息", exact: true }).click();
  await page
    .getByLabel("一句话介绍", { exact: true })
    .fill("已更新的项目介绍，刷新后应保留。");
  await page.getByRole("button", { name: "保存项目", exact: true }).click();
  await expect(
    page.getByText("已更新的项目介绍，刷新后应保留。", { exact: true }),
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
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
  expect(errors).toEqual([]);
  await page.screenshot({
    path: "../docs/screenshots/catalog-desktop.png",
    fullPage: true,
  });
});

test("mobile catalog stays within the viewport and remains accessible", async ({
  page,
  request,
}) => {
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
    path: "../docs/screenshots/catalog-mobile.png",
    fullPage: true,
  });
});
