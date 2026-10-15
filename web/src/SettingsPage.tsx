import { useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  formatDate,
  listIngestionRuns,
  listSources,
  triggerIngestion,
  updateSource,
  type IngestionRun,
  type RemoteSource,
  type SourceConfig,
  type SourceView,
} from "./api";

const sourceNames: Record<RemoteSource, string> = {
  GITHUB: "GitHub",
  HACKER_NEWS: "Hacker News",
  PRODUCT_HUNT: "Product Hunt",
};

const sourceDescriptions: Record<RemoteSource, string> = {
  GITHUB: "GitHub API 候选发现",
  HACKER_NEWS: "Top / New / Show 社区信号",
  PRODUCT_HUNT: "Product Hunt 新产品发布",
};

const statusNames: Record<IngestionRun["status"], string> = {
  QUEUED: "等待执行",
  RUNNING: "采集中",
  RETRY_WAIT: "等待重试",
  SUCCEEDED: "采集成功",
  SUCCEEDED_EMPTY: "成功，无新结果",
  PARTIAL: "部分成功",
  FAILED: "采集失败",
  INTERRUPTED: "已中断",
};

const activeStatuses = new Set(["QUEUED", "RUNNING", "RETRY_WAIT"]);

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

export function SettingsPage() {
  const [sources, setSources] = useState<SourceView[]>([]);
  const [runs, setRuns] = useState<IngestionRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      listSources(controller.signal),
      listIngestionRuns(controller.signal),
    ])
      .then(([sourcePage, runPage]) => {
        setSources(sourcePage.data);
        setRuns(runPage.data);
        setError("");
      })
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === "AbortError"))
          setError(errorMessage(reason));
      })
      .finally(() => setLoading(false));
    return () => controller.abort();
  }, []);

  const hasActiveRun = runs.some((run) => activeStatuses.has(run.status));
  useEffect(() => {
    if (!hasActiveRun) return;
    const timer = window.setInterval(() => {
      listIngestionRuns()
        .then((page) => setRuns(page.data))
        .catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [hasActiveRun]);

  function replaceSource(updated: SourceView) {
    setSources((current) =>
      current.map((source) =>
        source.source === updated.source ? updated : source,
      ),
    );
  }

  function addRun(run: IngestionRun) {
    setRuns((current) => [
      run,
      ...current.filter((item) => item.id !== run.id),
    ]);
  }

  return (
    <section className="settings-page" aria-labelledby="settings-title">
      <div className="page-heading">
        <div>
          <p className="eyebrow">SOURCE OPERATIONS</p>
          <h1 id="settings-title">信息源与采集</h1>
          <p className="intro">
            配置发现范围、采集节奏和本地预算。任务结果保存在本地，失败不会清空已有观测。
          </p>
        </div>
      </div>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {loading ? (
        <p className="loading">正在读取来源状态…</p>
      ) : (
        <div className="source-grid">
          {sources.map((source) => (
            <SourceCard
              key={source.source}
              source={source}
              activeRun={runs.find(
                (run) =>
                  run.source === source.source &&
                  activeStatuses.has(run.status),
              )}
              onUpdated={replaceSource}
              onRun={addRun}
            />
          ))}
        </div>
      )}
      <RunHistory runs={runs} />
    </section>
  );
}

function SourceCard({
  source,
  activeRun,
  onUpdated,
  onRun,
}: {
  source: SourceView;
  activeRun?: IngestionRun;
  onUpdated: (source: SourceView) => void;
  onRun: (run: IngestionRun) => void;
}) {
  const [draft, setDraft] = useState<SourceConfig>({ ...source.config });
  const [saving, setSaving] = useState(false);
  const [triggering, setTriggering] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const retryKey = useRef<string | null>(null);
  const name = sourceNames[source.source];

  useEffect(() => {
    setDraft({ ...source.config });
  }, [source.revision, source.config]);

  const availability = !draft.isEnabled
    ? "已停用"
    : source.availability === "MISSING_CREDENTIALS"
      ? "缺少凭据"
      : "可用";
  const hasUnsavedChanges =
    JSON.stringify(draft) !== JSON.stringify(source.config);

  function set<K extends keyof SourceConfig>(key: K, value: SourceConfig[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function save() {
    setSaving(true);
    setError("");
    setMessage("");
    const patch: Partial<Omit<SourceConfig, "source">> = {
      isEnabled: draft.isEnabled,
      isScheduleEnabled: draft.isScheduleEnabled,
      intervalMinutes: draft.intervalMinutes,
    };
    if (source.source === "GITHUB") {
      patch.keywords = draft.keywords;
      patch.discoveryDays = draft.discoveryDays;
      patch.candidatesPerKeyword = draft.candidatesPerKeyword;
      patch.requestBudgetPerHour = draft.requestBudgetPerHour;
    } else if (source.source === "HACKER_NEWS") {
      patch.feeds = draft.feeds;
      patch.itemsPerFeed = draft.itemsPerFeed;
    } else {
      patch.discoveryDays = draft.discoveryDays;
      patch.maxPages = draft.maxPages;
    }
    try {
      const response = await updateSource(
        source.source,
        source.revision,
        patch,
      );
      onUpdated(response.data);
      setMessage("设置已保存");
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setSaving(false);
    }
  }

  async function trigger() {
    retryKey.current ??= intentKey();
    setTriggering(true);
    setError("");
    setMessage("");
    try {
      const response = await triggerIngestion(source.source, retryKey.current);
      retryKey.current = null;
      onRun(response.data);
      setMessage("采集任务已进入队列");
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setTriggering(false);
    }
  }

  const cannotTrigger =
    !draft.isEnabled ||
    source.availability === "MISSING_CREDENTIALS" ||
    hasUnsavedChanges ||
    Boolean(activeRun) ||
    triggering;

  return (
    <article
      className="source-card"
      aria-labelledby={`source-${source.source}`}
    >
      <header className="source-card-header">
        <div>
          <p className="eyebrow">{source.source.replaceAll("_", " ")}</p>
          <h2 id={`source-${source.source}`}>
            {sourceDescriptions[source.source]}
          </h2>
        </div>
        <span
          className={`source-status ${availability === "可用" ? "available" : ""}`}
        >
          {availability}
        </span>
      </header>

      <div className="source-facts">
        <span>{source.hasCredentials ? "已检测到凭据" : "无凭据模式"}</span>
        <span>
          {source.budget.remainingRequests == null
            ? "预算尚无观测"
            : source.source === "PRODUCT_HUNT"
              ? `剩余 ${source.budget.remainingRequests} complexity`
              : `剩余 ${source.budget.remainingRequests} 次`}
        </span>
        {source.budget.resetAt && (
          <span>预算恢复：{formatDate(source.budget.resetAt)}</span>
        )}
        <span>
          最近观测：
          {source.lastObservationAt
            ? formatDate(source.lastObservationAt)
            : "尚无"}
        </span>
      </div>
      {source.source === "PRODUCT_HUNT" && !source.hasCredentials && (
        <p className="source-notice">需要 Product Hunt Token 才能采集</p>
      )}
      {source.nextRetryAt && (
        <p className="source-notice">
          来源退避至 {formatDate(source.nextRetryAt)}
        </p>
      )}

      <div className="source-form">
        <label className="toggle-field">
          <input
            type="checkbox"
            checked={draft.isEnabled}
            onChange={(event) => set("isEnabled", event.target.checked)}
          />
          启用 {name}
        </label>
        <label className="toggle-field">
          <input
            type="checkbox"
            checked={draft.isScheduleEnabled}
            onChange={(event) => set("isScheduleEnabled", event.target.checked)}
          />
          按计划自动采集
        </label>
        <label>
          {name} 采集间隔（分钟）
          <input
            type="number"
            min="15"
            max="1440"
            value={draft.intervalMinutes}
            onChange={(event) =>
              set("intervalMinutes", Number(event.target.value))
            }
          />
        </label>
        {source.source === "GITHUB" && (
          <GitHubFields
            draft={draft}
            set={set}
            hasCredentials={source.hasCredentials}
          />
        )}
        {source.source === "HACKER_NEWS" && (
          <HackerNewsFields draft={draft} set={set} />
        )}
        {source.source === "PRODUCT_HUNT" && (
          <ProductHuntFields draft={draft} set={set} />
        )}
      </div>

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
      <footer className="source-actions">
        <button className="secondary" disabled={saving} onClick={save}>
          {saving ? "保存中…" : `保存 ${name} 设置`}
        </button>
        <button className="primary" disabled={cannotTrigger} onClick={trigger}>
          {activeRun
            ? statusNames[activeRun.status]
            : hasUnsavedChanges
              ? "请先保存设置"
              : `立即采集 ${name}`}
        </button>
      </footer>
    </article>
  );
}

type FieldSetter = <K extends keyof SourceConfig>(
  key: K,
  value: SourceConfig[K],
) => void;

function GitHubFields({
  draft,
  set,
  hasCredentials,
}: {
  draft: SourceConfig;
  set: FieldSetter;
  hasCredentials: boolean;
}) {
  return (
    <>
      <label className="wide-field">
        关键词（每行一项）
        <textarea
          rows={3}
          value={(draft.keywords ?? []).join("\n")}
          onChange={(event) =>
            set(
              "keywords",
              event.target.value
                .split(/[\n,]/)
                .map((value) => value.trim())
                .filter(Boolean),
            )
          }
        />
      </label>
      <label>
        新项目窗口（天）
        <input
          type="number"
          min="1"
          max="365"
          value={draft.discoveryDays ?? 180}
          onChange={(event) => set("discoveryDays", Number(event.target.value))}
        />
      </label>
      <label>
        每词候选数
        <input
          type="number"
          min="2"
          max="30"
          value={draft.candidatesPerKeyword ?? 10}
          onChange={(event) =>
            set("candidatesPerKeyword", Number(event.target.value))
          }
        />
      </label>
      <label>
        每小时本地预算
        <input
          type="number"
          min="1"
          max={hasCredentials ? 5000 : 60}
          value={draft.requestBudgetPerHour ?? 60}
          onChange={(event) =>
            set("requestBudgetPerHour", Number(event.target.value))
          }
        />
      </label>
    </>
  );
}

function HackerNewsFields({
  draft,
  set,
}: {
  draft: SourceConfig;
  set: FieldSetter;
}) {
  const feeds = draft.feeds ?? [];
  function toggle(feed: string, checked: boolean) {
    set(
      "feeds",
      checked ? [...feeds, feed] : feeds.filter((item) => item !== feed),
    );
  }
  return (
    <>
      <fieldset className="wide-field feed-field">
        <legend>榜单</legend>
        {[
          ["TOP", "Top"],
          ["NEW", "New"],
          ["SHOW", "Show HN"],
        ].map(([value, label]) => (
          <label key={value} className="toggle-field">
            <input
              type="checkbox"
              checked={feeds.includes(value)}
              onChange={(event) => toggle(value, event.target.checked)}
            />
            {label}
          </label>
        ))}
      </fieldset>
      <label>
        每榜读取条数
        <input
          type="number"
          min="1"
          max="50"
          value={draft.itemsPerFeed ?? 20}
          onChange={(event) => set("itemsPerFeed", Number(event.target.value))}
        />
      </label>
    </>
  );
}

function ProductHuntFields({
  draft,
  set,
}: {
  draft: SourceConfig;
  set: FieldSetter;
}) {
  return (
    <>
      <label>
        发布时间窗口（天）
        <input
          type="number"
          min="1"
          max="30"
          value={draft.discoveryDays ?? 7}
          onChange={(event) => set("discoveryDays", Number(event.target.value))}
        />
      </label>
      <label>
        最多页数
        <input
          type="number"
          min="1"
          max="3"
          value={draft.maxPages ?? 3}
          onChange={(event) => set("maxPages", Number(event.target.value))}
        />
      </label>
    </>
  );
}

function RunHistory({ runs }: { runs: IngestionRun[] }) {
  const visibleRuns = useMemo(() => runs.slice(0, 12), [runs]);
  return (
    <section className="run-history" aria-labelledby="run-history-title">
      <div className="section-heading">
        <div>
          <p className="eyebrow">RECENT RUNS</p>
          <h2 id="run-history-title">最近采集任务</h2>
        </div>
        <span>{visibleRuns.length} 条</span>
      </div>
      {visibleRuns.length === 0 ? (
        <p className="run-empty">尚未创建采集任务。</p>
      ) : (
        <div className="run-list">
          {visibleRuns.map((run) => (
            <article className="run-row" key={run.id}>
              <div>
                <strong>{sourceNames[run.source]}</strong>
                <span>{run.trigger === "MANUAL" ? "手动" : "定时"}</span>
              </div>
              <span className={`run-status status-${run.status.toLowerCase()}`}>
                {statusNames[run.status]}
              </span>
              <span>{formatDate(run.createdAt)}</span>
              <span>
                {run.acceptedItems} 条 · {run.observationsWritten} 项观测
              </span>
              {run.errors[0] && (
                <p>
                  {run.errors[0].code}：{run.errors[0].message}
                </p>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
