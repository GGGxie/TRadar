export const categories = {
  AI_AGENT: "AI Agent",
  DEVTOOLS: "开发者工具",
  SAAS: "SaaS / 产品",
  OTHER: "其他方向",
} as const;
export type Category = keyof typeof categories;
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
