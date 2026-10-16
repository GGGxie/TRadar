import { useEffect, useState } from "react";
import {
  categories,
  formatDate,
  projectHost,
  request,
  type Project,
  type Resource,
} from "./api";
import { ProjectForm } from "./ProjectForm";
import { EvidencePanel } from "./EvidencePanel";
import { AnalysisPanel } from "./AnalysisPanel";
import { ResearchPanel } from "./ResearchPanel";
export function ProjectDetails({ id }: { id: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [evidenceRevision, setEvidenceRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    request<Resource<Project>>(`/projects/${encodeURIComponent(id)}`, {
      signal: controller.signal,
    })
      .then((result) => setProject(result.data))
      .catch((error) => {
        if (!controller.signal.aborted) setError(error.message);
      });
    return () => controller.abort();
  }, [id]);
  return (
    <>
      <a href="/radar" className="back-link">
        ← 返回项目目录
      </a>
      {error ? (
        <div className="empty-state">
          <p role="alert" className="error">
            {error}
          </p>
        </div>
      ) : !project ? (
        <p role="status" className="loading">
          正在读取项目…
        </p>
      ) : (
        <>
          <header className="page-heading">
            <div>
              <p className="eyebrow">
                PROJECT / {categories[project.category]}
              </p>
              <h1>{project.name}</h1>
              <a
                href={project.canonicalUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="source-link"
              >
                {projectHost(project.canonicalUrl)}{" "}
                <span aria-hidden="true">↗</span>
              </a>
            </div>
            <button
              className="secondary"
              disabled={editing}
              onClick={() => setEditing(true)}
            >
              编辑信息
            </button>
          </header>
          {editing && (
            <ProjectForm
              project={project}
              onSaved={(saved) => {
                setProject(saved);
                setEditing(false);
              }}
              onCancel={() => setEditing(false)}
            />
          )}
          <section className="detail-card">
            <p className="eyebrow">AT A GLANCE</p>
            <h2>一句话认识这个项目</h2>
            <p className={project.summary ? "summary" : "muted"}>
              {project.summary ||
                "还没有介绍。用一句话写下它帮助谁解决了什么问题。"}
            </p>
            <dl className="metadata">
              <div>
                <dt>关注方向</dt>
                <dd>{categories[project.category]}</dd>
              </div>
              <div>
                <dt>首次发现</dt>
                <dd>{formatDate(project.firstSeenAt)}</dd>
              </div>
              <div>
                <dt>最近更新</dt>
                <dd>{formatDate(project.updatedAt)}</dd>
              </div>
            </dl>
          </section>
          <section
            className="evidence-note"
            aria-labelledby="evidence-guidance-title"
          >
            <span className="note-icon" aria-hidden="true">
              i
            </span>
            <div>
              <h2 id="evidence-guidance-title">先记录，再求证</h2>
              <p>
                项目介绍来自你的录入。热度、用户需求和商业表现，需要各自的来源与证据。
              </p>
            </div>
          </section>
          <AnalysisPanel
            project={project}
            evidenceRevision={evidenceRevision}
          />
          <ResearchPanel project={project} />
          <EvidencePanel
            projectId={project.id}
            onChanged={() => setEvidenceRevision((value) => value + 1)}
          />
        </>
      )}
    </>
  );
}
