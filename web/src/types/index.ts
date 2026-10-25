export type Role = 'system_admin' | 'tenant_admin' | 'member'

export interface User {
  id: string
  email: string
  displayName: string
  role: Role
  tenantId: string | null
  status: 'active' | 'disabled'
}

export interface MenuItem {
  key: string
  label: string
  path: string
}

export interface Tenant {
  id: string
  name: string
  namespace: string
  quotaCpu: number
  quotaMemory: number
  quotaGpu: number
  status: 'active' | 'provisioning_failed' | 'deleting'
}

export interface Team {
  id: string
  tenantId: string
  name: string
  quotaCpu: number
  quotaMemory: number
  quotaGpu: number
}

export interface SizePreset {
  id: string
  tenantId: string
  name: string
  cpuRequest: number
  memoryRequest: number
  gpuRequest: number
  storage: number
}

export interface TokenQuota {
  id: string
  tenantId: string
  modelName: string
  tokenLimit: number
  window: 'daily' | 'monthly'
}

export type WorkspaceStatus = 'pending' | 'running' | 'stopped' | 'failed'

export interface Workspace {
  id: string
  tenantId: string
  teamId: string
  ownerUserId: string
  name: string
  imageKey: 'jupyter' | 'vscode'
  size: string
  status: WorkspaceStatus
  errorMessage?: string
}

export interface WorkspaceCatalog {
  images: string[]
  sizes: { size: string; cpu: number; memory: number; gpu: number }[]
  teamQuota: { cpu: number; memory: number; gpu: number }
}

export interface TestHook {
  id: string
  workspaceId: string
  name: string
  testImage: string
  testCommand: string[]
  imagePullSecret?: string
  revoked: boolean
  lastTriggeredAt?: string
}

// Only Create's response includes these — the raw token is never shown
// again after this, and never appears on TestHook/List.
export interface CreatedTestHook extends TestHook {
  token: string
  triggerUrl: string
}

export type TestRunStatus = 'pending' | 'running' | 'passed' | 'failed'

export interface TestRun {
  id: string
  testHookId: string
  workspaceId: string
  commitSha?: string
  imageTag?: string
  status: TestRunStatus
  errorMessage?: string
  createdAt: string
  finishedAt?: string
}

export interface AlertReceiver {
  name: string
  type: 'slack' | 'webhook'
  target: string
}
