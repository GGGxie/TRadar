import { useEffect, useState } from "react";
import {
  formatDate,
  metricLabels,
  request,
  sourceLabels,
  type AttentionMetric,
  type Page,
  type Resource,
  type SourceRecord,
  type TrendResult,
} from "./api";

// 展示自动来源的事实层：来源记录、各指标最新累计值和趋势状态。
// 当前关注度与增速分列，不把累计量写成增长。
export function SignalPanel({ projectId }: { projectId: string }) {
  const [trend, setTrend] = useState<TrendResult | null>(null);
  const [sources, setSources] = useState<SourceRecord[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    const id = encodeURIComponent(projectId);
    Promise.all([
      request<Resource<TrendResult>>(`/projects/${id}/trend?window=24h`, {
        signal: controller.signal,
      }),
      request<Page<SourceRecord>>(
        `/projects/${id}/sources?page=1&pageSize=100`,
        {
          signal: controller.signal,
        },
      ),
    ])
      .then(([trendResult, sourceResult]) => {
        setTrend(trendResult.data);
        setSources(sourceResult.data);
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(error.message);
      });
    return () => controller.abort();
  }, [projectId]);

  const metricsByRecord = new Map<string, AttentionMetric[]>();
  for (const metric of trend?.attention?.metrics ?? []) {
    metricsByRecord.set(metric.sourceRecordId, [
      ...(metricsByRecord.get(metric.sourceRecordId) ?? []),
      metric,
    ]);
  }

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
              {sources.map((record) => (
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
                  </div>
                  {record.excerpt && <p>{record.excerpt}</p>}
                  {(metricsByRecord.get(record.id) ?? []).length > 0 && (
                    <dl className="signal-metrics">
                      {metricsByRecord.get(record.id)!.map((metric) => (
                        <div key={metric.metric}>
                          <dt>
                            {metricLabels[metric.metric] ?? metric.metric}
                          </dt>
                          <dd>{metric.value.toLocaleString("en-US")}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                  <small className="muted">
                    采集于 {formatDate(record.fetchedAt)}
                    {record.publishedAt &&
                      ` · 发布于 ${formatDate(record.publishedAt)}`}
                  </small>
                </li>
              ))}
            </ul>
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
