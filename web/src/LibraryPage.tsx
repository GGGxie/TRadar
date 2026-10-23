import { useEffect, useState, type FormEvent } from "react";
import {
  categories,
  formatDate,
  getResearchRhythm,
  listResearch,
  request,
  type ResearchRhythm,
  type Project,
  type ResearchDecision,
  type ResearchEntry,
  type ResearchStage,
  type Resource,
} from "./api";
import { researchDecisionLabels, researchStageLabels } from "./ResearchPanel";

interface LibraryItem {
  entry: ResearchEntry;
  project: Project;
}

export function LibraryPage() {
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [stageDraft, setStageDraft] = useState<ResearchStage | "">("");
  const [stage, setStage] = useState<ResearchStage | "">("");
  const [decisionDraft, setDecisionDraft] = useState<ResearchDecision | "">("");
  const [decision, setDecision] = useState<ResearchDecision | "">("");
  const [tagDraft, setTagDraft] = useState("");
  const [tag, setTag] = useState("");
  const [starredDraft, setStarredDraft] = useState(false);
  const [starred, setStarred] = useState(false);
  const [items, setItems] = useState<LibraryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({
      page: String(page),
      pageSize: "20",
    });
    if (query) params.set("q", query);
    if (stage) params.set("stage", stage);
    if (decision) params.set("decision", decision);
    if (tag) params.set("tag", tag);
    if (starred) params.set("isStarred", "true");
    setLoading(true);
    setError("");
    listResearch(params, controller.signal)
      .then(async (result) => {
        const projects = await Promise.all(
          result.data.map((entry) =>
            request<Resource<Project>>(
              `/projects/${encodeURIComponent(entry.projectId)}`,
              { signal: controller.signal },
            ).then((response) => response.data),
          ),
        );
        if (controller.signal.aborted) return;
        setItems(
          result.data.map((entry, index) => ({
            entry,
            project: projects[index],
          })),
        );
        setTotal(result.pagination.totalItems);
        setTotalPages(result.pagination.totalPages);
      })
      .catch((problem) => {
        if (!controller.signal.aborted)
          setError(
            problem instanceof Error ? problem.message : "读取认知库失败",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [decision, page, query, stage, starred, tag]);

  function filter(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setQuery(search.trim());
    setStage(stageDraft);
    setDecision(decisionDraft);
    setTag(tagDraft.trim());
    setStarred(starredDraft);
  }

  return (
    <section className="library-page" aria-labelledby="library-title">
      <header className="page-heading">
        <div>
          <p className="eyebrow">RESEARCH LIBRARY</p>
          <h1 id="library-title">认知库</h1>
          <p className="intro">
            回看收藏、判断和下一步。研究建议帮助形成节奏，不限制你保存项目。
          </p>
        </div>
        <span className="library-count">{total} 条研究记录</span>
      </header>
      <RhythmPanel />
      <form className="library-filters" onSubmit={filter}>
        <label className="library-search">
          <span>搜索认知库</span>
          <input
            type="search"
            value={search}
            maxLength={200}
            placeholder="项目、标签、笔记或下一步…"
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <label>
          <span>筛选研究阶段</span>
          <select
            value={stageDraft}
            onChange={(event) =>
              setStageDraft(event.target.value as ResearchStage | "")
            }
          >
            <option value="">全部阶段</option>
            {Object.entries(researchStageLabels).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>筛选判断</span>
          <select
            value={decisionDraft}
            onChange={(event) =>
              setDecisionDraft(event.target.value as ResearchDecision | "")
            }
          >
            <option value="">全部判断</option>
            {Object.entries(researchDecisionLabels).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>标签</span>
          <input
            value={tagDraft}
            maxLength={32}
            placeholder="精确标签"
            onChange={(event) => setTagDraft(event.target.value)}
          />
        </label>
        <label className="library-starred">
          <input
            type="checkbox"
            checked={starredDraft}
            onChange={(event) => setStarredDraft(event.target.checked)}
          />
          只看收藏
        </label>
        <button className="secondary">筛选</button>
      </form>
      {error ? (
        <div className="empty-state">
          <p role="alert" className="error">
            {error}
          </p>
        </div>
      ) : loading ? (
        <p role="status" className="loading">
          正在读取认知库…
        </p>
      ) : !items.length ? (
        <div className="empty-state">
          <div className="empty-radar" aria-hidden="true">
            <span />
          </div>
          <h3>认知库还是空的</h3>
          <p>在项目详情中开始个人研究，记录收藏、判断和下一步。</p>
          <a className="primary" href="/radar">
            浏览项目目录
          </a>
        </div>
      ) : (
        <div className="library-list">
          {items.map(({ entry, project }) => (
            <article key={entry.id}>
              <header>
                <div>
                  <p className="eyebrow">
                    {entry.isStarred ? "★ STARRED" : "RESEARCH"} /{" "}
                    {categories[project.category]}
                  </p>
                  <h2>{project.name}</h2>
                </div>
                <a className="secondary" href={`/projects/${project.id}`}>
                  打开项目
                </a>
              </header>
              <div className="library-meta">
                <span>{researchStageLabels[entry.stage]}</span>
                <span>{researchDecisionLabels[entry.decision]}</span>
                <time>更新于 {formatDate(entry.updatedAt)}</time>
              </div>
              {!!entry.tags.length && (
                <div className="research-tags">
                  {entry.tags.map((item) => (
                    <span key={item}>{item}</span>
                  ))}
                </div>
              )}
              <div className="library-copy">
                <div>
                  <strong>个人笔记</strong>
                  <p>{entry.notes || "尚未写下个人笔记。"}</p>
                </div>
                <div>
                  <strong>下一步</strong>
                  <p>{entry.nextAction || "尚未安排下一步。"}</p>
                  {entry.nextActionAt && (
                    <NextActionTime value={entry.nextActionAt} />
                  )}
                </div>
              </div>
            </article>
          ))}
          <nav className="pagination" aria-label="认知库分页">
            <span>
              第 {page} / {totalPages} 页 · 共 {total} 条
            </span>
            <div>
              <button
                className="secondary"
                disabled={page <= 1}
                onClick={() => setPage((value) => value - 1)}
              >
                上一页
              </button>
              <button
                className="secondary"
                disabled={page >= totalPages}
                onClick={() => setPage((value) => value + 1)}
              >
                下一页
              </button>
            </div>
          </nav>
        </div>
      )}
    </section>
  );
}

// 下一步时间：到期后在站内标出，不发送外部提醒（SPEC-research-workspace）。
function NextActionTime({ value }: { value: string }) {
  const overdue = new Date(value).getTime() <= Date.now();
  return (
    <time
      dateTime={value}
      className={overdue ? "next-action-time overdue" : "next-action-time"}
    >
      {overdue ? "已到期 · " : "计划时间 · "}
      {formatDate(value)}
    </time>
  );
}

// 研究节奏：只做提示，建议为 0 的项不显示，超过建议也不阻止保存（#48）。
function RhythmPanel() {
  const [rhythm, setRhythm] = useState<ResearchRhythm | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    const zone =
      Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
    getResearchRhythm(zone, controller.signal)
      .then((result) => setRhythm(result.data))
      .catch(() => {
        // 节奏是附加信息；读取失败时不影响认知库。
      });
    return () => controller.abort();
  }, []);
  if (!rhythm) return null;
  const items = [
    {
      label: "今日收藏",
      value: rhythm.dailySaved,
      target: rhythm.dailySaveTarget,
      hint: "今天变为收藏的项目",
    },
    {
      label: "本周深研",
      value: rhythm.weeklyDeep,
      target: rhythm.weeklyDeepTarget,
      hint: "本周写过六维深研的项目",
    },
    {
      label: "本月实践",
      value: rhythm.monthlyPractice,
      target: rhythm.monthlyPracticeTarget,
      hint: "本月新建过实践记录的项目",
    },
  ].filter((item) => item.target > 0);
  if (!items.length) return null;
  return (
    <section className="rhythm-panel" aria-label="研究节奏">
      {items.map((item) => (
        <div key={item.label}>
          <span>{item.label}</span>
          <strong>
            {item.value} / {item.target}
          </strong>
          <small className="muted">
            {item.value > item.target
              ? "已超过建议，节奏只是参考"
              : item.value === item.target
                ? "已达到建议"
                : item.hint}
          </small>
        </div>
      ))}
    </section>
  );
}
