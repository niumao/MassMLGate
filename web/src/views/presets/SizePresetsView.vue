<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NInputNumber, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import type { SizePreset } from '../../types'
import { coresToMillicores, millicoresToCores, mbToBytes, bytesToMB, gbToBytes, bytesToGB } from '../../utils/units'

const message = useMessage()
const dialog = useDialog()

const presets = ref<SizePreset[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await client.get<SizePreset[]>('/size-presets')
    presets.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load size presets')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// --- create ---
const showCreate = ref(false)
const createForm = ref({ name: '', cpuRequest: 0, memoryRequest: 0, gpuRequest: 0, storage: 0 })

function openCreate() {
  createForm.value = { name: '', cpuRequest: 0, memoryRequest: 0, gpuRequest: 0, storage: 0 }
  showCreate.value = true
}

async function submitCreate() {
  try {
    const { name, cpuRequest, memoryRequest, gpuRequest, storage } = createForm.value
    await client.post('/size-presets', {
      name, gpuRequest,
      cpuRequest: coresToMillicores(cpuRequest), memoryRequest: mbToBytes(memoryRequest), storage: gbToBytes(storage),
    })
    message.success('Size preset created')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create size preset')
  }
}

// --- edit ---
const showEdit = ref(false)
const editTarget = ref<SizePreset | null>(null)
const editForm = ref({ name: '', cpuRequest: 0, memoryRequest: 0, gpuRequest: 0, storage: 0 })

function openEdit(p: SizePreset) {
  editTarget.value = p
  editForm.value = {
    name: p.name, gpuRequest: p.gpuRequest,
    cpuRequest: millicoresToCores(p.cpuRequest), memoryRequest: bytesToMB(p.memoryRequest), storage: bytesToGB(p.storage),
  }
  showEdit.value = true
}

async function submitEdit() {
  if (!editTarget.value) return
  try {
    const { name, cpuRequest, memoryRequest, gpuRequest, storage } = editForm.value
    await client.patch(`/size-presets/${editTarget.value.id}`, {
      name, gpuRequest,
      cpuRequest: coresToMillicores(cpuRequest), memoryRequest: mbToBytes(memoryRequest), storage: gbToBytes(storage),
    })
    message.success('Size preset updated')
    showEdit.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update size preset (renaming is blocked while a workspace uses it)')
  }
}

// --- delete ---
function confirmDelete(p: SizePreset) {
  dialog.warning({
    title: 'Delete size preset',
    content: `Delete size preset "${p.name}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/size-presets/${p.id}`)
        message.success('Size preset deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete size preset (it may be in use by a workspace)')
      }
    },
  })
}

const columns: DataTableColumns<SizePreset> = [
  { title: 'Name', key: 'name' },
  { title: 'CPU (cores)', key: 'cpuRequest', render: (row) => millicoresToCores(row.cpuRequest) },
  { title: 'Memory (MB)', key: 'memoryRequest', render: (row) => Math.round(bytesToMB(row.memoryRequest)) },
  { title: 'GPU', key: 'gpuRequest' },
  { title: 'Storage (GB)', key: 'storage', render: (row) => Math.round(bytesToGB(row.storage)) },
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
  <n-card title="Size Presets">
    <template #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ New Preset</n-button>
    </template>
    <n-data-table :columns="columns" :data="presets" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="New Size Preset" style="width: 420px">
    <n-form>
      <n-form-item label="Name"><n-input v-model:value="createForm.name" placeholder="small" /></n-form-item>
      <n-form-item label="CPU (cores)"><n-input-number v-model:value="createForm.cpuRequest" :min="0" :step="0.1" placeholder="0.5" style="width: 100%" /></n-form-item>
      <n-form-item label="Memory (MB)"><n-input-number v-model:value="createForm.memoryRequest" :min="0" placeholder="512" style="width: 100%" /></n-form-item>
      <n-form-item label="GPU"><n-input-number v-model:value="createForm.gpuRequest" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="Storage (GB)"><n-input-number v-model:value="createForm.storage" :min="0" placeholder="10" style="width: 100%" /></n-form-item>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showEdit" preset="card" :title="`Edit — ${editTarget?.name}`" style="width: 420px">
    <n-form>
      <n-form-item label="Name"><n-input v-model:value="editForm.name" /></n-form-item>
      <n-form-item label="CPU (cores)"><n-input-number v-model:value="editForm.cpuRequest" :min="0" :step="0.1" style="width: 100%" /></n-form-item>
      <n-form-item label="Memory (MB)"><n-input-number v-model:value="editForm.memoryRequest" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="GPU"><n-input-number v-model:value="editForm.gpuRequest" :min="0" style="width: 100%" /></n-form-item>
      <n-form-item label="Storage (GB)"><n-input-number v-model:value="editForm.storage" :min="0" style="width: 100%" /></n-form-item>
      <n-button type="primary" block @click="submitEdit">Save</n-button>
    </n-form>
  </n-modal>
</template>
