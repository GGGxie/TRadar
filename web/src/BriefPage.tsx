import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  createBrief,
  downloadBriefMarkdown,
  getBriefRun,
  getBriefSettings,
  listBriefs,
  updateBriefSettings,
  type BriefReason,
  type BriefSettings,
  type DailyBrief,
  type ResearchDecision,
} from "./api";
import { researchDecisionLabels } from "./ResearchPanel";

const reasonLabels: Record<BriefReason, string> = {
  NEW_DISCOVERY: "新发现",
  FIRST_COVERAGE: "首次覆盖",
  GROWTH_CHANGE: "增长变化",
  NEW_EVIDENCE: "新增证据",
  ANALYSIS_CHANGE: "分析变化",
  RESEARCH_CHANGE: "个人判断变化",
};

const coverageLabels = {
  AVAILABLE: "新鲜",
  STALE: "超过 24 小时",
  NO_DATA: "无数据",
} as const;

function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : "操作未能完成，请稍后重试。";
}

function intentKey() {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`
  );
}

function dateInTimeZone(timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date());
  const values = Object.fromEntries(
    parts.map((part) => [part.type, part.value]),
  );
  return `${values.year}-${values.month}-${values.day}`;
}

export function BriefPage() {
  const [briefs, setBriefs] = useState<DailyBrief[]>([]);
  const [settings, setSettings] = useState<BriefSettings | null>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(0);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const retryKey = useRef<string | null>(null);

  async function load(targetPage: number, signal?: AbortSignal) {
    const [settingsResult, briefResult] = await Promise.all([
      getBriefSettings(signal),
      listBriefs(targetPage, signal),
    ]);
    setSettings(settingsResult.data);
    setBriefs(briefResult.data);
    setTotal(briefResult.pagination.totalItems);
    setTotalPages(briefResult.pagination.totalPages);
  }

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    load(page, controller.signal)
      .then(() => setError(""))
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === "AbortError"))
          setError(errorMessage(reason));
      })
      .finally(() => setLoading(false));
    return () => controller.abort();
  }, [page]);

  async function waitForCompletion(runID: string) {
    for (let attempt = 0; attempt < 60; attempt += 1) {
      const result = await getBriefRun(runID);
      if (result.data.status === "SUCCEEDED") return;
      if (
        result.data.status === "FAILED" ||
        result.data.status === "INTERRUPTED"
      )
        throw new Error("日报任务未能完成，可在刷新后查看任务状态。");
      await new Promise((resolve) => window.setTimeout(resolve, 250));
    }
    throw new Error("日报仍在生成，请稍后刷新查看。");
  }

  async function generate() {
    if (!settings) return;
    retryKey.current ??= intentKey();
    setGenerating(true);
    setError("");
    setMessage("正在冻结当前资料并生成日报…");
    try {
      const result = await createBrief(
        dateInTimeZone(settings.timeZone),
        settings.timeZone,
        retryKey.current,
      );
      await waitForCompletion(result.data.id);
      retryKey.current = null;
      setPage(1);
      await load(1);
      setMessage("日报已生成，当前版本保留生成时的数据快照。");
    } catch (reason) {
      setMessage("");
      setError(errorMessage(reason));
    } finally {
      setGenerating(false);
    }
  }

  return (
    <section className="brief-page" aria-labelledby="brief-title">
      <header className="page-heading brief-heading">
        <div>
          <p className="eyebrow">DAILY SIGNAL BRIEF</p>
          <h1 id="brief-title">每日简报</h1>
          <p className="intro">
            只收录首次覆盖或发生实质变化的项目。每个版本冻结来源、证据和个人判断。
          </p>
        </div>
        <button
          className="primary"
          onClick={generate}
          disabled={!settings || generating}
        >
          <span aria-hidden="true">＋</span>
          {generating ? "生成中…" : "生成今日简报"}
        </button>
      </header>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {message && (
        <p className="success-message" role="status">
          {message}
        </p>
      )}
      {settings && <BriefSettingsForm value={settings} onSaved={setSettings} />}
      {loading ? (
        <p className="loading" role="status">
          正在读取日报…
        </p>
      ) : briefs.length === 0 ? (
        <div className="empty-state brief-empty">
          <div className="empty-radar" aria-hidden="true">
            <span />
          </div>
          <h2>还没有日报</h2>
          <p>生成第一份日报后，这里会保留每次生成的版本和当时使用的数据。</p>
        </div>
      ) : (
        <div className="brief-list">
          {briefs.map((item) => (
            <BriefCard
              key={item.id}
              brief={item}
              previous={briefs.find(
                (candidate) => candidate.id === item.previousBriefId,
              )}
            />
          ))}
          <nav className="pagination" aria-label="日报分页">
            <span>
              第 {page} / {totalPages} 页 · 共 {total} 个版本
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

function BriefSettingsForm({
  value,
  onSaved,
}: {
  value: BriefSettings;
  onSaved: (value: BriefSettings) => void;
}) {
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  useEffect(() => setDraft(value), [value]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(value);

  async function save() {
    setSaving(true);
    setMessage("");
    setError("");
    try {
      const result = await updateBriefSettings(value, {
        isEnabled: draft.isEnabled,
        timeZone: draft.timeZone,
        localTime: draft.localTime,
        maxItems: draft.maxItems,
      });
      onSaved(result.data);
      setMessage("日报设置已保存");
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="brief-settings" aria-labelledby="brief-settings-title">
      <div>
        <p className="eyebrow">AUTOMATION</p>
        <h2 id="brief-settings-title">生成节奏</h2>
        <p>应用运行时会在设定时间生成当天一份；停机后当天首次启动会补一次。</p>
      </div>
      <div className="brief-settings-fields">
        <label className="toggle-field">
          <input
            type="checkbox"
            checked={draft.isEnabled}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                isEnabled: event.target.checked,
              }))
            }
          />
          启用自动日报
        </label>
        <label>
          时区
          <input
            aria-label="日报时区"
            value={draft.timeZone}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                timeZone: event.target.value,
              }))
            }
          />
        </label>
        <label>
          每天时间
          <input
            aria-label="日报生成时间"
            type="time"
            value={draft.localTime}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                localTime: event.target.value,
              }))
            }
          />
        </label>
        <label>
          最多项目
          <input
            aria-label="日报最多项目"
            type="number"
            min={1}
            max={20}
            value={draft.maxItems}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                maxItems: Number(event.target.value),
              }))
            }
          />
        </label>
        <button
          className="secondary"
          disabled={!dirty || saving}
          onClick={save}
        >
          {saving ? "保存中…" : "保存设置"}
        </button>
      </div>
      {message && (
        <p className="success-message" role="status">
          {message}
        </p>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

// 同日重新生成只比较上一版本之后的变化（SPEC-daily-brief DB-03），在版本上写明比较基准（#49）。
function baselineNote(brief: DailyBrief, previous?: DailyBrief) {
  if (!brief.previousBriefId)
    return "首份日报：收录截止前 24 小时内的新发现与首次覆盖的项目。";
  if (previous && previous.date === brief.date)
    return `本版只列出版本 ${previous.version} 之后的新变化；版本 ${previous.version} 中的项目仍保留在下方。`;
  if (previous)
    return `本版只列出上一份日报（${previous.date} 版本 ${previous.version}）之后的新变化。`;
  return "本版只列出上一份日报之后的新变化。";
}

function BriefCard({
  brief,
  previous,
}: {
  brief: DailyBrief;
  previous?: DailyBrief;
}) {
  const [downloadError, setDownloadError] = useState("");
  return (
    <article className="brief-card">
      <header>
        <div>
          <p className="eyebrow">
            {brief.date} · {brief.timeZone}
          </p>
          <h2>
            {brief.date} · 版本 {brief.version}
          </h2>
          <p>
            截止 {brief.cutoffAt} · {brief.selectionVersion} /{" "}
            {brief.rankingVersion}
          </p>
        </div>
        <a
          className="secondary"
          href={`/api/v1/briefs/${encodeURIComponent(brief.id)}/markdown`}
          download
          onClick={(event) => {
            event.preventDefault();
            setDownloadError("");
            downloadBriefMarkdown(brief.id).catch((reason: unknown) =>
              setDownloadError(errorMessage(reason)),
            );
          }}
        >
          下载 Markdown
        </a>
      </header>
      {downloadError && (
        <p className="error" role="alert">
          {downloadError}
        </p>
      )}
      <p className="brief-baseline">{baselineNote(brief, previous)}</p>
      <div className="brief-coverage" aria-label="数据覆盖">
        {brief.coverage.map((item) => (
          <span
            key={item.source}
            className={`coverage-${item.state.toLowerCase()}`}
          >
            {item.source.replaceAll("_", " ")} · {coverageLabels[item.state]}
          </span>
        ))}
      </div>
      {brief.emptyReason ? (
        <div className="brief-empty-copy">
          <strong>
            {brief.emptyReason === "NO_DATA"
              ? "尚无数据"
              : "今天没有新的实质变化"}
          </strong>
          <p>
            {brief.emptyReason === "NO_DATA"
              ? "先录入项目或运行信息源采集，再生成日报。"
              : "已有项目与资料保持不变，因此本版没有重复推荐。"}
          </p>
        </div>
      ) : (
        <div className="brief-items">
          {brief.items.map((item) => (
            <section key={item.projectId} className="brief-item">
              <div className="brief-item-heading">
                <div>
                  <div className="brief-reasons">
                    {item.reasons.map((reason) => (
                      <span key={reason}>{reasonLabels[reason]}</span>
                    ))}
                  </div>
                  <h3>{item.title}</h3>
                  {item.summary && (
                    <p className="brief-summary">{item.summary}</p>
                  )}
                </div>
                <a href={item.url} target="_blank" rel="noreferrer">
                  项目来源 ↗
                </a>
              </div>
              <dl>
                <div>
                  <dt>事实</dt>
                  <dd>{item.factSummary}</dd>
                </div>
                {item.hypothesisSummary && (
                  <div>
                    <dt>已存推测</dt>
                    <dd>{item.hypothesisSummary}</dd>
                  </div>
                )}
                {item.personalDecision && (
                  <div>
                    <dt>个人判断</dt>
                    <dd>
                      {researchDecisionLabels[
                        item.personalDecision as ResearchDecision
                      ] ?? item.personalDecision}
                    </dd>
                  </div>
                )}
                {item.nextAction && (
                  <div>
                    <dt>下一步</dt>
                    <dd>{item.nextAction}</dd>
                  </div>
                )}
              </dl>
              {!!item.evidenceSnapshots.length && (
                <details>
                  <summary>{item.evidenceSnapshots.length} 条冻结证据</summary>
                  <ul>
                    {item.evidenceSnapshots.map((evidence) => (
                      <li key={evidence.id}>
                        <a href={evidence.url} target="_blank" rel="noreferrer">
                          {evidence.title} ↗
                        </a>
                        <span>
                          {evidence.kind} · {evidence.collectedAt}
                        </span>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </section>
          ))}
        </div>
      )}
    </article>
  );
}
