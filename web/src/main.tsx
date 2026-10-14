import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ProjectList } from "./ProjectList";
import { ProjectDetails } from "./ProjectDetails";
import "./styles.css";
import "./evidence.css";
function App() {
  const path = window.location.pathname;
  const projectID = path.startsWith("/projects/")
    ? path.slice("/projects/".length)
    : null;
  const today = new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(new Date());
  return (
    <div className="app-shell">
      <a href="#main" className="skip-link">
        跳转到主要内容
      </a>
      <aside className="sidebar" aria-label="工作区导航">
        <a href="/radar" className="brand">
          <svg viewBox="0 0 36 36" width="36" height="36" aria-hidden="true">
            <circle cx="18" cy="18" r="15" />
            <circle cx="18" cy="18" r="9" />
            <path d="M18 18 29 7M18 3v4M3 18h4M18 29v4M29 18h4" />
            <circle cx="23" cy="14" r="2" className="radar-dot" />
          </svg>
          <span>
            TRadar<span className="brand-caption">技术与商业机会雷达</span>
          </span>
        </a>
        <p className="nav-caption">我的工作台</p>
        <nav aria-label="主导航">
          <a
            href="/radar"
            className="nav-item active"
            aria-current={!projectID ? "page" : undefined}
          >
            <span aria-hidden="true">◎</span> 项目目录
            <span className="nav-chevron" aria-hidden="true">
              ›
            </span>
          </a>
        </nav>
        <div className="sidebar-note">
          <span className="eyebrow">STAY CURIOUS.</span>
          <p>
            关注变化，
            <br />
            积累自己的判断。
          </p>
          <div className="sidebar-rule" />
          <span>发现 → 理解 → 验证</span>
        </div>
        <div className="profile">
          <span className="profile-avatar">我</span>
          <div>
            <strong>个人工作空间</strong>
            <span>本地保存 · 自主研究</span>
          </div>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <span>
            工作空间 <span aria-hidden="true">/</span>{" "}
            <strong>{projectID ? "项目详情" : "项目目录"}</strong>
          </span>
          <time>{today}</time>
        </header>
        <main id="main">
          {projectID ? <ProjectDetails id={projectID} /> : <ProjectList />}
        </main>
        <footer className="page-footer">
          <span>TRadar · Keep a signal, build a perspective.</span>
          <span>事实、推测与判断，分别记录。</span>
        </footer>
      </div>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
