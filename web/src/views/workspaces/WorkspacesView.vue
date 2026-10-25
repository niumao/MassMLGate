<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NSelect, NTag, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import type { Team, Workspace, WorkspaceCatalog } from '../../types'
import { millicoresToCores, bytesToMB } from '../../utils/units'

const auth = useAuthStore()
const isMember = computed(() => auth.user?.role === 'member')
const message = useMessage()
const dialog = useDialog()

const workspaces = ref<Workspace[]>([])
const myTeams = ref<Team[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await client.get<Workspace[]>('/workspaces')
    workspaces.value = data
    if (isMember.value) {
      const teamsRes = await client.get<Team[]>('/teams')
      myTeams.value = teamsRes.data
    }
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load workspaces')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// Poll while any workspace is still pending, so create/start show Running
// once the (simulated or real) K8s provisioning finishes.
let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  pollTimer = setInterval(() => {
    if (workspaces.value.some((w) => w.status === 'pending')) load()
  }, 3000)
})
onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})

const teamName = (id: string) => myTeams.value.find((t) => t.id === id)?.name ?? id

// --- create ---
const showCreate = ref(false)
const createForm = ref({ teamId: null as string | null, name: '', imageKey: null as string | null, size: null as string | null })
const catalog = ref<WorkspaceCatalog | null>(null)

function openCreate() {
  createForm.value = { teamId: myTeams.value[0]?.id ?? null, name: '', imageKey: null, size: null }
  catalog.value = null
  showCreate.value = true
  if (createForm.value.teamId) loadCatalog()
}

async function loadCatalog() {
  if (!createForm.value.teamId) return
  const { data } = await client.get<WorkspaceCatalog>('/workspace-catalog', { params: { team_id: createForm.value.teamId } })
  catalog.value = data
}

async function submitCreate() {
  const { teamId, name, imageKey, size } = createForm.value
  if (!teamId || !name || !imageKey || !size) {
    message.error('All fields are required')
    return
  }
  try {
    await client.post('/workspaces', { teamId, name, imageKey, size })
    message.success('Workspace created — provisioning...')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create workspace')
  }
}

const teamOptions = () => myTeams.value.map((t) => ({ label: t.name, value: t.id }))
const imageOptions = () => (catalog.value?.images ?? []).map((i) => ({ label: i, value: i }))
const sizeOptions = () =>
  (catalog.value?.sizes ?? []).map((s) => ({
    label: `${s.size} (${millicoresToCores(s.cpu)} CPU, ${Math.round(bytesToMB(s.memory))}MB mem${s.gpu ? `, ${s.gpu} GPU` : ''})`,
    value: s.size,
  }))

// --- actions ---
async function start(w: Workspace) {
  try {
    await client.post(`/workspaces/${w.id}/start`)
    message.success('Starting...')
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to start')
  }
}

async function stop(w: Workspace) {
  try {
    await client.post(`/workspaces/${w.id}/stop`)
    message.success('Stopped')
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to stop')
  }
}

function confirmDelete(w: Workspace) {
  dialog.warning({
    title: 'Delete workspace',
    content: `Delete workspace "${w.name}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/workspaces/${w.id}`)
        message.success('Workspace deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete workspace')
      }
    },
  })
}

const statusType: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
  running: 'success',
  pending: 'warning',
  stopped: 'default',
  failed: 'error',
}

const columns = computed<DataTableColumns<Workspace>>(() => {
  const cols: DataTableColumns<Workspace> = [
    { title: 'Name', key: 'name' },
    { title: 'Team', key: 'teamId', render: (row) => teamName(row.teamId) },
    { title: 'Image', key: 'imageKey' },
    { title: 'Size', key: 'size' },
    {
      title: 'Status',
      key: 'status',
      render: (row) => h(NTag, { type: statusType[row.status], size: 'small' }, { default: () => row.status }),
    },
  ]
  if (isMember.value) {
    cols.push({
      title: '',
      key: 'actions',
      render: (row) =>
        h('div', { style: 'display:flex; gap:8px' }, [
          row.status === 'running'
            ? h(NButton, { size: 'small', onClick: () => stop(row) }, { default: () => 'Stop' })
            : h(NButton, { size: 'small', disabled: row.status === 'pending', onClick: () => start(row) }, { default: () => 'Start' }),
          h(NButton, { size: 'small', type: 'error', onClick: () => confirmDelete(row) }, { default: () => 'Delete' }),
        ]),
    })
  }
  return cols
})
</script>

<template>
  <n-card :title="isMember ? 'My Workspaces' : 'Workspaces (tenant-wide, read-only)'">
    <template v-if="isMember" #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ New Workspace</n-button>
    </template>
    <n-data-table :columns="columns" :data="workspaces" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="New Workspace" style="width: 440px">
    <n-form>
      <n-form-item label="Team">
        <n-select v-model:value="createForm.teamId" :options="teamOptions()" @update:value="loadCatalog" />
      </n-form-item>
      <n-form-item label="Name"><n-input v-model:value="createForm.name" placeholder="my-notebook" /></n-form-item>
      <n-form-item label="Image">
        <n-select v-model:value="createForm.imageKey" :options="imageOptions()" placeholder="Select image" />
      </n-form-item>
      <n-form-item label="Size">
        <n-select v-model:value="createForm.size" :options="sizeOptions()" placeholder="Select size" />
      </n-form-item>
      <p v-if="catalog" style="font-size: 12px; opacity: 0.7; margin-bottom: 16px">
        Team quota: {{ millicoresToCores(catalog.teamQuota.cpu) }} CPU / {{ Math.round(bytesToMB(catalog.teamQuota.memory)) }}MB mem / {{ catalog.teamQuota.gpu }} GPU
      </p>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>
</template>
