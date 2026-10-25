<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NInputNumber, NSelect, useMessage, useDialog, NList, NListItem, NThing,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import type { Team, User } from '../../types'
import { coresToMillicores, millicoresToCores, mbToBytes, bytesToMB } from '../../utils/units'

const message = useMessage()
const dialog = useDialog()

const teams = ref<Team[]>([])
const users = ref<User[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const [teamsRes, usersRes] = await Promise.all([client.get<Team[]>('/teams'), client.get<User[]>('/users')])
    teams.value = teamsRes.data
    users.value = usersRes.data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load teams')
  } finally {
    loading.value = false
  }
}
onMounted(load)

function userLabel(id: string) {
  return users.value.find((u) => u.id === id)?.email ?? id
}

// --- create ---
const showCreate = ref(false)
const createForm = ref({ name: '', quotaCpu: 0, quotaMemory: 0, quotaGpu: 0 })

function openCreate() {
  createForm.value = { name: '', quotaCpu: 0, quotaMemory: 0, quotaGpu: 0 }
  showCreate.value = true
}

async function submitCreate() {
  try {
    const { quotaCpu, quotaMemory, quotaGpu, ...rest } = createForm.value
    await client.post('/teams', { ...rest, quotaCpu: coresToMillicores(quotaCpu), quotaMemory: mbToBytes(quotaMemory), quotaGpu })
    message.success('Team created')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create team')
  }
}

// --- edit quota ---
const showEdit = ref(false)
const editTarget = ref<Team | null>(null)
const editForm = ref({ quotaCpu: 0, quotaMemory: 0, quotaGpu: 0 })

function openEdit(t: Team) {
  editTarget.value = t
  editForm.value = { quotaCpu: millicoresToCores(t.quotaCpu), quotaMemory: bytesToMB(t.quotaMemory), quotaGpu: t.quotaGpu }
  showEdit.value = true
}

async function submitEdit() {
  if (!editTarget.value) return
  try {
    const { quotaCpu, quotaMemory, quotaGpu } = editForm.value
    await client.patch(`/teams/${editTarget.value.id}`, { quotaCpu: coresToMillicores(quotaCpu), quotaMemory: mbToBytes(quotaMemory), quotaGpu })
    message.success('Quota updated')
    showEdit.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update quota')
  }
}

// --- delete ---
function confirmDelete(t: Team) {
  dialog.warning({
    title: 'Delete team',
    content: `Delete team "${t.name}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/teams/${t.id}`)
        message.success('Team deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete team (it may still have workspaces)')
      }
    },
  })
}

// --- members ---
const showMembers = ref(false)
const memberTarget = ref<Team | null>(null)
const memberIds = ref<string[]>([])
const addMemberUserId = ref<string | null>(null)

async function openMembers(t: Team) {
  memberTarget.value = t
  showMembers.value = true
  addMemberUserId.value = null
  const { data } = await client.get(`/teams/${t.id}/members`)
  memberIds.value = data.userIds ?? []
}

async function addMember() {
  if (!memberTarget.value || !addMemberUserId.value) return
  await client.post(`/teams/${memberTarget.value.id}/members/${addMemberUserId.value}`)
  await openMembers(memberTarget.value)
}

async function removeMember(userId: string) {
  if (!memberTarget.value) return
  await client.delete(`/teams/${memberTarget.value.id}/members/${userId}`)
  await openMembers(memberTarget.value)
}

const userOptions = () => users.value.map((u) => ({ label: u.email, value: u.id }))

const columns: DataTableColumns<Team> = [
  { title: 'Name', key: 'name' },
  { title: 'CPU quota (cores)', key: 'quotaCpu', render: (row) => millicoresToCores(row.quotaCpu) },
  { title: 'Memory quota (MB)', key: 'quotaMemory', render: (row) => Math.round(bytesToMB(row.quotaMemory)) },
  { title: 'GPU quota', key: 'quotaGpu' },
  {
    title: '',
    key: 'actions',
    render: (row) =>
      h('div', { style: 'display:flex; gap:8px' }, [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit quota' }),
        h(NButton, { size: 'small', onClick: () => openMembers(row) }, { default: () => 'Members' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => confirmDelete(row) }, { default: () => 'Delete' }),
      ]),
  },
]
</script>

<template>
  <n-card title="Teams">
    <template #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ New Team</n-button>
    </template>
    <n-data-table :columns="columns" :data="teams" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="New Team" style="width: 420px">
    <n-form>
      <n-form-item label="Name"><n-input v-model:value="createForm.name" /></n-form-item>
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

  <n-modal v-model:show="showMembers" preset="card" :title="`Members — ${memberTarget?.name}`" style="width: 420px">
    <n-list>
      <n-list-item v-for="id in memberIds" :key="id">
        <n-thing :title="userLabel(id)" />
        <template #suffix>
          <n-button size="small" type="error" @click="removeMember(id)">Remove</n-button>
        </template>
      </n-list-item>
    </n-list>
    <div style="display:flex; gap:8px; margin-top:16px">
      <n-select v-model:value="addMemberUserId" :options="userOptions()" placeholder="Select user" style="flex:1" />
      <n-button type="primary" @click="addMember">Add</n-button>
    </div>
  </n-modal>
</template>
