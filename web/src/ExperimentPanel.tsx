import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ApiError,
  formatDate,
  getExperiment,
  listExperiments,
  request,
  saveExperiment,
  type Evidence,
  type Experiment,
  type ExperimentFields,
  type ExperimentOutcome,
  type ExperimentStatus,
  type Page,
  type Project,
} from "./api";

const statusLabels: Record<ExperimentStatus, string> = {
  PLANNED: "计划中",
  RUNNING: "进行中",
  COMPLETED: "已完成",
  CANCELLED: "已取消",
};

const outcomeLabels: Record<ExperimentOutcome, string> = {
  SUPPORTED: "支持假设",
  REFUTED: "推翻假设",
  INCONCLUSIVE: "证据不足",
};

const emptyExperiment: ExperimentFields = {
  title: "",
  hypothesis: "",
  method: "",
  status: "PLANNED",
  timeBudgetMinutes: 180,
  actualMinutes: null,
  result: "",
  outcome: null,
  evidenceIds: [],
  artifactUrls: [],
  nextAction: "",
};

function parseURLs(value: string) {
  return value
    .split(/\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function ExperimentPanel({
  project,
  evidenceRevision,
}: {
  project: Project;
  evidenceRevision: number;
}) {
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Page<Experiment> | null>(null);
  const [evidence, setEvidence] = useState<Evidence[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [evidenceError, setEvidenceError] = useState("");
  const [draft, setDraft] = useState<ExperimentFields | null>(null);
  const [editing, setEditing] = useState<Experiment | null>(null);
  const [artifactText, setArtifactText] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [message, setMessage] = useState("");
  const [conflict, setConflict] = useState<Experiment | null>(null);
  const intent = useRef(crypto.randomUUID());
  const dirty = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    listExperiments(project.id, page, controller.signal)
      .then(setResult)
      .catch((problem) => {
        if (!controller.signal.aborted)
          setError(
            problem instanceof Error ? problem.message : "读取实践记录失败",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [page, project.id]);

  useEffect(() => {
    const controller = new AbortController();
    request<Page<Evidence>>(
      `/projects/${encodeURIComponent(project.id)}/evidence?page=1&pageSize=100&current=true`,
      { signal: controller.signal },
    )
      .then((response) => {
        setEvidence(response.data);
        setEvidenceError("");
      })
      .catch((problem) => {
        if (!controller.signal.aborted)
          setEvidenceError(
            problem instanceof Error ? problem.message : "读取证据失败",
          );
      });
    return () => controller.abort();
  }, [evidenceRevision, project.id]);

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

  function start(item?: Experiment) {
    setDraft(
      item
        ? {
            title: item.title,
            hypothesis: item.hypothesis,
            method: item.method,
            status: item.status,
            timeBudgetMinutes: item.timeBudgetMinutes,
            actualMinutes: item.actualMinutes,
            result: item.result,
            outcome: item.outcome,
            evidenceIds: [...item.evidenceIds],
            artifactUrls: [...item.artifactUrls],
            nextAction: item.nextAction,
          }
        : { ...emptyExperiment, evidenceIds: [], artifactUrls: [] },
    );
    setEditing(item ?? null);
    setArtifactText(item?.artifactUrls.join("\n") ?? "");
    setConflict(null);
    setSaveError("");
    setMessage("");
    dirty.current = false;
    intent.current = crypto.randomUUID();
  }

  function change<K extends keyof ExperimentFields>(
    field: K,
    value: ExperimentFields[K],
  ) {
    setDraft((current) => (current ? { ...current, [field]: value } : current));
    dirty.current = true;
    setSaveError("");
    if (!editing) intent.current = crypto.randomUUID();
  }

  function changeArtifacts(value: string) {
    setArtifactText(value);
    dirty.current = true;
    setSaveError("");
    if (!editing) intent.current = crypto.randomUUID();
  }

  function toggleEvidence(id: string, checked: boolean) {
    if (!draft) return;
    change(
      "evidenceIds",
      checked
        ? [...draft.evidenceIds, id]
        : draft.evidenceIds.filter((item) => item !== id),
    );
  }

  function cancel() {
    if (
      !dirty.current ||
      window.confirm("有尚未保存的实践内容，确定放弃吗？")
    ) {
      dirty.current = false;
      setDraft(null);
      setEditing(null);
      setConflict(null);
      setSaveError("");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!draft || saving) return;
    setSaving(true);
    setSaveError("");
    setMessage("");
    const fields: ExperimentFields = {
      ...draft,
      artifactUrls: parseURLs(artifactText),
    };
    try {
      const saved = await saveExperiment(
        project.id,
        fields,
        intent.current,
        editing ?? undefined,
      );
      setResult((current) => {
        if (!current) return current;
        if (editing) {
          return {
            ...current,
            data: current.data.map((item) =>
              item.id === saved.data.id ? saved.data : item,
            ),
          };
        }
        if (current.pagination.page !== 1) return current;
        const totalItems = current.pagination.totalItems + 1;
        return {
          data: [saved.data, ...current.data].slice(0, 20),
          pagination: {
            ...current.pagination,
            totalItems,
            totalPages: Math.ceil(totalItems / 20),
          },
        };
      });
      dirty.current = false;
      setDraft(null);
      setEditing(null);
      setConflict(null);
      setMessage("实践记录已保存");
      if (!editing && page !== 1) setPage(1);
    } catch (problem) {
      if (problem instanceof ApiError && problem.status === 412 && editing) {
        try {
          const latest = await getExperiment(editing.id);
          setEditing(latest.data);
          setConflict(latest.data);
          setSaveError(
            "服务器上的实践记录已经变化。你的本地内容仍保留；请对照下方最新版本手工合并，再次保存时会基于最新版本提交。",
          );
        } catch {
          setSaveError(
            "服务器上的实践记录已经变化。你的本地内容仍保留，但暂时无法读取最新版本，请稍后重试。",
          );
        }
      } else {
        setSaveError(
          problem instanceof Error ? problem.message : "保存实践记录失败",
        );
      }
    } finally {
      setSaving(false);
    }
  }

  const evidenceByID = new Map(evidence.map((item) => [item.id, item]));

  return (
    <section className="experiment-card" aria-labelledby="experiment-title">
      <div className="section-heading experiment-heading">
        <div>
          <p className="eyebrow">PRACTICE VALIDATION</p>
          <h2 id="experiment-title">实践验证</h2>
          <p className="section-description">
            用约 3
            小时记录假设、方法和真实结果。系统只保存记录，不会安装、运行或访问陌生项目。
          </p>
        </div>
        <button
          className="secondary"
          disabled={!!draft || loading}
          onClick={() => start()}
        >
          新建实践
        </button>
      </div>
      {draft && (
        <form className="experiment-form" onSubmit={submit}>
          <div className="experiment-form-grid">
            <label className="experiment-title-field">
              <span>实践标题</span>
              <input
                required
                maxLength={200}
                value={draft.title}
                disabled={saving}
                onChange={(event) => change("title", event.target.value)}
              />
            </label>
            <label>
              <span>实践状态</span>
              <select
                value={draft.status}
                disabled={saving}
                onChange={(event) =>
                  change("status", event.target.value as ExperimentStatus)
                }
              >
                {Object.entries(statusLabels).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>时间预算（分钟）</span>
              <input
                type="number"
                min="1"
                max="10080"
                required
                value={draft.timeBudgetMinutes}
                disabled={saving}
                onChange={(event) =>
                  change("timeBudgetMinutes", Number(event.target.value))
                }
              />
            </label>
            <label>
              <span>实际用时（分钟，可选）</span>
              <input
                type="number"
                min="0"
                max="1000000"
                value={draft.actualMinutes ?? ""}
                disabled={saving}
                onChange={(event) =>
                  change(
                    "actualMinutes",
                    event.target.value === ""
                      ? null
                      : Number(event.target.value),
                  )
                }
              />
            </label>
            <label>
              <span>结论</span>
              <select
                aria-label="实践结论"
                value={draft.outcome ?? ""}
                required={draft.status === "COMPLETED"}
                disabled={saving}
                onChange={(event) =>
                  change(
                    "outcome",
                    (event.target.value || null) as ExperimentOutcome | null,
                  )
                }
              >
                <option value="">尚未形成结论</option>
                {Object.entries(outcomeLabels).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label>
            <span>假设</span>
            <textarea
              rows={3}
              maxLength={10000}
              value={draft.hypothesis}
              disabled={saving}
              onChange={(event) => change("hypothesis", event.target.value)}
            />
          </label>
          <label>
            <span>验证方法</span>
            <textarea
              rows={4}
              maxLength={10000}
              value={draft.method}
              disabled={saving}
              onChange={(event) => change("method", event.target.value)}
            />
          </label>
          <label>
            <span>结果记录</span>
            <textarea
              rows={5}
              maxLength={10000}
              required={draft.status === "COMPLETED"}
              value={draft.result}
              disabled={saving}
              onChange={(event) => change("result", event.target.value)}
            />
          </label>
          <label>
            <span>下一步行动</span>
            <textarea
              rows={3}
              maxLength={2000}
              value={draft.nextAction}
              disabled={saving}
              onChange={(event) => change("nextAction", event.target.value)}
            />
          </label>
          <fieldset className="experiment-evidence">
            <legend>证据引用</legend>
            {evidenceError ? (
              <p role="alert" className="error">
                {evidenceError}
              </p>
            ) : !evidence.length ? (
              <p>当前项目还没有可引用证据，可先保存实践草稿。</p>
            ) : (
              evidence.map((item) => (
                <label key={item.id}>
                  <input
                    type="checkbox"
                    aria-label={`引用证据：${item.title}`}
                    checked={draft.evidenceIds.includes(item.id)}
                    disabled={saving}
                    onChange={(event) =>
                      toggleEvidence(item.id, event.target.checked)
                    }
                  />
                  <span>
                    {item.title}
                    <small>{item.isPublic ? "公开" : "私密"}</small>
                  </span>
                </label>
              ))
            )}
          </fieldset>
          <label>
            <span>产物链接（每行一个）</span>
            <textarea
              rows={3}
              value={artifactText}
              disabled={saving}
              placeholder="https://example.com/prototype"
              onChange={(event) => changeArtifacts(event.target.value)}
            />
          </label>
          <p className="draft-note">
            产物链接只会被保存和显示，系统不会访问链接或执行其中的内容。关闭页面前的未保存草稿不保证保留。
          </p>
          {saveError && (
            <p role="alert" className="error">
              {saveError}
            </p>
          )}
          {conflict && (
            <aside
              className="experiment-conflict"
              aria-labelledby="experiment-conflict-title"
            >
              <h3 id="experiment-conflict-title">
                服务器最新版本（第 {conflict.revision} 版）
              </h3>
              <p>
                {statusLabels[conflict.status]} · 时间预算{" "}
                {conflict.timeBudgetMinutes} 分钟
              </p>
              <dl>
                <div>
                  <dt>假设</dt>
                  <dd>{conflict.hypothesis || "空"}</dd>
                </div>
                <div>
                  <dt>结果</dt>
                  <dd>{conflict.result || "空"}</dd>
                </div>
                <div>
                  <dt>下一步</dt>
                  <dd>{conflict.nextAction || "空"}</dd>
                </div>
              </dl>
            </aside>
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
              {saving ? "正在保存…" : "保存实践"}
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
          正在读取实践记录…
        </p>
      ) : !result?.data.length ? (
        <div className="experiment-empty">
          <h3>还没有实践记录</h3>
          <p>选择一个可验证的假设，记录实际体验或小型原型结果。</p>
        </div>
      ) : (
        <div className="experiment-list">
          {result.data.map((item) => (
            <article key={item.id}>
              <header>
                <div>
                  <p className="eyebrow">
                    {statusLabels[item.status]}
                    {item.outcome ? ` / ${outcomeLabels[item.outcome]}` : ""}
                  </p>
                  <h3>{item.title}</h3>
                </div>
                <button
                  className="secondary"
                  disabled={!!draft}
                  onClick={() => start(item)}
                >
                  编辑实践
                </button>
              </header>
              <div className="experiment-meta">
                <span>预算 {item.timeBudgetMinutes} 分钟</span>
                <span>
                  实际用时
                  {item.actualMinutes === null
                    ? "待记录"
                    : ` ${item.actualMinutes} 分钟`}
                </span>
                <time>更新于 {formatDate(item.updatedAt)}</time>
              </div>
              <div className="experiment-copy">
                <div>
                  <strong>假设</strong>
                  <p>{item.hypothesis || "尚未写下假设。"}</p>
                </div>
                <div>
                  <strong>方法</strong>
                  <p>{item.method || "尚未记录验证方法。"}</p>
                </div>
                <div>
                  <strong>结果</strong>
                  <p>{item.result || "实践结果尚未填写。"}</p>
                </div>
                <div>
                  <strong>下一步</strong>
                  <p>{item.nextAction || "尚未安排下一步。"}</p>
                </div>
              </div>
              {!!item.evidenceIds.length && (
                <div className="experiment-links">
                  <strong>引用证据</strong>
                  {item.evidenceIds.map((id) => {
                    const itemEvidence = evidenceByID.get(id);
                    return itemEvidence ? (
                      <a
                        key={id}
                        href={itemEvidence.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {itemEvidence.title} ↗
                      </a>
                    ) : (
                      <span key={id}>证据 {id}</span>
                    );
                  })}
                </div>
              )}
              {!!item.artifactUrls.length && (
                <div className="experiment-links">
                  <strong>实践产物</strong>
                  {item.artifactUrls.map((url) => (
                    <a
                      key={url}
                      href={url}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      {url} ↗
                    </a>
                  ))}
                </div>
              )}
            </article>
          ))}
          {result.pagination.totalPages > 1 && (
            <nav className="pagination" aria-label="实践记录分页">
              <span>
                第 {page} / {result.pagination.totalPages} 页 · 共{" "}
                {result.pagination.totalItems} 条
              </span>
              <div>
                <button
                  className="secondary"
                  disabled={page <= 1 || !!draft}
                  onClick={() => setPage((value) => value - 1)}
                >
                  上一页
                </button>
                <button
                  className="secondary"
                  disabled={page >= result.pagination.totalPages || !!draft}
                  onClick={() => setPage((value) => value + 1)}
                >
                  下一页
                </button>
              </div>
            </nav>
          )}
        </div>
      )}
      {message && (
        <p role="status" className="success-message">
          {message}
        </p>
      )}
    </section>
  );
}
