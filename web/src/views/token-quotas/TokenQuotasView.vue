<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NInputNumber, NSelect, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns, SelectOption } from 'naive-ui'
import client from '../../api/client'
import type { TokenQuota } from '../../types'

const message = useMessage()
const dialog = useDialog()

const windowOptions: SelectOption[] = [
  { label: 'Daily', value: 'daily' },
  { label: 'Monthly', value: 'monthly' },
]

const quotas = ref<TokenQuota[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await client.get<TokenQuota[]>('/token-quotas')
    quotas.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load token quotas')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// --- create ---
const showCreate = ref(false)
const createForm = ref({ modelName: '', tokenLimit: 0, window: 'daily' as 'daily' | 'monthly' })

function openCreate() {
  createForm.value = { modelName: '', tokenLimit: 0, window: 'daily' }
  showCreate.value = true
}

async function submitCreate() {
  try {
    await client.post('/token-quotas', createForm.value)
    message.success('Token quota created')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create token quota')
  }
}

// --- edit ---
const showEdit = ref(false)
const editTarget = ref<TokenQuota | null>(null)
const editForm = ref({ modelName: '', tokenLimit: 0, window: 'daily' as 'daily' | 'monthly' })

function openEdit(q: TokenQuota) {
  editTarget.value = q
  editForm.value = { modelName: q.modelName, tokenLimit: q.tokenLimit, window: q.window }
  showEdit.value = true
}

async function submitEdit() {
  if (!editTarget.value) return
  try {
    await client.patch(`/token-quotas/${editTarget.value.id}`, editForm.value)
    message.success('Token quota updated')
    showEdit.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update token quota')
  }
}

// --- delete ---
function confirmDelete(q: TokenQuota) {
  dialog.warning({
    title: 'Delete token quota',
    content: `Delete the token quota for "${q.modelName}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/token-quotas/${q.id}`)
        message.success('Token quota deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete token quota')
      }
    },
  })
}

const columns: DataTableColumns<TokenQuota> = [
  { title: 'Model', key: 'modelName' },
  { title: 'Token limit', key: 'tokenLimit' },
  { title: 'Window', key: 'window' },
  {
    title: '',
    key: 'actions',
    render: (row) =>
      h('div', { style: 'display:flex; gap:8px' }, [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => confirmDelete(row) }, { default: () => 'Delete' }),
      ]),
  },
]
</script>

<template>
  <n-card title="Token Quotas">
    <template #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ New Quota</n-button>
    </template>
    <p style="font-size: 12px; opacity: 0.7; margin-top: 0">
      Per-model token budgets for this tenant, enforced by the AI gateway (agentrouter) at the
      request path — not just advisory numbers in this table.
    </p>
    <n-data-table :columns="columns" :data="quotas" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="New Token Quota" style="width: 420px">
    <n-form>
      <n-form-item label="Model"><n-input v-model:value="createForm.modelName" placeholder="gpt-4" /></n-form-item>
      <n-form-item label="Token limit"><n-input-number v-model:value="createForm.tokenLimit" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="Window"><n-select v-model:value="createForm.window" :options="windowOptions" /></n-form-item>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showEdit" preset="card" :title="`Edit — ${editTarget?.modelName}`" style="width: 420px">
    <n-form>
      <n-form-item label="Model"><n-input v-model:value="editForm.modelName" /></n-form-item>
      <n-form-item label="Token limit"><n-input-number v-model:value="editForm.tokenLimit" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="Window"><n-select v-model:value="editForm.window" :options="windowOptions" /></n-form-item>
      <n-button type="primary" block @click="submitEdit">Save</n-button>
    </n-form>
  </n-modal>
</template>
