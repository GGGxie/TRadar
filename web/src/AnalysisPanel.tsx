import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  createAnalysis,
  evidenceKinds,
  formatDate,
  getAnalysisRun,
  getAnalysisCapabilities,
  listAnalyses,
  request,
  runRulesAnalysis,
  runModelAnalysis,
  type AnalysisCapabilities,
  type AnalysisClaim,
  type AnalysisContent,
  type AnalysisLevel,
  type AnalysisSectionKey,
  type AnalysisVersion,
  type ClaimKind,
  type Evidence,
  type Page,
  type Project,
} from "./api";

const sectionKeys: Record<AnalysisLevel, AnalysisSectionKey[]> = {
  SCREEN: ["WHAT", "PROBLEM", "ATTENTION", "FIT"],
  DEEP: ["NEED", "PRODUCT", "TECH", "GROWTH", "BUSINESS", "COMPETITION"],
};
const sectionLabels: Record<AnalysisSectionKey, string> = {
  WHAT: "对象是什么",
  PROBLEM: "解决什么问题",
  ATTENTION: "为何受到关注",
  FIT: "与我是否匹配",
  NEED: "用户需求",
  PRODUCT: "产品与体验",
  TECH: "技术与壁垒",
  GROWTH: "增长信号",
  BUSINESS: "商业模式",
  COMPETITION: "竞争与替代",
};
const claimLabels: Record<ClaimKind, string> = {
  FACT: "事实",
  HYPOTHESIS: "推测",
  JUDGMENT: "个人判断",
  UNKNOWN: "未知",
};
const originLabels = {
  MANUAL: "人工版本",
  RULES: "规则模板",
  MODEL: "模型生成",
} as const;

function emptyContent(level: AnalysisLevel): AnalysisContent {
  return {
    sections: sectionKeys[level].map((key) => ({
      key,
      claims: [
        {
          kind: "UNKNOWN",
          text: "尚未形成判断。",
          evidenceIds: [],
        },
      ],
    })),
    openQuestions: [],
    nextChecks: [],
  };
}

function cloneContent(version: AnalysisVersion): AnalysisContent {
  return {
    sections: version.sections.map((section) => ({
      key: section.key,
      claims: section.claims.map((claim) => ({
        ...claim,
        evidenceIds: [...(claim.evidenceIds ?? [])],
      })),
    })),
    openQuestions: [...version.openQuestions],
    nextChecks: [...version.nextChecks],
  };
}

function splitLines(value: string) {
  return value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

export function AnalysisPanel({
  project,
  evidenceRevision,
}: {
  project: Project;
  evidenceRevision: number;
}) {
  const [level, setLevel] = useState<AnalysisLevel>("SCREEN");
  const [versions, setVersions] = useState<Page<AnalysisVersion> | null>(null);
  const [evidence, setEvidence] = useState<Evidence[]>([]);
  const [capabilities, setCapabilities] = useState<AnalysisCapabilities | null>(
    null,
  );
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [mode, setMode] = useState<"RULES" | "MODEL" | "MANUAL" | null>(null);
  const [selectedEvidence, setSelectedEvidence] = useState<string[]>([]);
  const [draft, setDraft] = useState<AnalysisContent | null>(null);
  const [basedOn, setBasedOn] = useState<string | undefined>();
  const [saving, setSaving] = useState(false);
  const [actionError, setActionError] = useState("");
  const [notice, setNotice] = useState("");
  const [reload, setReload] = useState(0);
  const intent = useRef(crypto.randomUUID());

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError("");
    Promise.all([
      listAnalyses(project.id, level, controller.signal),
      request<Page<Evidence>>(
        `/projects/${encodeURIComponent(project.id)}/evidence?page=1&pageSize=100&current=true`,
        { signal: controller.signal },
      ),
      getAnalysisCapabilities(controller.signal),
    ])
      .then(([analysisPage, evidencePage, capabilityResult]) => {
        setVersions(analysisPage);
        setEvidence(evidencePage.data);
        setCapabilities(capabilityResult.data);
      })
      .catch((problem) => {
        if (!controller.signal.aborted)
          setLoadError(
            problem instanceof Error ? problem.message : "读取分析失败",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [evidenceRevision, level, project.id, project.revision, reload]);

  function newIntent() {
    intent.current = crypto.randomUUID();
    setActionError("");
  }

  function switchLevel(next: AnalysisLevel) {
    if (next === level) return;
    setLevel(next);
    setMode(null);
    setDraft(null);
    setSelectedEvidence([]);
    setNotice("");
    newIntent();
  }

  function beginRules() {
    setMode("RULES");
    setDraft(null);
    setSelectedEvidence([]);
    setNotice("");
    newIntent();
  }

  function beginModel() {
    setMode("MODEL");
    setDraft(null);
    setSelectedEvidence([]);
    setNotice("");
    newIntent();
  }

  function beginManual(base?: AnalysisVersion) {
    setMode("MANUAL");
    setDraft(base ? cloneContent(base) : emptyContent(level));
    setBasedOn(base?.id);
    setSelectedEvidence([]);
    setNotice("");
    newIntent();
  }

  function cancel() {
    setMode(null);
    setDraft(null);
    setActionError("");
  }

  function toggleSelected(id: string) {
    setSelectedEvidence((current) =>
      current.includes(id)
        ? current.filter((value) => value !== id)
        : [...current, id],
    );
    newIntent();
  }

  async function generateAutomated(event: FormEvent) {
    event.preventDefault();
    if (saving || (mode !== "RULES" && mode !== "MODEL")) return;
    setSaving(true);
    setActionError("");
    try {
      const input = {
        level,
        projectRevision: project.revision,
        evidenceIds: selectedEvidence,
      };
      let run =
        mode === "MODEL"
          ? (await runModelAnalysis(project.id, input, intent.current)).data
          : (await runRulesAnalysis(project.id, input, intent.current)).data;
      if (mode === "MODEL") {
        for (let attempt = 0; attempt < 60; attempt += 1) {
          if (run.status === "SUCCEEDED") break;
          if (run.status === "FAILED" || run.status === "INTERRUPTED")
            throw new Error(
              `模型任务未完成（${run.errorCode ?? run.status}），所选证据仍保留。`,
            );
          run = (await getAnalysisRun(run.id)).data;
          if (run.status === "PENDING" || run.status === "RUNNING")
            await new Promise((resolve) => window.setTimeout(resolve, 250));
        }
      }
      if (run.status !== "SUCCEEDED")
        throw new Error(
          mode === "MODEL"
            ? "模型仍在生成，请稍后刷新查看。"
            : "规则分析尚未完成，请稍后重试。",
        );
      setMode(null);
      setNotice(
        mode === "MODEL"
          ? "模型版本已生成，引用与用量已冻结；请人工核对所有推测。"
          : "规则版本已生成，引用和当前趋势已冻结。",
      );
      setReload((value) => value + 1);
    } catch (problem) {
      setActionError(
        problem instanceof Error ? problem.message : "规则分析失败",
      );
    } finally {
      setSaving(false);
    }
  }

  function updateClaim(
    sectionIndex: number,
    claimIndex: number,
    patch: Partial<AnalysisClaim>,
  ) {
    setDraft((current) => {
      if (!current) return current;
      const sections = current.sections.map((section, currentSection) => ({
        ...section,
        claims:
          currentSection === sectionIndex
            ? section.claims.map((claim, currentClaim) =>
                currentClaim === claimIndex ? { ...claim, ...patch } : claim,
              )
            : section.claims,
      }));
      return { ...current, sections };
    });
    newIntent();
  }

  function addClaim(sectionIndex: number) {
    setDraft((current) => {
      if (!current) return current;
      return {
        ...current,
        sections: current.sections.map((section, index) =>
          index === sectionIndex
            ? {
                ...section,
                claims: [
                  ...section.claims,
                  { kind: "UNKNOWN", text: "尚未形成判断。", evidenceIds: [] },
                ],
              }
            : section,
        ),
      };
    });
    newIntent();
  }

  function removeClaim(sectionIndex: number, claimIndex: number) {
    setDraft((current) => {
      if (!current) return current;
      return {
        ...current,
        sections: current.sections.map((section, index) =>
          index === sectionIndex
            ? {
                ...section,
                claims: section.claims.filter((_, item) => item !== claimIndex),
              }
            : section,
        ),
      };
    });
    newIntent();
  }

  function updateLines(field: "openQuestions" | "nextChecks", value: string) {
    setDraft((current) =>
      current ? { ...current, [field]: splitLines(value) } : current,
    );
    newIntent();
  }

  async function saveManual(event: FormEvent) {
    event.preventDefault();
    if (!draft || saving) return;
    setSaving(true);
    setActionError("");
    try {
      await createAnalysis(
        project.id,
        {
          ...draft,
          level,
          projectRevision: project.revision,
          basedOnAnalysisId: basedOn,
        },
        intent.current,
      );
      setMode(null);
      setDraft(null);
      setNotice("人工版本已保存，旧版本继续保留。");
      setReload((value) => value + 1);
    } catch (problem) {
      setActionError(
        problem instanceof Error ? problem.message : "保存分析失败",
      );
    } finally {
      setSaving(false);
    }
  }

  const levelName = level === "SCREEN" ? "初筛" : "深研";

  return (
    <section className="analysis-card" aria-labelledby="analysis-title">
      <div className="section-heading analysis-heading">
        <div>
          <p className="eyebrow">ANALYSIS</p>
          <h2 id="analysis-title">证据化分析</h2>
          <p className="section-description">
            把事实、推测和个人判断分开记录；每次保存都会形成可回溯的新版本。
          </p>
        </div>
        <div className="analysis-actions">
          <button
            className="secondary"
            disabled={!!mode || loading}
            onClick={beginRules}
          >
            生成规则{levelName}
          </button>
          <button
            className="secondary"
            disabled={!!mode || loading || !capabilities?.modelEnabled}
            onClick={beginModel}
            title={
              capabilities?.modelEnabled
                ? "只会发送本次明确选择的公开证据"
                : "需要先在服务端配置模型、输出上限和每日预算"
            }
          >
            生成模型{levelName}
          </button>
          <button
            className="primary"
            disabled={!!mode || loading}
            onClick={() => beginManual()}
          >
            新建人工版本
          </button>
        </div>
      </div>
      <div className="level-tabs" role="tablist" aria-label="分析层级">
        <button
          role="tab"
          aria-selected={level === "SCREEN"}
          onClick={() => switchLevel("SCREEN")}
        >
          四问初筛
        </button>
        <button
          role="tab"
          aria-selected={level === "DEEP"}
          onClick={() => switchLevel("DEEP")}
        >
          六维深研
        </button>
      </div>
      <div className="analysis-capability-note">
        <span>本地规则可用</span>
        <span>
          {capabilities?.modelEnabled
            ? `模型已启用：${capabilities.provider} / ${capabilities.model}`
            : "模型分析未启用"}
        </span>
      </div>
      {(mode === "RULES" || mode === "MODEL") && (
        <form className="analysis-builder" onSubmit={generateAutomated}>
          <div>
            <h3>选择本次要冻结的证据</h3>
            <p>
              {mode === "MODEL"
                ? "模型只接收你明确选择的公开证据；私密证据不可选择，也不会发送。模型没有浏览或工具权限。"
                : "规则模板只整理你明确选择的内容。私密证据保留在本机，不会发送给外部服务。"}
            </p>
          </div>
          {evidence.length > 1 && (
            <div className="picker-shortcuts">
              <button
                type="button"
                className="text-button"
                disabled={saving}
                onClick={() =>
                  setSelectedEvidence(
                    evidence
                      .filter((item) => mode !== "MODEL" || item.isPublic)
                      .map((item) => item.id),
                  )
                }
              >
                全选
              </button>
              <button
                type="button"
                className="text-button"
                disabled={saving}
                onClick={() => setSelectedEvidence([])}
              >
                清空
              </button>
            </div>
          )}
          {evidence.length ? (
            <div className="analysis-evidence-picker">
              {evidence.map((item) => (
                <label key={item.id}>
                  <input
                    type="checkbox"
                    checked={selectedEvidence.includes(item.id)}
                    onChange={() => toggleSelected(item.id)}
                    disabled={saving || (mode === "MODEL" && !item.isPublic)}
                  />
                  <span>
                    <strong>{item.title}</strong>
                    <small>
                      {item.isPublic
                        ? "公开证据"
                        : mode === "MODEL"
                          ? "私密证据 · 不会发送"
                          : "私密证据 · 仅本地规则使用"}
                    </small>
                  </span>
                </label>
              ))}
            </div>
          ) : (
            <p className="muted">
              当前没有证据；仍可生成明确标注未知项的模板。采集来的项目可在上方「来源与当前关注」点「保存为证据」后再勾选。
            </p>
          )}
          {actionError && (
            <p role="alert" className="error">
              {actionError} 选择已保留，可以直接重试。
            </p>
          )}
          <div className="form-actions">
            <button type="button" className="text-button" onClick={cancel}>
              取消
            </button>
            <button className="primary" disabled={saving}>
              {saving
                ? "正在生成…"
                : mode === "MODEL"
                  ? "调用模型并冻结版本"
                  : "生成并冻结版本"}
            </button>
          </div>
        </form>
      )}
      {mode === "MANUAL" && draft && (
        <form
          className="analysis-builder manual-analysis"
          onSubmit={saveManual}
        >
          <div>
            <h3>{basedOn ? "基于历史版本修订" : `新建人工${levelName}`}</h3>
            <p>事实必须引用至少一条证据；推测、个人判断与未知可以不引用。</p>
          </div>
          {draft.sections.map((section, sectionIndex) => (
            <fieldset key={section.key} className="analysis-section-editor">
              <legend>
                {sectionIndex + 1}. {sectionLabels[section.key]}
              </legend>
              {section.claims.map((claim, claimIndex) => {
                const label = `${sectionLabels[section.key]} · 声明 ${claimIndex + 1}`;
                const claimEvidence = claim.evidenceIds ?? [];
                return (
                  <div className="claim-editor" key={claimIndex}>
                    <div className="claim-editor-row">
                      <label>
                        <span>{label} 类型</span>
                        <select
                          aria-label={`${label} 类型`}
                          value={claim.kind}
                          disabled={saving}
                          onChange={(event) => {
                            const kind = event.target.value as ClaimKind;
                            updateClaim(sectionIndex, claimIndex, {
                              kind,
                              evidenceIds: kind === "FACT" ? claimEvidence : [],
                            });
                          }}
                        >
                          {Object.entries(claimLabels).map(([value, text]) => (
                            <option key={value} value={value}>
                              {text}
                            </option>
                          ))}
                        </select>
                      </label>
                      {section.claims.length > 1 && (
                        <button
                          type="button"
                          className="text-button"
                          onClick={() => removeClaim(sectionIndex, claimIndex)}
                          disabled={saving}
                        >
                          删除此声明
                        </button>
                      )}
                    </div>
                    <label>
                      <span>{label} 内容</span>
                      <textarea
                        aria-label={`${label} 内容`}
                        value={claim.text}
                        maxLength={2000}
                        rows={3}
                        required
                        disabled={saving}
                        onChange={(event) =>
                          updateClaim(sectionIndex, claimIndex, {
                            text: event.target.value,
                          })
                        }
                      />
                    </label>
                    {claim.kind === "FACT" && (
                      <fieldset className="claim-evidence-picker">
                        <legend>引用证据（至少一条）</legend>
                        {evidence.length ? (
                          evidence.map((item) => (
                            <label key={item.id}>
                              <input
                                type="checkbox"
                                aria-label={`${label} 引用 ${item.title}`}
                                checked={claimEvidence.includes(item.id)}
                                disabled={saving}
                                onChange={() =>
                                  updateClaim(sectionIndex, claimIndex, {
                                    evidenceIds: claimEvidence.includes(item.id)
                                      ? claimEvidence.filter(
                                          (value) => value !== item.id,
                                        )
                                      : [...claimEvidence, item.id],
                                  })
                                }
                              />
                              {item.title}
                              <small>
                                {item.isPublic ? "公开" : "私密 / 本地"}
                              </small>
                            </label>
                          ))
                        ) : (
                          <p className="muted">请先在下方记录证据。</p>
                        )}
                      </fieldset>
                    )}
                  </div>
                );
              })}
              <button
                type="button"
                className="text-button add-claim"
                disabled={saving || section.claims.length >= 12}
                onClick={() => addClaim(sectionIndex)}
              >
                + 添加声明
              </button>
            </fieldset>
          ))}
          <div className="analysis-text-lists">
            <label>
              <span>待验证问题（每行一条）</span>
              <textarea
                value={draft.openQuestions.join("\n")}
                rows={4}
                disabled={saving}
                onChange={(event) =>
                  updateLines("openQuestions", event.target.value)
                }
              />
            </label>
            <label>
              <span>下一步检查（每行一条）</span>
              <textarea
                value={draft.nextChecks.join("\n")}
                rows={4}
                disabled={saving}
                onChange={(event) =>
                  updateLines("nextChecks", event.target.value)
                }
              />
            </label>
          </div>
          {actionError && (
            <p role="alert" className="error">
              {actionError} 草稿已保留，可以直接重试。
            </p>
          )}
          <div className="form-actions">
            <button type="button" className="text-button" onClick={cancel}>
              取消
            </button>
            <button className="primary" disabled={saving}>
              {saving ? "正在保存…" : "保存人工版本"}
            </button>
          </div>
        </form>
      )}
      {notice && (
        <p role="status" className="analysis-notice">
          {notice}
        </p>
      )}
      {loadError ? (
        <p role="alert" className="error">
          {loadError}
        </p>
      ) : loading ? (
        <p role="status" className="loading">
          正在读取分析版本…
        </p>
      ) : !versions?.data.length ? (
        <div className="analysis-empty">
          <h3>还没有{levelName}版本</h3>
          <p>
            {level === "SCREEN"
              ? "先用四个问题判断是否值得继续研究。"
              : "把需求、产品、技术、增长、商业和竞争逐项写清。"}
          </p>
        </div>
      ) : (
        <div className="analysis-versions">
          {versions.data.map((version) => (
            <article className="analysis-version" key={version.id}>
              <header>
                <div>
                  <div className="analysis-version-meta">
                    <span
                      className={`origin origin-${version.origin.toLowerCase()}`}
                    >
                      {originLabels[version.origin]}
                    </span>
                    <span>v{version.version}</span>
                    <time>冻结于 {formatDate(version.cutoffAt)}</time>
                  </div>
                  <h3>
                    {version.level === "SCREEN" ? "四问初筛" : "六维深研"} ·
                    版本 {version.version}
                  </h3>
                </div>
                <button
                  className="secondary"
                  disabled={!!mode}
                  onClick={() => beginManual(version)}
                >
                  基于此版修订
                </button>
              </header>
              {version.contextChanged && (
                <p className="context-warning">
                  项目信息或证据关联已变化；此处仍展示生成时冻结的上下文。
                </p>
              )}
              <div className="analysis-section-grid">
                {version.sections.map((section) => (
                  <section key={section.key}>
                    <h4>{sectionLabels[section.key]}</h4>
                    <ul>
                      {section.claims.map((claim, index) => (
                        <li key={index}>
                          <span
                            className={`claim-kind claim-${claim.kind.toLowerCase()}`}
                          >
                            {claimLabels[claim.kind]}
                          </span>
                          <p>{claim.text}</p>
                          {!!(claim.evidenceIds ?? []).length && (
                            <small>
                              引用：
                              {(claim.evidenceIds ?? [])
                                .map(
                                  (id) =>
                                    version.evidenceSnapshots.find(
                                      (item) => item.id === id,
                                    )?.title ?? id,
                                )
                                .join("、")}
                            </small>
                          )}
                        </li>
                      ))}
                    </ul>
                  </section>
                ))}
              </div>
              {(version.openQuestions.length > 0 ||
                version.nextChecks.length > 0) && (
                <div className="analysis-followups">
                  <div>
                    <strong>待验证问题</strong>
                    <ul>
                      {version.openQuestions.map((item, index) => (
                        <li key={index}>{item}</li>
                      ))}
                    </ul>
                  </div>
                  <div>
                    <strong>下一步检查</strong>
                    <ul>
                      {version.nextChecks.map((item, index) => (
                        <li key={index}>{item}</li>
                      ))}
                    </ul>
                  </div>
                </div>
              )}
              {!!version.evidenceSnapshots.length && (
                <details className="analysis-snapshots">
                  <summary>
                    冻结证据快照（{version.evidenceSnapshots.length}）
                  </summary>
                  {version.evidenceSnapshots.map((item) => (
                    <div key={item.id}>
                      <span>
                        {item.kind === "METRIC_OBSERVATION"
                          ? "指标观测"
                          : evidenceKinds[item.kind]}
                      </span>
                      <strong>{item.title}</strong>
                      <small>
                        {item.isPublic
                          ? "公开证据"
                          : "私密证据 · 仅本地规则使用"}
                      </small>
                      <blockquote>{item.excerpt}</blockquote>
                      <a
                        href={item.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        查看原始来源 ↗
                      </a>
                    </div>
                  ))}
                </details>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
