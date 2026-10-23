import { useEffect, useRef, useState } from "react";
import {
  deriveSourceEvidence,
  formatDate,
  getSource,
  metricLabels,
  request,
  sourceLabels,
  updateSource,
  type AttentionMetric,
  type Page,
  type Resource,
  type SourceRecord,
  type SourceView,
  type SourceWeek,
  type TrendResult,
} from "./api";

const weekdays = ["日", "一", "二", "三", "四", "五", "六"];

// 展示自动来源的事实层：来源记录、各指标最新累计值、来源自然周日桶和趋势状态。
// 当前关注度、来源日级新增与滚动增速分列，不把累计量写成增长。
export function SignalPanel({
  projectId,
  onEvidenceSaved,
}: {
  projectId: string;
  onEvidenceSaved?: () => void;
}) {
  const [deriving, setDeriving] = useState(false);
  const [deriveMessage, setDeriveMessage] = useState("");
  const deriveKey = useRef(crypto.randomUUID());
  const [trend, setTrend] = useState<TrendResult | null>(null);
  const [sources, setSources] = useState<SourceRecord[] | null>(null);
  const [github, setGitHub] = useState<SourceView | null>(null);
  const [error, setError] = useState("");
  const [trackError, setTrackError] = useState("");
  const [tracking, setTracking] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const id = encodeURIComponent(projectId);
    Promise.all([
      request<Resource<TrendResult>>(`/projects/${id}/trend?window=24h`, {
        signal: controller.signal,
      }),
      request<Page<SourceRecord>>(
        `/projects/${id}/sources?page=1&pageSize=100`,
        { signal: controller.signal },
      ),
    ])
      .then(([trendResult, sourceResult]) => {
        setTrend(trendResult.data);
        setSources(sourceResult.data);
        if (sourceResult.data.some((record) => record.source === "GITHUB")) {
          getSource("GITHUB", controller.signal)
            .then((result) => setGitHub(result.data))
            .catch(() => {
              // 跟踪入口是附加能力；读取失败时不影响来源展示。
            });
        }
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(error.message);
      });
    return () => controller.abort();
  }, [projectId]);

  // 来源记录会被采集更新；分析需要引用冻结后的证据副本。
  async function saveAsEvidence() {
    setDeriving(true);
    setDeriveMessage("");
    try {
      const result = await deriveSourceEvidence(projectId, deriveKey.current);
      deriveKey.current = crypto.randomUUID();
      setDeriveMessage(
        result.data.created > 0
          ? `已保存 ${result.data.created} 条来源证据，可在分析时勾选引用。`
          : "来源证据已是最新，没有新增。",
      );
      onEvidenceSaved?.();
    } catch (error) {
      setDeriveMessage(
        error instanceof Error ? error.message : "保存失败，请稍后重试。",
      );
    } finally {
      setDeriving(false);
    }
  }

  async function toggleTracking(recordId: string) {
    if (!github) return;
    const current = github.config.trackedSourceRecordIds ?? [];
    const next = current.includes(recordId)
      ? current.filter((id) => id !== recordId)
      : [...current, recordId];
    setTracking(true);
    setTrackError("");
    try {
      const result = await updateSource("GITHUB", github.revision, {
        trackedSourceRecordIds: next,
      });
      setGitHub(result.data);
    } catch (error) {
      // 版本冲突或校验失败：重新读取最新设置，让用户基于最新版本再操作。
      setTrackError(
        `${error instanceof Error ? error.message : "保存失败"}。已重新读取 GitHub 设置，请再试一次。`,
      );
      getSource("GITHUB")
        .then((result) => setGitHub(result.data))
        .catch(() => {});
    } finally {
      setTracking(false);
    }
  }

  const metricsByRecord = new Map<string, AttentionMetric[]>();
  for (const metric of trend?.attention?.metrics ?? []) {
    metricsByRecord.set(metric.sourceRecordId, [
      ...(metricsByRecord.get(metric.sourceRecordId) ?? []),
      metric,
    ]);
  }
  const weekByRecord = new Map<string, SourceWeek>();
  for (const week of trend?.attention?.sourceWeeks ?? []) {
    weekByRecord.set(week.sourceRecordId, week);
  }
  const trackedIds = github?.config.trackedSourceRecordIds ?? [];

  return (
    <section className="signal-panel" aria-labelledby="signal-title">
      <div className="section-heading">
        <div>
          <p className="eyebrow">SOURCES / SIGNALS</p>
          <h2 id="signal-title">来源与当前关注</h2>
          <p className="muted">
            累计关注量只说明有人在看；增速需要多次观测，商业表现需要单独的证据。
          </p>
        </div>
        {trend && (
          <span className="signal-status">
            {trend.score == null
              ? "增速待积累"
              : `趋势分 ${trend.score.toFixed(1)}`}
          </span>
        )}
      </div>
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !trend || !sources ? (
        <p role="status" className="loading">
          正在读取来源与观测…
        </p>
      ) : (
        <>
          {sources.length === 0 ? (
            <p className="muted signal-empty">
              这个项目还没有自动来源。可在信息源设置中采集，或在下方记录证据。
            </p>
          ) : (
            <ul className="signal-sources">
              {sources.map((record) => {
                const metrics = metricsByRecord.get(record.id) ?? [];
                const week = weekByRecord.get(record.id);
                const isTracked = trackedIds.includes(record.id);
                return (
                  <li key={record.id}>
                    <div className="signal-source-head">
                      <span className="tag">{sourceLabels[record.source]}</span>
                      <a
                        href={record.targetUrl ?? record.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {record.title} <span aria-hidden="true">↗</span>
                      </a>
                      {record.source === "GITHUB" && github && (
                        <button
                          type="button"
                          className="text-button signal-track"
                          disabled={tracking}
                          aria-pressed={isTracked}
                          onClick={() => toggleTracking(record.id)}
                        >
                          {isTracked ? "取消跟踪" : "跟踪这个仓库"}
                        </button>
                      )}
                    </div>
                    {record.excerpt && <p>{record.excerpt}</p>}
                    {metrics.length > 0 && (
                      <dl className="signal-metrics">
                        {metrics.map((metric) => (
                          <div key={metric.metric}>
                            <dt>
                              {metricLabels[metric.metric] ?? metric.metric}
                            </dt>
                            <dd>{metric.value.toLocaleString("en-US")}</dd>
                          </div>
                        ))}
                      </dl>
                    )}
                    {week && <SourceWeekView week={week} />}
                    {record.source === "GITHUB" && isTracked && (
                      <small className="muted">
                        已跟踪：每轮采集都会刷新它的累计值和日级历史，即使它不再出现在搜索结果里。
                      </small>
                    )}
                    <small className="muted">
                      采集于 {formatDate(record.fetchedAt)}
                      {record.publishedAt &&
                        ` · 发布于 ${formatDate(record.publishedAt)}`}
                    </small>
                  </li>
                );
              })}
            </ul>
          )}
          {trackError && (
            <p role="alert" className="error">
              {trackError}
            </p>
          )}
          {sources.length > 0 && (
            <div className="signal-derive">
              <button
                type="button"
                className="secondary"
                disabled={deriving}
                onClick={saveAsEvidence}
              >
                {deriving ? "保存中…" : "保存为证据"}
              </button>
              <small className="muted">
                把当前来源描述和最新累计指标冻结为公开证据，供规则或人工分析引用。
              </small>
              {deriveMessage && <p role="status">{deriveMessage}</p>}
            </div>
          )}
          {trend.score != null && trend.growth.delta != null && (
            <p className="signal-growth">
              24 小时净增 {trend.growth.delta > 0 ? "+" : ""}
              {trend.growth.delta}
            </p>
          )}
          {trend.explanations?.length > 0 && (
            <ul className="signal-explanations">
              {trend.explanations.map((text) => (
                <li key={text}>{text}</li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}

function SourceWeekView({ week }: { week: SourceWeek }) {
  const label = `${sourceLabels[week.source]} 来源本周每日新增 ${metricLabels[week.metric] ?? week.metric}`;
  const peak = Math.max(1, ...week.days.map((value) => value ?? 0));
  const start = formatDate(week.weekStart).split(" ")[0];
  return (
    <div className="signal-week">
      <p>
        <strong>来源本周新增 +{week.total.toLocaleString("en-US")}</strong>
        <span className="muted">
          {" "}
          平台自然周（{start} 起），时区
          {week.timeZone === "UNSPECIFIED" ? "未说明" : ` ${week.timeZone}`}
          ；不计入趋势分
        </span>
      </p>
      <ul aria-label={label}>
        {week.days.map((value, index) => (
          <li key={index}>
            <span
              className={`signal-bar level-${Math.round(((value ?? 0) / peak) * 10)}`}
              aria-hidden="true"
            />
            <span className="signal-day-value">
              {value == null ? "—" : value.toLocaleString("en-US")}
            </span>
            <span className="muted">周{weekdays[index]}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
