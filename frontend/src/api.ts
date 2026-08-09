// Minimal fetch client with session cookie + CSRF handling.
const CSRF_COOKIE = 'aihub_csrf'

function readCookie(name: string): string {
  const m = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'))
  return m ? decodeURIComponent(m[1]) : ''
}

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  let payload: string | undefined
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  const csrf = readCookie(CSRF_COOKIE)
  if (csrf) headers['X-CSRF-Token'] = csrf
  const resp = await fetch(path, { method, headers, body: payload, credentials: 'same-origin' })
  const data = await resp.json().catch(() => ({}))
  if (!resp.ok) {
    const err = data?.error
    throw new ApiError(resp.status, err?.code || 'error', err?.message || `请求失败 (${resp.status})`)
  }
  return data?.data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

export interface PageResult<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface ModuleInfo {
  id: string
  version: string
  dependsOn: string[]
  enabled: boolean
  tools: string[]
}

export interface Category {
  id: number
  parentId?: number
  projectId?: number
  name: string
  slug: string
  icon: string
  description: string
  sortOrder: number
  archived: boolean
  schemaId?: number
  schema?: { id: number; version: number; schema: Record<string, any>; createdAt: string }
}

export interface Prompt {
  id: number
  projectId?: number
  categoryId: number
  categoryName?: string
  slug: string
  title: string
  description: string
  tags: string[]
  status: string
  currentVersion?: PromptVersion
  draft?: { content: Record<string, any>; variables: string[]; summary: string }
  createdAt: string
  updatedAt: string
}

export interface PromptVersion {
  id: number
  version: number
  content: Record<string, any>
  variables: string[]
  summary: string
  schemaId: number
  createdAt: string
}

export interface Skill {
  id: number
  projectId?: number
  name: string
  slug: string
  description: string
  category: string
  tags: string[]
  status: string
  currentVersion?: SkillVersion
  createdAt: string
  updatedAt: string
}

export interface SkillVersion {
  id: number
  version: number
  sha256: string
  size: number
  rootDir: string
  files: string[]
  changelog: string
  createdAt: string
}

export interface ExpertPack {
  id: number
  name: string
  slug: string
  description: string
  domain: string
  responsibility: string
  usage: string
  status: string
  currentVersion?: any
  createdAt: string
  updatedAt: string
}

export interface MCPDefinition {
  id: number
  name: string
  slug: string
  description: string
  category: string
  tags: string[]
  transport: string
  status: string
  currentVersion?: {
    id: number
    version: number
    config: Record<string, any>
    envVars: Record<string, any>[]
    tools: Record<string, any>[]
  }
}

export interface MCPProfile {
  id: number
  name: string
  slug: string
  description: string
  scope: string
  projectId?: number
  status: string
  items?: MCPProfileItem[]
  createdAt: string
  updatedAt: string
}

export interface MCPProfileItem {
  id: number
  definitionId: number
  definitionSlug: string
  definitionName: string
  definitionVersionId: number
  version: number
  transport: string
  enabledTools: string[]
  disabledTools: string[]
  position: number
  config: Record<string, any>
  envVars: Record<string, any>[]
  tools: Record<string, any>[]
}

export interface Project {
  id: number
  name: string
  slug: string
  description: string
  scope: string
  archived: boolean
  createdAt: string
  updatedAt: string
}

export interface APIToken {
  id: number
  name: string
  scopes: string[]
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
  revoked: boolean
}
