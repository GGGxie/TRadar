import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ApiError,
  formatDate,
  getResearch,
  saveResearch,
  type Project,
  type ResearchDecision,
  type ResearchEntry,
  type ResearchFields,
  type ResearchStage,
} from "./api";

export const researchStageLabels: Record<ResearchStage, string> = {
  SCREENING: "快速初筛",
  DEEP_RESEARCH: "深入研究",
  PRACTICE: "实践验证",
  REVIEWED: "已复盘",
  ARCHIVED: "已归档",
};
export const researchDecisionLabels: Record<ResearchDecision, string> = {
  UNDECIDED: "尚未决定",
  OBSERVE: "继续观察",
  LEARN: "值得学习",
  USE: "准备使用",
  VALIDATE: "需要验证",
  PASS: "暂时放弃",
};

const emptyResearch: ResearchFields = {
  isStarred: false,
  tags: [],
  stage: "SCREENING",
  decision: "UNDECIDED",
  rationale: "",
  notes: "",
  nextAction: "",
  nextActionAt: null,
};

function toLocalInput(value: string | null) {
  if (!value) return null;
  const date = new Date(value);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

function parseTags(value: string) {
  return value
    .split(/[，,\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function ResearchPanel({ project }: { project: Project }) {
  const [entry, setEntry] = useState<ResearchEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<ResearchFields>(emptyResearch);
  const [tagText, setTagText] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [conflict, setConflict] = useState<ResearchEntry | null>(null);
  const [message, setMessage] = useState("");
  const intent = useRef(crypto.randomUUID());
  const dirty = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    getResearch(project.id, controller.signal)
      .then((result) => {
        setEntry(result.data);
        setLoadError("");
      })
      .catch((problem) => {
        if (controller.signal.aborted) return;
        if (problem instanceof ApiError && problem.status === 404) {
          setEntry(null);
          setLoadError("");
          return;
        }
        setLoadError(
          problem instanceof Error ? problem.message : "读取研究记录失败",
        );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [project.id]);

  useEffect(() => {
    const protect = (event: BeforeUnloadEvent) => {
      if (editing && dirty.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, [editing]);

  function begin() {
    setTagText(entry?.tags.join(", ") ?? "");
    setDraft(
      entry
        ? {
            isStarred: entry.isStarred,
            tags: [...entry.tags],
            stage: entry.stage,
            decision: entry.decision,
            rationale: entry.rationale,
            notes: entry.notes,
            nextAction: entry.nextAction,
            nextActionAt: toLocalInput(entry.nextActionAt),
          }
        : { ...emptyResearch, tags: [] },
    );
    dirty.current = false;
    intent.current = crypto.randomUUID();
    setSaveError("");
    setConflict(null);
    setMessage("");
    setEditing(true);
  }

  function change<K extends keyof ResearchFields>(
    field: K,
    value: ResearchFields[K],
  ) {
    setDraft((current) => ({ ...current, [field]: value }));
    dirty.current = true;
    setSaveError("");
    if (!entry) intent.current = crypto.randomUUID();
  }

  function changeTags(value: string) {
    setTagText(value);
    dirty.current = true;
    setSaveError("");
    if (!entry) intent.current = crypto.randomUUID();
  }

  function cancel() {
    if (
      !dirty.current ||
      window.confirm("有尚未保存的个人研究内容，确定放弃吗？")
    ) {
      dirty.current = false;
      setEditing(false);
      setSaveError("");
      setConflict(null);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setSaveError("");
    setMessage("");
    const fields: ResearchFields = {
      ...draft,
      tags: parseTags(tagText),
      nextActionAt: draft.nextActionAt
        ? new Date(draft.nextActionAt).toISOString()
        : null,
    };
    try {
      const saved = await saveResearch(
        project.id,
        fields,
        intent.current,
        entry ?? undefined,
      );
      setEntry(saved.data);
      dirty.current = false;
      setEditing(false);
      setConflict(null);
      setMessage("个人研究已保存");
    } catch (problem) {
      if (problem instanceof ApiError && problem.status === 412) {
        try {
          const latest = await getResearch(project.id);
          setEntry(latest.data);
          setConflict(latest.data);
          setSaveError(
            "服务器上的研究记录已经变化。你的本地内容仍保留；请对照下方最新版本手工合并，再次保存时会基于最新版本提交。",
          );
        } catch {
          setSaveError(
            "服务器上的研究记录已经变化。你的本地内容仍保留，但暂时无法读取最新版本，请稍后重试。",
          );
        }
      } else {
        setSaveError(
          problem instanceof Error ? problem.message : "保存研究记录失败",
        );
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="research-card" aria-labelledby="research-title">
      <div className="section-heading research-heading">
        <div>
          <p className="eyebrow">PERSONAL RESEARCH</p>
          <h2 id="research-title">个人研究</h2>
          <p className="section-description">
            这里保存你的收藏、判断和笔记。规则或模型分析不会覆盖这些内容。
          </p>
        </div>
        {!loading && !loadError && !editing && (
          <button className={entry ? "secondary" : "primary"} onClick={begin}>
            {entry ? "编辑个人研究" : "开始研究记录"}
          </button>
        )}
      </div>
      {loadError ? (
        <p role="alert" className="error">
          {loadError}
        </p>
      ) : loading ? (
        <p role="status" className="loading">
          正在读取个人研究…
        </p>
      ) : editing ? (
        <form className="research-form" onSubmit={submit}>
          <div className="research-form-grid">
            <label className="research-star-choice">
              <input
                type="checkbox"
                checked={draft.isStarred}
                disabled={saving}
                onChange={(event) => change("isStarred", event.target.checked)}
              />
              收藏到认知库
            </label>
            <label>
              <span>研究阶段</span>
              <select
                value={draft.stage}
                disabled={saving}
                onChange={(event) =>
                  change("stage", event.target.value as ResearchStage)
                }
              >
                {Object.entries(researchStageLabels).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>当前判断</span>
              <select
                value={draft.decision}
                disabled={saving}
                onChange={(event) =>
                  change("decision", event.target.value as ResearchDecision)
                }
              >
                {Object.entries(researchDecisionLabels).map(
                  ([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ),
                )}
              </select>
            </label>
            <label>
              <span>标签</span>
              <input
                value={tagText}
                placeholder="例如 Agent, 商业"
                disabled={saving}
                onChange={(event) => changeTags(event.target.value)}
              />
            </label>
            <label>
              <span>下一步时间（可选）</span>
              <input
                type="datetime-local"
                value={draft.nextActionAt ?? ""}
                disabled={saving}
                onChange={(event) =>
                  change("nextActionAt", event.target.value || null)
                }
              />
            </label>
          </div>
          <label>
            <span>判断依据</span>
            <textarea
              value={draft.rationale}
              rows={3}
              maxLength={10000}
              disabled={saving}
              onChange={(event) => change("rationale", event.target.value)}
            />
          </label>
          <label>
            <span>个人笔记</span>
            <textarea
              value={draft.notes}
              rows={7}
              maxLength={50000}
              disabled={saving}
              onChange={(event) => change("notes", event.target.value)}
            />
          </label>
          <label>
            <span>下一步行动</span>
            <textarea
              value={draft.nextAction}
              rows={3}
              maxLength={2000}
              disabled={saving}
              onChange={(event) => change("nextAction", event.target.value)}
            />
          </label>
          <p className="draft-note">
            仅在点击保存后写入本地数据库；关闭页面前的草稿不保证保留。
          </p>
          {saveError && (
            <p role="alert" className="error">
              {saveError}
            </p>
          )}
          {conflict && (
            <aside
              className="research-conflict"
              aria-labelledby="research-conflict-title"
            >
              <h3 id="research-conflict-title">
                服务器最新版本（第 {conflict.revision} 版）
              </h3>
              <p className="research-conflict-meta">
                {researchStageLabels[conflict.stage]} ·{" "}
                {researchDecisionLabels[conflict.decision]}
                {conflict.tags.length ? ` · ${conflict.tags.join("、")}` : ""}
              </p>
              <dl>
                <div>
                  <dt>判断依据</dt>
                  <dd>{conflict.rationale || "空"}</dd>
                </div>
                <div>
                  <dt>个人笔记</dt>
                  <dd>{conflict.notes || "空"}</dd>
                </div>
                <div>
                  <dt>下一步行动</dt>
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
              {saving ? "正在保存…" : "保存研究记录"}
            </button>
          </div>
        </form>
      ) : !entry ? (
        <div className="research-empty">
          <h3>还没有个人研究记录</h3>
          <p>收藏这个项目，写下当前判断、个人笔记和下一步行动。</p>
        </div>
      ) : (
        <div className="research-view">
          <div className="research-status-row">
            <span>{entry.isStarred ? "★ 已收藏" : "☆ 未收藏"}</span>
            <span>{researchStageLabels[entry.stage]}</span>
            <span>{researchDecisionLabels[entry.decision]}</span>
            <time>更新于 {formatDate(entry.updatedAt)}</time>
          </div>
          {!!entry.tags.length && (
            <div className="research-tags">
              {entry.tags.map((tag) => (
                <span key={tag}>{tag}</span>
              ))}
            </div>
          )}
          <div className="research-copy-grid">
            <div>
              <h3>判断依据</h3>
              <p>{entry.rationale || "尚未记录判断依据。"}</p>
            </div>
            <div>
              <h3>下一步行动</h3>
              <p>{entry.nextAction || "尚未安排下一步。"}</p>
              {entry.nextActionAt && (
                <time>计划时间：{formatDate(entry.nextActionAt)}</time>
              )}
            </div>
          </div>
          <div className="research-notes">
            <h3>个人笔记</h3>
            <p>{entry.notes || "尚未写下个人笔记。"}</p>
          </div>
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
