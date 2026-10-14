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
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}
export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  let result: Response;
  try {
    result = await fetch(`/api/v1${path}`, options);
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
    throw new ApiError(message, result.status, code);
  }
  return result.json() as Promise<T>;
}
export function saveProject(
  input: ProjectInput,
  key: string,
  project?: Project,
) {
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
  return request<Resource<Project>>("/projects", {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(input),
  });
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
