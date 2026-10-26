import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ApiError,
  categories,
  saveProject,
  type Project,
  type ProjectInput,
} from "./api";

interface Props {
  project?: Project;
  // existing=true：链接已在目录中，服务端返回已有项目，表单内容没有写入。
  onSaved: (project: Project, existing?: boolean) => void;
  onCancel: () => void;
}
export function ProjectForm({ project, onSaved, onCancel }: Props) {
  const [draft, setDraft] = useState<ProjectInput>({
    name: project?.name ?? "",
    canonicalUrl: project?.canonicalUrl ?? "",
    summary: project?.summary ?? "",
    category: project?.category ?? "OTHER",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dirty = useRef(false);
  const [conflict, setConflict] = useState(false);
  const key = useRef(crypto.randomUUID());
  const first = useRef<HTMLInputElement>(null);
  useEffect(() => {
    first.current?.focus();
  }, []);
  useEffect(() => {
    const protect = (event: BeforeUnloadEvent) => {
      if (dirty.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, []);
  function change<K extends keyof ProjectInput>(
    name: K,
    value: ProjectInput[K],
  ) {
    setDraft((previous) => ({ ...previous, [name]: value }));
    dirty.current = true;
    key.current = crypto.randomUUID();
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || conflict) return;
    setBusy(true);
    setError("");
    try {
      const result = await saveProject(draft, key.current, project);
      dirty.current = false;
      onSaved(result.data, result.existing ?? false);
    } catch (error) {
      setError(error instanceof Error ? error.message : "保存失败");
      if (error instanceof ApiError && error.status === 412) setConflict(true);
    } finally {
      setBusy(false);
    }
  }
  function cancel() {
    if (
      !dirty.current ||
      window.confirm("有尚未保存的内容，确定放弃本次修改吗？")
    ) {
      dirty.current = false;
      onCancel();
    }
  }
  return (
    <section className="form-panel" aria-labelledby="form-title">
      <div className="section-heading">
        <div>
          <p className="eyebrow">PROJECT NOTE</p>
          <h2 id="form-title">
            {project ? "编辑项目信息" : "记录一个值得关注的项目"}
          </h2>
        </div>
        <button
          className="text-button"
          type="button"
          onClick={cancel}
          disabled={busy}
        >
          取消
        </button>
      </div>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label>
            项目名称
            <input
              ref={first}
              value={draft.name}
              onChange={(event) => change("name", event.target.value)}
              required
              maxLength={200}
              placeholder="例如：一个新的 Agent 工具"
              disabled={busy}
            />
          </label>
          <label>
            项目链接
            <input
              type="url"
              value={draft.canonicalUrl}
              onChange={(event) => change("canonicalUrl", event.target.value)}
              required
              maxLength={4096}
              placeholder="https://"
              disabled={busy || !!project}
            />
          </label>
          <label>
            <span id="project-category-label">项目分类</span>
            <select
              aria-labelledby="project-category-label"
              value={draft.category}
              onChange={(event) =>
                change(
                  "category",
                  event.target.value as ProjectInput["category"],
                )
              }
              disabled={busy}
            >
              {Object.entries(categories).map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <label>
          <span id="project-summary-label">一句话介绍</span>
          <textarea
            aria-labelledby="project-summary-label"
            value={draft.summary}
            onChange={(event) => change("summary", event.target.value)}
            maxLength={4000}
            rows={4}
            placeholder="它是什么？帮助谁解决什么问题？"
            disabled={busy}
          />
        </label>
        {error && (
          <p role="alert" className="error">
            {error}
            {conflict && " 本地内容已保留，请先复制内容，再重新加载最新项目。"}
          </p>
        )}
        <div className="form-footer">
          <p className="muted">
            链接和文字会保存到你的项目目录。不会自动访问该链接。
          </p>
          <button className="primary" disabled={busy || conflict}>
            {busy ? "正在保存…" : "保存项目"}
          </button>
        </div>
      </form>
    </section>
  );
}
