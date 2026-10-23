import { useEffect, useState, type FormEvent } from "react";
import {
  attentionBadge,
  categories,
  primaryAttention,
  projectHost,
  request,
  type Page,
  type ProjectTrend,
} from "./api";
import { ProjectForm } from "./ProjectForm";
export function ProjectList() {
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("");
  const [source, setSource] = useState("");
  const [trendWindow, setTrendWindow] = useState<"24h" | "7d">("24h");
  const [data, setData] = useState<Page<ProjectTrend> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const [creating, setCreating] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    const params = new URLSearchParams({
      page: String(page),
      pageSize: "20",
      q: query,
      category,
      source,
      window: trendWindow,
    });
    request<Page<ProjectTrend>>(`/radar?${params}`, {
      signal: controller.signal,
    })
      .then(setData)
      .catch((error) => {
        if (!controller.signal.aborted) setError(error.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [page, query, category, source, trendWindow, refresh]);
  function filter(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setQuery(search);
  }
  return (
    <>
      <header className="page-heading">
        <div>
          <p className="eyebrow">YOUR DISCOVERY SPACE</p>
          <h1>技术与商业机会雷达</h1>
          <p className="intro">发现值得关注的项目，留下有依据的判断。</p>
        </div>
        <button
          className="primary"
          onClick={() => setCreating(true)}
          disabled={creating}
        >
          <span aria-hidden="true">＋</span> 记录项目
        </button>
      </header>
      {creating && (
        <ProjectForm
          onSaved={(project) => {
            window.location.assign(`/projects/${project.id}`);
          }}
          onCancel={() => setCreating(false)}
        />
      )}
      <div className="context-strip">
        <div>
          <span className="status-dot" />
          <strong>个人项目目录</strong>
          <span className="muted">从一个真正感兴趣的项目开始</span>
        </div>
        <span className="mono">
          {data?.pagination.totalItems ?? "—"} 个匹配项目
        </span>
      </div>
      <section className="catalog" aria-labelledby="catalog-title">
        <div className="section-heading">
          <div>
            <p className="eyebrow">RADAR / PROJECTS</p>
            <h2 id="catalog-title">项目目录</h2>
          </div>
          <button
            className="text-button"
            onClick={() => setRefresh((value) => value + 1)}
            disabled={loading}
          >
            刷新列表
          </button>
        </div>
        <form className="filters" onSubmit={filter}>
          <div className="search-field">
            <label className="sr-only" htmlFor="search">
              搜索项目
            </label>
            <input
              id="search"
              type="search"
              placeholder="搜索项目名称或介绍…"
              value={search}
              maxLength={200}
              onChange={(event) => setSearch(event.target.value)}
            />
            <button type="submit" className="secondary">
              搜索
            </button>
          </div>
          <label className="category-filter">
            <span className="sr-only">筛选方向</span>
            <select
              value={category}
              onChange={(event) => {
                setCategory(event.target.value);
                setPage(1);
              }}
            >
              <option value="">全部方向</option>
              {Object.entries(categories).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label className="category-filter">
            <span className="sr-only">筛选来源</span>
            <select
              aria-label="筛选来源"
              value={source}
              onChange={(event) => {
                setSource(event.target.value);
                setPage(1);
              }}
            >
              <option value="">全部来源</option>
              <option value="GITHUB">GitHub</option>
              <option value="HACKER_NEWS">Hacker News</option>
              <option value="PRODUCT_HUNT">Product Hunt</option>
            </select>
          </label>
          <label className="category-filter">
            <span className="sr-only">趋势窗口</span>
            <select
              aria-label="趋势窗口"
              value={trendWindow}
              onChange={(event) => {
                setTrendWindow(event.target.value as "24h" | "7d");
                setPage(1);
              }}
            >
              <option value="24h">24 小时趋势</option>
              <option value="7d">7 天趋势</option>
            </select>
          </label>
        </form>
        {error ? (
          <div className="empty-state">
            <p role="alert" className="error">
              {error}
            </p>
            <button
              className="secondary"
              onClick={() => setRefresh((v) => v + 1)}
            >
              重新加载
            </button>
          </div>
        ) : loading ? (
          <div className="loading" role="status">
            正在读取项目目录…
          </div>
        ) : data?.data.length ? (
          <>
            {data.data.every(({ trend }) => trend.score == null) && (
              <p className="radar-hint" role="note">
                <span>
                  本页项目暂按当前关注度排列。增速需要同一项目相隔约 24
                  小时的两次观测，可以明天再采集一次，或
                </span>
                <a href="/settings">开启定时采集</a>
              </p>
            )}
            <div className="project-table">
              <div className="table-head">
                <span>项目 / 需求</span>
                <span>方向</span>
                <span>当前关注 / 趋势</span>
                <span />
              </div>
              {data.data.map(({ project, trend }) => (
                <a
                  className="project-row"
                  key={project.id}
                  href={`/projects/${project.id}`}
                >
                  <div className="project-identity">
                    <span className="project-avatar" aria-hidden="true">
                      {project.name.slice(0, 1).toUpperCase()}
                    </span>
                    <div>
                      <h3>{project.name}</h3>
                      <p>
                        {project.summary || projectHost(project.canonicalUrl)}
                      </p>
                    </div>
                  </div>
                  <span className="tag">{categories[project.category]}</span>
                  <span className="trend-summary">
                    {primaryAttention(trend.attention?.metrics ?? []).map(
                      (metric) => (
                        <span className="attention-badge" key={metric.source}>
                          {attentionBadge(metric)}
                        </span>
                      ),
                    )}
                    {(trend.attention?.sourceWeeks ?? []).map((week) => (
                      <small key={week.sourceRecordId}>
                        来源本周 +{week.total}
                      </small>
                    ))}
                    {trend.score == null ? (
                      <small>
                        {trend.attention?.metrics.length
                          ? "增速待积累"
                          : "暂无来源观测"}
                      </small>
                    ) : (
                      <>
                        <strong>{trend.score.toFixed(1)}</strong>
                        {trend.growth.delta != null && (
                          <small>
                            净增 {trend.growth.delta > 0 ? "+" : ""}
                            {trend.growth.delta}
                          </small>
                        )}
                      </>
                    )}
                  </span>
                  <span aria-hidden="true" className="row-arrow">
                    ↗
                  </span>
                </a>
              ))}
            </div>
            <nav className="pagination" aria-label="项目分页">
              <span>
                第 {page} / {data.pagination.totalPages} 页
              </span>
              <div>
                <button
                  className="secondary"
                  disabled={page <= 1}
                  onClick={() => setPage((v) => v - 1)}
                >
                  上一页
                </button>
                <button
                  className="secondary"
                  disabled={page >= data.pagination.totalPages}
                  onClick={() => setPage((v) => v + 1)}
                >
                  下一页
                </button>
              </div>
            </nav>
          </>
        ) : (
          <div className="empty-state">
            <div className="empty-radar" aria-hidden="true">
              <span />
            </div>
            <h3>
              {query || category || source
                ? "没有匹配的项目"
                : "先采集一次，看看最近受关注的项目"}
            </h3>
            <p>
              {query || category || source
                ? "换一个关键词或方向，继续寻找。"
                : "在信息源设置里点「立即采集」，GitHub 和 Hacker News 无需凭据即可使用。也可以手动记录一个你已经感兴趣的项目。"}
            </p>
            {!query && !category && !source && (
              <div className="empty-actions">
                <a className="primary" href="/settings">
                  前往信息源设置
                </a>
                <button
                  className="secondary"
                  onClick={() => setCreating(true)}
                  disabled={creating}
                >
                  手动记录项目
                </button>
              </div>
            )}
          </div>
        )}
      </section>
      <section className="research-guide" aria-labelledby="guide-title">
        <div className="section-heading">
          <h2 id="guide-title">让每次发现都有下一步</h2>
          <span className="muted">研究深度，由决策决定</span>
        </div>
        <div className="guide-steps">
          <article>
            <span className="step-number">01</span>
            <div>
              <h3>3 分钟 · 快速认识</h3>
              <p>是什么、解决什么、为什么关注、与我有什么关系。</p>
            </div>
          </article>
          <article>
            <span className="step-number">02</span>
            <div>
              <h3>30 分钟 · 六维分析</h3>
              <p>需求、产品、技术、增长、商业与竞争。</p>
            </div>
          </article>
          <article>
            <span className="step-number">03</span>
            <div>
              <h3>3 小时 · 亲手验证</h3>
              <p>实际体验，记录证据，形成下一步行动。</p>
            </div>
          </article>
        </div>
      </section>
    </>
  );
}
