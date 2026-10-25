<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NInputNumber, NTag, NSelect, NSpace, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import type { Tenant, AlertReceiver } from '../../types'
import { coresToMillicores, millicoresToCores, mbToBytes, bytesToMB } from '../../utils/units'

const message = useMessage()
const dialog = useDialog()

const tenants = ref<Tenant[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await client.get<Tenant[]>('/tenants')
    tenants.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load tenants')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// --- create ---
const showCreate = ref(false)
const createForm = ref({ name: '', namespace: '', quotaCpu: 0, quotaMemory: 0, quotaGpu: 0 })

function openCreate() {
  createForm.value = { name: '', namespace: '', quotaCpu: 1, quotaMemory: 1024, quotaGpu: 0 }
  showCreate.value = true
}

async function submitCreate() {
  try {
    const { quotaCpu, quotaMemory, quotaGpu, ...rest } = createForm.value
    await client.post('/tenants', { ...rest, quotaCpu: coresToMillicores(quotaCpu), quotaMemory: mbToBytes(quotaMemory), quotaGpu })
    message.success('Tenant created')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create tenant')
  }
}

// --- edit quota ---
const showEdit = ref(false)
const editTarget = ref<Tenant | null>(null)
const editForm = ref({ quotaCpu: 0, quotaMemory: 0, quotaGpu: 0 })

function openEdit(t: Tenant) {
  editTarget.value = t
  editForm.value = { quotaCpu: millicoresToCores(t.quotaCpu), quotaMemory: bytesToMB(t.quotaMemory), quotaGpu: t.quotaGpu }
  showEdit.value = true
}

async function submitEdit() {
  if (!editTarget.value) return
  try {
    const { quotaCpu, quotaMemory, quotaGpu } = editForm.value
    await client.patch(`/tenants/${editTarget.value.id}`, { quotaCpu: coresToMillicores(quotaCpu), quotaMemory: mbToBytes(quotaMemory), quotaGpu })
    message.success('Quota updated')
    showEdit.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update quota')
  }
}

// --- delete ---
function confirmDelete(t: Tenant) {
  dialog.warning({
    title: 'Delete tenant',
    content: `Delete tenant "${t.name}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/tenants/${t.id}`)
        message.success('Tenant deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete tenant')
      }
    },
  })
}

// --- cluster-wide Kueue setup ---
const kueueForm = ref({ quotaCpu: 16, quotaMemory: 16384, quotaGpu: 1 })
const kueueLoading = ref(false)

async function submitKueueSetup() {
  kueueLoading.value = true
  try {
    const { quotaCpu, quotaMemory, quotaGpu } = kueueForm.value
    await client.post('/cluster/kueue-setup', { quotaCpu: coresToMillicores(quotaCpu), quotaMemory: mbToBytes(quotaMemory), quotaGpu })
    message.success('Cluster ResourceFlavor + ClusterQueue applied')
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to apply cluster Kueue setup')
  } finally {
    kueueLoading.value = false
  }
}

// --- alert receivers (Alertmanager routing) ---
const receivers = ref<AlertReceiver[]>([])
const receiversLoading = ref(false)
const receiversSaving = ref(false)

async function loadReceivers() {
  receiversLoading.value = true
  try {
    const { data } = await client.get<AlertReceiver[]>('/cluster/alerting')
    receivers.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load alert receivers')
  } finally {
    receiversLoading.value = false
  }
}
onMounted(loadReceivers)

function addReceiver() {
  receivers.value.push({ name: '', type: 'webhook', target: '' })
}

function removeReceiver(index: number) {
  receivers.value.splice(index, 1)
}

async function saveReceivers() {
  receiversSaving.value = true
  try {
    await client.put('/cluster/alerting', receivers.value)
    message.success('Alert receivers saved — every alert routes to all receivers listed')
    await loadReceivers()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to save alert receivers')
  } finally {
    receiversSaving.value = false
  }
}

const receiverTypeOptions = [
  { label: 'Slack webhook', value: 'slack' },
  { label: 'Generic webhook', value: 'webhook' },
]

const columns: DataTableColumns<Tenant> = [
  { title: 'Name', key: 'name' },
  { title: 'Namespace', key: 'namespace' },
  { title: 'CPU (cores)', key: 'quotaCpu', render: (row) => millicoresToCores(row.quotaCpu) },
  { title: 'Memory (MB)', key: 'quotaMemory', render: (row) => Math.round(bytesToMB(row.quotaMemory)) },
  { title: 'GPU', key: 'quotaGpu' },
  {
    title: 'Status',
    key: 'status',
    render: (row) => h(NTag, { type: row.status === 'active' ? 'success' : 'error', size: 'small' }, { default: () => row.status }),
  },
  {
    title: '',
    key: 'actions',
    render: (row) =>
      h('div', { style: 'display:flex; gap:8px' }, [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit quota' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => confirmDelete(row) }, { default: () => 'Delete' }),
      ]),
  },
]
</script>

<template>
  <n-card title="Cluster Setup" style="margin-bottom: 16px">
    <p style="font-size: 12px; opacity: 0.7; margin-top: 0">
      Creates (or updates the quota on) the cluster-wide Kueue ResourceFlavor + ClusterQueue every
      tenant's per-namespace LocalQueue binds to. Run once per cluster, and again whenever total
      capacity changes.
    </p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap">
      <div>
        <div style="font-size: 12px; margin-bottom: 4px">CPU quota (cores)</div>
        <n-input-number v-model:value="kueueForm.quotaCpu" :min="0" :step="0.1" style="width: 180px" />
      </div>
      <div>
        <div style="font-size: 12px; margin-bottom: 4px">Memory quota (MB)</div>
        <n-input-number v-model:value="kueueForm.quotaMemory" :min="0" style="width: 180px" />
      </div>
      <div>
        <div style="font-size: 12px; margin-bottom: 4px">GPU quota</div>
        <n-input-number v-model:value="kueueForm.quotaGpu" style="width: 180px" />
      </div>
      <n-button type="primary" :loading="kueueLoading" @click="submitKueueSetup">Apply Kueue Setup</n-button>
    </div>
  </n-card>

  <n-card title="Alert Routing" style="margin-bottom: 16px">
    <p style="font-size: 12px; opacity: 0.7; margin-top: 0">
      Every receiver below gets every alert (no custom routing tree). Writes to the cluster's
      AlertmanagerConfig CRD — requires Prometheus Operator's Alertmanager deployed with a matching
      <code>alertmanagerConfigSelector</code>.
    </p>
    <n-space vertical>
      <n-space v-for="(r, i) in receivers" :key="i" align="center">
        <n-input v-model:value="r.name" placeholder="Name" style="width: 140px" />
        <n-select v-model:value="r.type" :options="receiverTypeOptions" style="width: 160px" />
        <n-input
          v-model:value="r.target"
          :placeholder="r.type === 'slack' ? 'Slack incoming webhook URL' : 'Webhook URL'"
          style="width: 360px"
        />
        <n-button size="small" type="error" @click="removeReceiver(i)">Remove</n-button>
      </n-space>
      <n-space>
        <n-button size="small" @click="addReceiver">+ Add Receiver</n-button>
        <n-button type="primary" size="small" :loading="receiversSaving" @click="saveReceivers">Save</n-button>
      </n-space>
    </n-space>
  </n-card>

  <n-card title="Tenants">
    <template #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ New Tenant</n-button>
    </template>
    <n-data-table :columns="columns" :data="tenants" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="New Tenant" style="width: 420px">
    <n-form>
      <n-form-item label="Name"><n-input v-model:value="createForm.name" /></n-form-item>
      <n-form-item label="Namespace slug"><n-input v-model:value="createForm.namespace" /></n-form-item>
      <n-form-item label="CPU quota (cores)"><n-input-number v-model:value="createForm.quotaCpu" :min="0" :step="0.1" style="width: 100%" /></n-form-item>
      <n-form-item label="Memory quota (MB)"><n-input-number v-model:value="createForm.quotaMemory" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="GPU quota"><n-input-number v-model:value="createForm.quotaGpu" style="width: 100%" /></n-form-item>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showEdit" preset="card" :title="`Edit quota — ${editTarget?.name}`" style="width: 420px">
    <n-form>
      <n-form-item label="CPU quota (cores)"><n-input-number v-model:value="editForm.quotaCpu" :min="0" :step="0.1" style="width: 100%" /></n-form-item>
      <n-form-item label="Memory quota (MB)"><n-input-number v-model:value="editForm.quotaMemory" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="GPU quota"><n-input-number v-model:value="editForm.quotaGpu" style="width: 100%" /></n-form-item>
      <n-button type="primary" block @click="submitEdit">Save</n-button>
    </n-form>
  </n-modal>
</template>
