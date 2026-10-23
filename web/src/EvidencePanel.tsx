import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  evidenceKinds,
  formatDate,
  request,
  saveEvidence,
  type Evidence,
  type EvidenceInput,
  type Page,
} from "./api";

const emptyDraft: EvidenceInput = {
  url: "",
  title: "",
  excerpt: "",
  kind: "PUBLIC_EXCERPT",
  isPublic: false,
};

export function EvidencePanel({
  projectId,
  onChanged,
  refreshKey = 0,
}: {
  projectId: string;
  onChanged?: () => void;
  // 其他面板（如来源派生）新增证据后递增，触发重新读取。
  refreshKey?: number;
}) {
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Page<Evidence> | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [draft, setDraft] = useState<EvidenceInput | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const intent = useRef(crypto.randomUUID());
  const dirty = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    request<Page<Evidence>>(
      `/projects/${encodeURIComponent(projectId)}/evidence?page=${page}&pageSize=20`,
      { signal: controller.signal },
    )
      .then(setResult)
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [projectId, page, refreshKey]);

  useEffect(() => {
    const protect = (event: BeforeUnloadEvent) => {
      if (draft && dirty.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, [draft]);

  function start(seed?: Evidence) {
    setSaveError("");
    dirty.current = false;
    intent.current = crypto.randomUUID();
    setDraft(
      seed
        ? {
            url: seed.url,
            title: seed.title,
            excerpt: seed.excerpt,
            kind:
              seed.kind === "METRIC_OBSERVATION" ? "PUBLIC_EXCERPT" : seed.kind,
            publisher: seed.publisher ?? undefined,
            publishedAt: seed.publishedAt ?? undefined,
            isPublic: seed.isPublic,
            supersedesId: seed.id,
          }
        : { ...emptyDraft },
    );
  }

  function change<K extends keyof EvidenceInput>(
    field: K,
    value: EvidenceInput[K],
  ) {
    setDraft((current) => (current ? { ...current, [field]: value } : current));
    dirty.current = true;
    intent.current = crypto.randomUUID();
  }

  function cancel() {
    if (
      !dirty.current ||
      window.confirm("有尚未保存的证据内容，确定放弃吗？")
    ) {
      dirty.current = false;
      setDraft(null);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!draft || saving) return;
    setSaving(true);
    setSaveError("");
    try {
      const input = {
        ...draft,
        publisher: draft.publisher?.trim() || undefined,
        publishedAt: draft.publishedAt?.trim() || undefined,
      };
      const saved = await saveEvidence(projectId, input, intent.current);
      dirty.current = false;
      setDraft(null);
      onChanged?.();
      if (page !== 1) {
        setPage(1);
        return;
      }
      setResult((current) => {
        if (!current) {
          return {
            data: [saved.data],
            pagination: {
              page: 1,
              pageSize: 20,
              totalItems: 1,
              totalPages: 1,
            },
          };
        }
        if (current.pagination.page !== 1) return current;
        return {
          data: [saved.data, ...current.data].slice(0, 20),
          pagination: {
            ...current.pagination,
            totalItems: current.pagination.totalItems + 1,
            totalPages: Math.ceil((current.pagination.totalItems + 1) / 20),
          },
        };
      });
    } catch (problem) {
      setSaveError(problem instanceof Error ? problem.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="evidence-card" aria-labelledby="evidence-title">
      <div className="section-heading">
        <div>
          <p className="eyebrow">EVIDENCE</p>
          <h2 id="evidence-title">证据记录</h2>
        </div>
        <button
          className="secondary"
          onClick={() => start()}
          disabled={!!draft || loading}
        >
          记录证据
        </button>
      </div>
      {draft && (
        <form className="evidence-form" onSubmit={submit}>
          {draft.supersedesId && (
            <p className="revision-note">正在补充修订；原版本会继续保留。</p>
          )}
          <div className="form-grid">
            <label>
              <span id="evidence-url-label">证据链接</span>
              <input
                aria-labelledby="evidence-url-label"
                type="url"
                value={draft.url}
                maxLength={4096}
                required
                disabled={saving}
                onChange={(event) => change("url", event.target.value)}
              />
            </label>
            <label>
              <span id="evidence-title-label">证据标题</span>
              <input
                aria-labelledby="evidence-title-label"
                value={draft.title}
                maxLength={500}
                required
                disabled={saving}
                onChange={(event) => change("title", event.target.value)}
              />
            </label>
            <label>
              <span id="evidence-kind-label">证据类型</span>
              <select
                aria-labelledby="evidence-kind-label"
                value={draft.kind}
                disabled={saving}
                onChange={(event) =>
                  change("kind", event.target.value as EvidenceInput["kind"])
                }
              >
                {Object.entries(evidenceKinds).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span id="evidence-publisher-label">发布者（可选）</span>
              <input
                aria-labelledby="evidence-publisher-label"
                value={draft.publisher ?? ""}
                maxLength={200}
                disabled={saving}
                onChange={(event) => change("publisher", event.target.value)}
              />
            </label>
            <label>
              <span id="evidence-published-label">发布时间（可选）</span>
              <input
                aria-labelledby="evidence-published-label"
                value={draft.publishedAt ?? ""}
                placeholder="例如 2026-10-14T08:00:00Z"
                disabled={saving}
                onChange={(event) => change("publishedAt", event.target.value)}
              />
            </label>
          </div>
          <label>
            <span id="evidence-excerpt-label">原文摘录</span>
            <textarea
              aria-labelledby="evidence-excerpt-label"
              value={draft.excerpt}
              maxLength={16000}
              rows={5}
              required
              disabled={saving}
              onChange={(event) => change("excerpt", event.target.value)}
            />
          </label>
          <label className="visibility-choice">
            <input
              type="checkbox"
              checked={draft.isPublic}
              disabled={saving}
              onChange={(event) => change("isPublic", event.target.checked)}
            />
            标记为公开证据，可供以后启用的模型分析使用
          </label>
          {saveError && (
            <p role="alert" className="error">
              {saveError} 内容已保留，可以直接重试。
            </p>
          )}
          <div className="form-actions">
            <button
              type="button"
              className="text-button"
              onClick={cancel}
              disabled={saving}
            >
              取消
            </button>
            <button className="primary" disabled={saving}>
              {saving ? "正在保存…" : "保存证据"}
            </button>
          </div>
        </form>
      )}
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : loading ? (
        <p role="status" className="loading">
          正在读取证据…
        </p>
      ) : !result?.data.length ? (
        <div className="evidence-empty">
          <h3>还没有证据记录</h3>
          <p>把定价页、发布说明或访谈原文保存下来，再形成判断。</p>
        </div>
      ) : (
        <div className="evidence-list">
          {result.data.map((item) => (
            <article
              key={item.id}
              className={item.supersededBy ? "superseded" : undefined}
            >
              <div className="evidence-meta">
                <span className="tag">
                  {item.kind === "METRIC_OBSERVATION"
                    ? "指标观测"
                    : evidenceKinds[item.kind]}
                </span>
                {item.supersededBy ? (
                  <span className="tag version-tag">已被新版本替代</span>
                ) : (
                  item.supersedesId && (
                    <span className="tag version-tag">修订版</span>
                  )
                )}
                <span>{item.isPublic ? "公开" : "私密"}</span>
                {item.publisher && <span>{item.publisher}</span>}
                {item.publishedAt && (
                  <span>发布于 {formatDate(item.publishedAt)}</span>
                )}
                <time>{formatDate(item.collectedAt)}</time>
              </div>
              <h3>{item.title}</h3>
              <blockquote>{item.excerpt}</blockquote>
              <div className="evidence-actions">
                <a href={item.url} target="_blank" rel="noopener noreferrer">
                  查看原始来源 ↗
                </a>
                {item.kind !== "METRIC_OBSERVATION" && !item.supersededBy && (
                  <button
                    className="text-button"
                    onClick={() => start(item)}
                    disabled={!!draft}
                  >
                    补充修订
                  </button>
                )}
              </div>
            </article>
          ))}
          <nav className="pagination" aria-label="证据分页">
            <span>
              第 {page} / {result.pagination.totalPages} 页 · 共{" "}
              {result.pagination.totalItems} 条
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
                disabled={page >= result.pagination.totalPages}
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
