<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NSelect, NTag, NAlert, NSpace,
  useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import type { CreatedTestHook, TestHook, TestRun, Workspace } from '../../types'

const auth = useAuthStore()
const isMember = computed(() => auth.user?.role === 'member')
const message = useMessage()
const dialog = useDialog()

// --- workspace picker ---
const workspaces = ref<Workspace[]>([])
const selectedWorkspaceId = ref<string | null>(null)
const loadingWorkspaces = ref(false)

async function loadWorkspaces() {
  loadingWorkspaces.value = true
  try {
    const { data } = await client.get<Workspace[]>('/workspaces')
    workspaces.value = data
    if (!selectedWorkspaceId.value && data.length > 0) selectedWorkspaceId.value = data[0].id
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load workspaces')
  } finally {
    loadingWorkspaces.value = false
  }
}
onMounted(loadWorkspaces)

const workspaceOptions = computed(() => workspaces.value.map((w) => ({ label: w.name, value: w.id })))
const canManageSelectedWorkspace = computed(() => {
  const w = workspaces.value.find((w) => w.id === selectedWorkspaceId.value)
  return isMember.value && !!w && w.ownerUserId === auth.user?.id
})

// --- hooks for the selected workspace ---
const hooks = ref<TestHook[]>([])
const loadingHooks = ref(false)
const selectedHookId = ref<string | null>(null)

async function loadHooks() {
  if (!selectedWorkspaceId.value) return
  loadingHooks.value = true
  try {
    const { data } = await client.get<TestHook[]>(`/workspaces/${selectedWorkspaceId.value}/test-hooks`)
    hooks.value = data
    if (!selectedHookId.value || !data.some((h) => h.id === selectedHookId.value)) {
      selectedHookId.value = data[0]?.id ?? null
    }
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load test hooks')
  } finally {
    loadingHooks.value = false
  }
}
watch(selectedWorkspaceId, loadHooks)

// --- create hook ---
const showCreate = ref(false)
const createForm = ref({ name: '', testImage: '', testCommand: '', imagePullSecret: '' })

function openCreate() {
  createForm.value = { name: '', testImage: '', testCommand: '', imagePullSecret: '' }
  showCreate.value = true
}

const showToken = ref(false)
const createdHook = ref<CreatedTestHook | null>(null)
const ghActionsSnippet = computed(() => {
  if (!createdHook.value) return ''
  return `- name: Trigger platform test run
  run: |
    curl -X POST "${createdHook.value.triggerUrl}" \\
      -H "Content-Type: application/json" \\
      -d '{"commitSha":"\${{ github.sha }}","imageTag":"latest"}'`
})

async function submitCreate() {
  const { name, testImage, testCommand, imagePullSecret } = createForm.value
  if (!name || !testImage) {
    message.error('Name and test image are required')
    return
  }
  try {
    const { data } = await client.post<CreatedTestHook>(`/workspaces/${selectedWorkspaceId.value}/test-hooks`, {
      name,
      testImage,
      testCommand: testCommand.split(',').map((s) => s.trim()).filter(Boolean),
      imagePullSecret: imagePullSecret || undefined,
    })
    showCreate.value = false
    createdHook.value = data
    showToken.value = true
    await loadHooks()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create test hook')
  }
}

function copy(text: string) {
  navigator.clipboard.writeText(text)
  message.success('Copied')
}

function confirmRevoke(h: TestHook) {
  dialog.warning({
    title: 'Revoke test hook',
    content: `Revoke "${h.name}"? Any CI pipeline still using its token will stop being able to trigger runs.`,
    positiveText: 'Revoke',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.post(`/workspaces/${selectedWorkspaceId.value}/test-hooks/${h.id}/revoke`)
        message.success('Test hook revoked')
        await loadHooks()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to revoke test hook')
      }
    },
  })
}

const hookColumns = computed<DataTableColumns<TestHook>>(() => {
  const cols: DataTableColumns<TestHook> = [
    { title: 'Name', key: 'name' },
    { title: 'Test Image', key: 'testImage' },
    {
      title: 'Status',
      key: 'revoked',
      render: (row) => h(NTag, { type: row.revoked ? 'error' : 'success', size: 'small' }, { default: () => (row.revoked ? 'revoked' : 'active') }),
    },
    {
      title: 'Last Triggered',
      key: 'lastTriggeredAt',
      render: (row) => row.lastTriggeredAt ?? 'never',
    },
    {
      title: '',
      key: 'actions',
      render: (row) =>
        h('div', { style: 'display:flex; gap:8px' }, [
          h(NButton, { size: 'small', onClick: () => (selectedHookId.value = row.id) }, { default: () => 'View runs' }),
          canManageSelectedWorkspace.value && !row.revoked
            ? h(NButton, { size: 'small', type: 'error', onClick: () => confirmRevoke(row) }, { default: () => 'Revoke' })
            : null,
        ]),
    },
  ]
  return cols
})

// --- run history for the selected hook ---
const runs = ref<TestRun[]>([])
const loadingRuns = ref(false)

async function loadRuns() {
  if (!selectedWorkspaceId.value || !selectedHookId.value) {
    runs.value = []
    return
  }
  loadingRuns.value = true
  try {
    const { data } = await client.get<TestRun[]>(
      `/workspaces/${selectedWorkspaceId.value}/test-hooks/${selectedHookId.value}/runs`,
    )
    runs.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load run history')
  } finally {
    loadingRuns.value = false
  }
}
watch(selectedHookId, loadRuns)

// Poll while any run for the selected hook is still pending/running.
let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  pollTimer = setInterval(() => {
    if (runs.value.some((r) => r.status === 'pending' || r.status === 'running')) loadRuns()
  }, 3000)
})
onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})

const statusType: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
  passed: 'success',
  running: 'warning',
  pending: 'default',
  failed: 'error',
}

const showLogs = ref(false)
const logsText = ref('')
async function viewLogs(run: TestRun) {
  try {
    const { data } = await client.get<{ logs: string }>(
      `/workspaces/${selectedWorkspaceId.value}/test-hooks/${selectedHookId.value}/runs/${run.id}/logs`,
    )
    logsText.value = data.logs
    showLogs.value = true
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Logs unavailable (the pod may have been garbage-collected)')
  }
}

const runColumns: DataTableColumns<TestRun> = [
  { title: 'Commit', key: 'commitSha', render: (row) => row.commitSha ?? '—' },
  { title: 'Image Tag', key: 'imageTag', render: (row) => row.imageTag ?? '—' },
  {
    title: 'Status',
    key: 'status',
    render: (row) => h(NTag, { type: statusType[row.status], size: 'small' }, { default: () => row.status }),
  },
  { title: 'Created', key: 'createdAt', render: (row) => new Date(row.createdAt).toLocaleString() },
  { title: 'Finished', key: 'finishedAt', render: (row) => (row.finishedAt ? new Date(row.finishedAt).toLocaleString() : '—') },
  {
    title: '',
    key: 'actions',
    render: (row) => h(NButton, { size: 'small', onClick: () => viewLogs(row) }, { default: () => 'View logs' }),
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-card title="Test Runs">
      <template #header-extra>
        <n-select
          v-model:value="selectedWorkspaceId"
          :options="workspaceOptions"
          placeholder="Select a workspace"
          style="width: 240px"
        />
      </template>

      <n-card title="Test Hooks" :bordered="false" size="small">
        <template v-if="canManageSelectedWorkspace" #header-extra>
          <n-button type="primary" size="small" :disabled="!selectedWorkspaceId" @click="openCreate">+ New Hook</n-button>
        </template>
        <n-data-table :columns="hookColumns" :data="hooks" :loading="loadingHooks" :bordered="false" />
      </n-card>

      <n-card title="Run History" :bordered="false" size="small" style="margin-top: 16px">
        <n-data-table :columns="runColumns" :data="runs" :loading="loadingRuns" :bordered="false" />
      </n-card>
    </n-card>
  </n-space>

  <n-modal v-model:show="showCreate" preset="card" title="New Test Hook" style="width: 480px">
    <n-form>
      <n-form-item label="Name"><n-input v-model:value="createForm.name" placeholder="unit-tests" /></n-form-item>
      <n-form-item label="Test Image">
        <n-input v-model:value="createForm.testImage" placeholder="registry.example.com/my-tenant/test-runner:latest" />
      </n-form-item>
      <n-form-item label="Test Command (comma-separated)">
        <n-input v-model:value="createForm.testCommand" placeholder="pytest, -q" />
      </n-form-item>
      <n-form-item label="Image Pull Secret (optional)">
        <n-input v-model:value="createForm.imagePullSecret" placeholder="name of an existing k8s pull secret" />
      </n-form-item>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showToken" preset="card" title="Test hook created" style="width: 560px">
    <n-alert type="warning" style="margin-bottom: 12px">
      This token is shown only once. Copy it into your CI's secrets now — it cannot be retrieved again (you'd have to create a new hook).
    </n-alert>
    <n-form>
      <n-form-item label="Trigger URL">
        <n-input :value="createdHook?.triggerUrl" readonly />
        <n-button size="small" style="margin-left: 8px" @click="copy(createdHook?.triggerUrl ?? '')">Copy</n-button>
      </n-form-item>
      <p style="font-size: 12px; opacity: 0.7; margin-bottom: 8px">Paste this as the final step of your GitHub Actions workflow:</p>
      <pre style="background: rgba(128,128,128,0.1); padding: 12px; border-radius: 6px; overflow-x: auto; font-size: 12px">{{ ghActionsSnippet }}</pre>
      <n-button size="small" @click="copy(ghActionsSnippet)">Copy snippet</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showLogs" preset="card" title="Run logs" style="width: 640px">
    <pre style="background: rgba(128,128,128,0.1); padding: 12px; border-radius: 6px; overflow-x: auto; font-size: 12px; max-height: 400px">{{ logsText }}</pre>
  </n-modal>
</template>
