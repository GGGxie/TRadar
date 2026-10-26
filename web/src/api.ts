export const categories = {
  AI_AGENT: "AI Agent",
  DEVTOOLS: "开发者工具",
  SAAS: "SaaS / 产品",
  OTHER: "其他方向",
} as const;
export const evidenceKinds = {
  OFFICIAL_DESCRIPTION: "官方介绍",
  OFFICIAL_PRICING: "官方定价",
  FOUNDER_CLAIM: "创始人自述",
  THIRD_PARTY_ESTIMATE: "第三方估算",
  PUBLIC_EXCERPT: "公开摘录",
} as const;
export type Category = keyof typeof categories;
export type EvidenceKind = keyof typeof evidenceKinds;
export interface Project {
  id: string;
  name: string;
  canonicalUrl: string;
  summary: string;
  category: Category;
  firstSeenAt: string;
  lastSeenAt: string;
  createdAt: string;
  updatedAt: string;
  revision: number;
}
export interface ProjectInput {
  name: string;
  canonicalUrl: string;
  summary: string;
  category: Category;
}
export interface Evidence {
  id: string;
  projectId: string;
  sourceRecordId: string | null;
  url: string;
  title: string;
  excerpt: string;
  kind: EvidenceKind | "METRIC_OBSERVATION";
  publisher: string | null;
  publishedAt: string | null;
  collectedAt: string;
  isPublic: boolean;
  observationIds: string[];
  supersedesId: string | null;
}
export interface EvidenceInput {
  url: string;
  title: string;
  excerpt: string;
  kind: EvidenceKind;
  publisher?: string;
  publishedAt?: string;
  isPublic: boolean;
  supersedesId?: string;
}
export interface Page<T> {
  data: T[];
  pagination: {
    page: number;
    pageSize: number;
    totalItems: number;
    totalPages: number;
  };
}
export interface Resource<T> {
  data: T;
}
export type RemoteSource = "GITHUB" | "HACKER_NEWS" | "PRODUCT_HUNT";
export type RunStatus =
  | "QUEUED"
  | "RUNNING"
  | "RETRY_WAIT"
  | "SUCCEEDED"
  | "SUCCEEDED_EMPTY"
  | "PARTIAL"
  | "FAILED"
  | "INTERRUPTED";
export interface SourceConfig {
  source: RemoteSource;
  isEnabled: boolean;
  isScheduleEnabled: boolean;
  intervalMinutes: number;
  keywords?: string[];
  discoveryDays?: number;
  candidatesPerKeyword?: number;
  trackedSourceRecordIds?: string[];
  requestBudgetPerHour?: number;
  feeds?: string[];
  itemsPerFeed?: number;
  maxPages?: number;
  minStars?: number;
  minPoints?: number;
}
export interface SourceView {
  source: RemoteSource;
  config: SourceConfig;
  revision: number;
  hasCredentials: boolean;
  availability: "AVAILABLE" | "DISABLED" | "MISSING_CREDENTIALS";
  lastRunId: string | null;
  lastSuccessfulAt: string | null;
  lastObservationAt: string | null;
  nextScheduledAt: string | null;
  nextRetryAt: string | null;
  budget: {
    remainingRequests: number | null;
    resetAt: string | null;
  };
}
export interface RunError {
  code: string;
  message: string;
  itemId: string | null;
}
export interface IngestionRun {
  id: string;
  source: RemoteSource;
  trigger: "MANUAL" | "SCHEDULED";
  status: RunStatus;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  fetchedItems: number;
  acceptedItems: number;
  skippedItems: number;
  failedItems: number;
  observationsWritten: number;
  requestsMade: number;
  isTruncated: boolean;
  nextRetryAt: string | null;
  errors: RunError[];
}
export interface GrowthResult {
  status: "READY" | "INSUFFICIENT_HISTORY" | "STALE" | "UNSUPPORTED";
  from: string | null;
  to: string | null;
  durationHours: number | null;
  delta: number | null;
  ratePerDay: number | null;
  growthRatio: number | null;
  observationIds: string[];
}
export interface TrendResult {
  projectId: string;
  asOf: string;
  algorithmVersion: "trend-v1";
  status: GrowthResult["status"];
  score: number | null;
  isPartial: boolean;
  primarySignal: {
    sourceRecordId: string;
    source: RemoteSource;
    metric: string;
    latestValue: number;
    latestObservedAt: string;
  } | null;
  growth: GrowthResult;
  persistence: {
    status: GrowthResult["status"];
    positiveWindows: number | null;
    validWindows: number;
    totalWindows: 7;
  };
  crossSource: { platforms: RemoteSource[]; sourceRecordIds: string[] };
  // 2026-10-23 之前冻结的日报/分析快照没有该字段。
  attention?: {
    index: number | null;
    metrics: AttentionMetric[];
    // #33 之前生成的结果没有该字段。
    sourceWeeks?: SourceWeek[];
  };
  explanations: string[];
  alert: { kind: string; threshold: number; observationIds: string[] } | null;
}
export interface AttentionMetric {
  sourceRecordId: string;
  source: RemoteSource;
  metric: string;
  value: number;
  observedAt: string;
  observationId: string;
}
export interface SourceWeek {
  sourceRecordId: string;
  source: RemoteSource;
  metric: string;
  weekStart: string;
  timeZone: string;
  days: (number | null)[];
  total: number;
  observedAt: string;
}
export interface SourceRecord {
  id: string;
  projectId: string;
  source: RemoteSource | "MANUAL";
  externalId: string;
  recordType: string;
  url: string;
  targetUrl: string | null;
  title: string;
  excerpt: string;
  publishedAt: string | null;
  fetchedAt: string;
  revision: number;
}
export const sourceLabels: Record<SourceRecord["source"], string> = {
  GITHUB: "GitHub",
  HACKER_NEWS: "Hacker News",
  PRODUCT_HUNT: "Product Hunt",
  MANUAL: "人工录入",
};
export const metricLabels: Record<string, string> = {
  STARS: "Stars",
  FORKS: "Forks",
  POINTS: "分数",
  COMMENTS: "评论",
  VOTES: "票数",
};
// 列表里每个平台只展示主指标，例如「GitHub ★ 17.4 万」「HN 412 分」。
export function attentionBadge(metric: AttentionMetric) {
  const value = compactCount(metric.value);
  switch (metric.source) {
    case "GITHUB":
      return `GitHub ★ ${value}`;
    case "HACKER_NEWS":
      return `HN ${value} 分`;
    default:
      return `PH ${value} 票`;
  }
}
export function primaryAttention(metrics: AttentionMetric[]) {
  const primary: Record<string, string> = {
    GITHUB: "STARS",
    HACKER_NEWS: "POINTS",
    PRODUCT_HUNT: "VOTES",
  };
  return metrics.filter((metric) => primary[metric.source] === metric.metric);
}
export function compactCount(value: number) {
  if (Math.abs(value) < 10000) return String(value);
  return `${(value / 10000).toFixed(1).replace(/\.0$/, "")} 万`;
}
export interface ProjectTrend {
  project: Project;
  trend: TrendResult;
}
export type AnalysisLevel = "SCREEN" | "DEEP";
export type AnalysisOrigin = "MANUAL" | "RULES" | "MODEL";
export type ClaimKind = "FACT" | "HYPOTHESIS" | "JUDGMENT" | "UNKNOWN";
export type AnalysisSectionKey =
  | "WHAT"
  | "PROBLEM"
  | "ATTENTION"
  | "FIT"
  | "NEED"
  | "PRODUCT"
  | "TECH"
  | "GROWTH"
  | "BUSINESS"
  | "COMPETITION";
export interface AnalysisClaim {
  kind: ClaimKind;
  text: string;
  evidenceIds: string[];
}
export interface AnalysisSection {
  key: AnalysisSectionKey;
  claims: AnalysisClaim[];
}
export interface AnalysisContent {
  sections: AnalysisSection[];
  openQuestions: string[];
  nextChecks: string[];
}
export interface AnalysisVersion extends AnalysisContent {
  id: string;
  projectId: string;
  level: AnalysisLevel;
  version: number;
  origin: AnalysisOrigin;
  projectRevision: number;
  basedOnAnalysisId: string | null;
  createdAt: string;
  cutoffAt: string;
  rulesVersion: string | null;
  model: {
    provider: string;
    name: string;
    promptVersion: string;
  } | null;
  evidenceIds: string[];
  evidenceSnapshots: Evidence[];
  trendSnapshot: TrendResult | null;
  contextChanged: boolean;
}
export interface AnalysisRun {
  id: string;
  projectId: string;
  level: AnalysisLevel;
  mode: "RULES" | "MODEL";
  status: "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "INTERRUPTED";
  revision: number;
  projectRevision: number;
  cutoffAt: string;
  analysisId: string | null;
  errorCode: string | null;
  usage: { inputTokens: number | null; outputTokens: number | null } | null;
  createdAt: string;
  finishedAt: string | null;
}
export interface AnalysisCapabilities {
  rulesEnabled: boolean;
  modelEnabled: boolean;
  provider: string | null;
  model: string | null;
  maxOutputTokens: number | null;
  dailyTokenBudget: number | null;
}
export type ResearchStage =
  | "SCREENING"
  | "DEEP_RESEARCH"
  | "PRACTICE"
  | "REVIEWED"
  | "ARCHIVED";
export type ResearchDecision =
  | "UNDECIDED"
  | "OBSERVE"
  | "LEARN"
  | "USE"
  | "VALIDATE"
  | "PASS";
export interface ResearchFields {
  isStarred: boolean;
  tags: string[];
  stage: ResearchStage;
  decision: ResearchDecision;
  rationale: string;
  notes: string;
  nextAction: string;
  nextActionAt: string | null;
}
export interface ResearchEntry extends ResearchFields {
  id: string;
  projectId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}
export interface ResearchPreferences {
  revision: number;
  focusTopics: string[];
  dailySaveTarget: number;
  weeklyDeepTarget: number;
  monthlyPracticeTarget: number;
}
export type ExperimentStatus =
  | "PLANNED"
  | "RUNNING"
  | "COMPLETED"
  | "CANCELLED";
export type ExperimentOutcome = "SUPPORTED" | "REFUTED" | "INCONCLUSIVE";
export interface ExperimentFields {
  title: string;
  hypothesis: string;
  method: string;
  status: ExperimentStatus;
  timeBudgetMinutes: number;
  actualMinutes: number | null;
  result: string;
  outcome: ExperimentOutcome | null;
  evidenceIds: string[];
  artifactUrls: string[];
  nextAction: string;
}
export interface Experiment extends ExperimentFields {
  id: string;
  projectId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}
export type BriefReason =
  | "NEW_DISCOVERY"
  | "FIRST_COVERAGE"
  | "GROWTH_CHANGE"
  | "NEW_EVIDENCE"
  | "ANALYSIS_CHANGE"
  | "RESEARCH_CHANGE";
export interface BriefCoverage {
  source: RemoteSource;
  latestCollectedAt: string | null;
  state: "AVAILABLE" | "STALE" | "NO_DATA";
}
export interface BriefItem {
  projectId: string;
  projectRevision: number;
  title: string;
  url: string;
  // brief-selection-v2 起提供；旧日报没有该字段。
  summary?: string;
  reasons: BriefReason[];
  factSummary: string;
  hypothesisSummary: string | null;
  personalDecision: string | null;
  nextAction: string | null;
  evidenceIds: string[];
  evidenceSnapshots: Evidence[];
  trendSnapshot: TrendResult;
  analysisId: string | null;
  researchRevision: number | null;
  changeFingerprint: string;
}
export interface DailyBrief {
  id: string;
  date: string;
  timeZone: string;
  version: number;
  cutoffAt: string;
  generatedAt: string;
  previousBriefId: string | null;
  selectionVersion: string;
  rankingVersion: string;
  isDemo: boolean;
  items: BriefItem[];
  coverage: BriefCoverage[];
  emptyReason: "NO_DATA" | "NO_NEW_CHANGES" | null;
}
export interface BriefRun {
  id: string;
  status: "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "INTERRUPTED";
  revision: number;
  date: string;
  timeZone: string;
  cutoffAt: string;
  briefId: string | null;
  errorCode: string | null;
  createdAt: string;
  finishedAt: string | null;
}
export interface BriefSettings {
  revision: number;
  isEnabled: boolean;
  timeZone: string;
  localTime: string;
  maxItems: number;
}
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}
const ACCESS_TOKEN_KEY = "tradar.accessToken";
export const UNAUTHORIZED_EVENT = "tradar:unauthorized";
// 令牌只放在 sessionStorage：关闭标签页即失效，也不会被其他标签页或下次访问带走。
export function getAccessToken(): string | null {
  return window.sessionStorage.getItem(ACCESS_TOKEN_KEY);
}
export function saveAccessToken(token: string) {
  window.sessionStorage.setItem(ACCESS_TOKEN_KEY, token);
}
export function clearAccessToken() {
  window.sessionStorage.removeItem(ACCESS_TOKEN_KEY);
}
function withAccessToken(options: RequestInit): RequestInit {
  const token = getAccessToken();
  if (!token) return options;
  const headers = new Headers(options.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return { ...options, headers };
}
export async function request<T>(
  path: string,
  options: RequestInit & { onStatus?: (status: number) => void } = {},
): Promise<T> {
  let result: Response;
  const { onStatus: _onStatus, ...init } = options;
  try {
    result = await fetch(`/api/v1${path}`, withAccessToken(init));
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new ApiError(
      "暂时无法连接服务，内容已保留，请稍后重试。",
      0,
      "NETWORK_ERROR",
    );
  }
  if (!result.ok) {
    let message = "请求未能完成，请稍后重试。",
      code = "REQUEST_ERROR";
    try {
      const body = (await result.json()) as {
        error?: {
          message?: string;
          code?: string;
          fields?: Record<string, string>;
        };
      };
      message = body.error?.message ?? message;
      code = body.error?.code ?? code;
      if (body.error?.fields)
        message += `：${Object.values(body.error.fields).join("；")}`;
    } catch {
      /* Non-JSON gateway errors still have a useful message. */
    }
    if (code === "UNAUTHORIZED") {
      clearAccessToken();
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    }
    throw new ApiError(message, result.status, code);
  }
  options.onStatus?.(result.status);
  return result.json() as Promise<T>;
}
export function saveProject(
  input: ProjectInput,
  key: string,
  project?: Project,
): Promise<Resource<Project> & { existing?: boolean }> {
  if (project) {
    const { name, summary, category } = input;
    return request<Resource<Project>>(`/projects/${project.id}`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${project.revision}"`,
      },
      body: JSON.stringify({ name, summary, category }),
    });
  }
  let status = 0;
  return request<Resource<Project>>("/projects", {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(input),
    onStatus: (value) => (status = value),
  }).then((result) => ({
    ...result,
    // 链接已存在时服务端返回 200 和已有项目，表单内容不会写入。
    existing: status === 200,
  }));
}
export function saveEvidence(
  projectId: string,
  input: EvidenceInput,
  key: string,
) {
  return request<Resource<Evidence>>(`/projects/${projectId}/evidence`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(input),
  });
}
export function listAnalyses(
  projectId: string,
  level: AnalysisLevel,
  signal?: AbortSignal,
) {
  return request<Page<AnalysisVersion>>(
    `/projects/${encodeURIComponent(projectId)}/analyses?level=${level}&page=1&pageSize=20`,
    { signal },
  );
}
export function createAnalysis(
  projectId: string,
  input: AnalysisContent & {
    level: AnalysisLevel;
    projectRevision: number;
    basedOnAnalysisId?: string;
  },
  key: string,
) {
  return request<Resource<AnalysisVersion>>(
    `/projects/${encodeURIComponent(projectId)}/analyses`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      body: JSON.stringify(input),
    },
  );
}
export function runRulesAnalysis(
  projectId: string,
  input: {
    level: AnalysisLevel;
    projectRevision: number;
    evidenceIds: string[];
  },
  key: string,
) {
  return request<Resource<AnalysisRun>>(
    `/projects/${encodeURIComponent(projectId)}/analysis-runs`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      body: JSON.stringify({ ...input, mode: "RULES" }),
    },
  );
}
export function runModelAnalysis(
  projectId: string,
  input: {
    level: AnalysisLevel;
    projectRevision: number;
    evidenceIds: string[];
  },
  key: string,
) {
  return request<Resource<AnalysisRun>>(
    `/projects/${encodeURIComponent(projectId)}/analysis-runs`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      body: JSON.stringify({ ...input, mode: "MODEL" }),
    },
  );
}
export function getAnalysisRun(id: string, signal?: AbortSignal) {
  return request<Resource<AnalysisRun>>(
    `/analysis-runs/${encodeURIComponent(id)}`,
    { signal },
  );
}
export function getAnalysisCapabilities(signal?: AbortSignal) {
  return request<Resource<AnalysisCapabilities>>("/analysis-capabilities", {
    signal,
  });
}
export function getResearch(projectId: string, signal?: AbortSignal) {
  return request<Resource<ResearchEntry>>(
    `/projects/${encodeURIComponent(projectId)}/research`,
    { signal },
  );
}
export function saveResearch(
  projectId: string,
  fields: ResearchFields,
  key: string,
  current?: ResearchEntry,
) {
  return request<Resource<ResearchEntry>>(
    `/projects/${encodeURIComponent(projectId)}/research`,
    current
      ? {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            "If-Match": `"${current.revision}"`,
          },
          body: JSON.stringify(fields),
        }
      : {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": key,
          },
          body: JSON.stringify(fields),
        },
  );
}
export function listResearch(params: URLSearchParams, signal?: AbortSignal) {
  return request<Page<ResearchEntry>>(`/research?${params}`, { signal });
}
export function getResearchPreferences(signal?: AbortSignal) {
  return request<Resource<ResearchPreferences>>("/research-preferences", {
    signal,
  });
}
export function updateResearchPreferences(
  current: ResearchPreferences,
  patch: Partial<Omit<ResearchPreferences, "revision">>,
) {
  return request<Resource<ResearchPreferences>>("/research-preferences", {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      "If-Match": `"${current.revision}"`,
    },
    body: JSON.stringify(patch),
  });
}
export function listExperiments(
  projectId: string,
  page = 1,
  signal?: AbortSignal,
) {
  return request<Page<Experiment>>(
    `/projects/${encodeURIComponent(projectId)}/experiments?page=${page}&pageSize=20`,
    { signal },
  );
}
export function getExperiment(id: string) {
  return request<Resource<Experiment>>(
    `/experiments/${encodeURIComponent(id)}`,
  );
}
export function saveExperiment(
  projectId: string,
  fields: ExperimentFields,
  key: string,
  current?: Experiment,
) {
  return request<Resource<Experiment>>(
    current
      ? `/experiments/${encodeURIComponent(current.id)}`
      : `/projects/${encodeURIComponent(projectId)}/experiments`,
    current
      ? {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            "If-Match": `"${current.revision}"`,
          },
          body: JSON.stringify(fields),
        }
      : {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": key,
          },
          body: JSON.stringify(fields),
        },
  );
}
export function listSources(signal?: AbortSignal) {
  return request<Page<SourceView>>("/sources?page=1&pageSize=20", { signal });
}
export function deriveSourceEvidence(projectId: string, key: string) {
  return request<Resource<{ created: number; evidence: Evidence[] }>>(
    `/projects/${encodeURIComponent(projectId)}/evidence/from-sources`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      body: "{}",
    },
  );
}
// 普通 <a download> 不会带访问令牌；用带凭据的请求取回文件，再交给浏览器保存（#45）。
export async function downloadBriefMarkdown(briefId: string) {
  const path = `/api/v1/briefs/${encodeURIComponent(briefId)}/markdown`;
  const response = await fetch(path, withAccessToken({}));
  if (!response.ok) {
    if (response.status === 401) {
      clearAccessToken();
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    }
    throw new ApiError(
      "Markdown 下载失败，请稍后重试。",
      response.status,
      "DOWNLOAD_FAILED",
    );
  }
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename =
    /filename="([^"]+)"/.exec(disposition)?.[1] ?? `tradar-brief-${briefId}.md`;
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}
export function getSource(source: RemoteSource, signal?: AbortSignal) {
  return request<Resource<SourceView>>(`/sources/${source}`, { signal });
}
export function updateSource(
  source: RemoteSource,
  revision: number,
  patch: Partial<Omit<SourceConfig, "source">>,
) {
  return request<Resource<SourceView>>(`/sources/${source}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      "If-Match": `"${revision}"`,
    },
    body: JSON.stringify(patch),
  });
}
export function listIngestionRuns(signal?: AbortSignal) {
  return request<Page<IngestionRun>>("/ingestion-runs?page=1&pageSize=20", {
    signal,
  });
}
export function triggerIngestion(source: RemoteSource, key: string) {
  return request<Resource<IngestionRun>>("/ingestion-runs", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": key,
    },
    body: JSON.stringify({ source }),
  });
}
export function listBriefs(page = 1, signal?: AbortSignal) {
  return request<Page<DailyBrief>>(`/briefs?page=${page}&pageSize=20`, {
    signal,
  });
}
export function createBrief(date: string, timeZone: string, key: string) {
  return request<Resource<BriefRun>>("/briefs", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": key,
    },
    body: JSON.stringify({ date, timeZone }),
  });
}
export function getBriefRun(id: string, signal?: AbortSignal) {
  return request<Resource<BriefRun>>(`/brief-runs/${encodeURIComponent(id)}`, {
    signal,
  });
}
export function getBriefSettings(signal?: AbortSignal) {
  return request<Resource<BriefSettings>>("/brief-settings", { signal });
}
export function updateBriefSettings(
  current: BriefSettings,
  patch: Partial<Omit<BriefSettings, "revision">>,
) {
  return request<Resource<BriefSettings>>("/brief-settings", {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      "If-Match": `"${current.revision}"`,
    },
    body: JSON.stringify(patch),
  });
}
export function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(value));
}
export function projectHost(url: string) {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}
