import { Fragment, useEffect, useState } from "react";
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
import { ExperimentPanel } from "./ExperimentPanel";
import { SignalPanel } from "./SignalPanel";
export function ProjectDetails({ id }: { id: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [evidenceRevision, setEvidenceRevision] = useState(0);
  // 从「记录项目」跳转而来且链接已存在时提示一次，并清掉地址参数。
  const [openedExisting] = useState(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("existing") !== "1") return false;
    window.history.replaceState(null, "", window.location.pathname);
    return true;
  });
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
              <h1>
                {/* 在「/」后允许换行，避免窄屏把 owner/repo 拆在单词中间。 */}
                {project.name.split("/").map((part, index) => (
                  <Fragment key={index}>
                    {index > 0 && (
                      <>
                        /<wbr />
                      </>
                    )}
                    {part}
                  </Fragment>
                ))}
              </h1>
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
          {openedExisting && (
            <p className="success-message" role="status">
              这个链接已在目录中，已为你打开现有项目；刚才填写的名称和介绍没有保存，可以在「编辑信息」里修改。
            </p>
          )}
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
          <SignalPanel
            projectId={project.id}
            onEvidenceSaved={() => setEvidenceRevision((value) => value + 1)}
          />
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
                项目介绍来自人工录入或来源采集。关注度不等于需求，用户需求和商业表现需要各自的证据。
              </p>
            </div>
          </section>
          <AnalysisPanel
            project={project}
            evidenceRevision={evidenceRevision}
          />
          <ResearchPanel project={project} />
          <ExperimentPanel
            project={project}
            evidenceRevision={evidenceRevision}
          />
          <EvidencePanel
            projectId={project.id}
            refreshKey={evidenceRevision}
            onChanged={() => setEvidenceRevision((value) => value + 1)}
          />
        </>
      )}
    </>
  );
}
