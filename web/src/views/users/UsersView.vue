<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import {
  NButton, NCard, NDataTable, NModal, NForm, NFormItem, NInput, NSelect, NTag, useMessage, useDialog,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import client from '../../api/client'
import type { User } from '../../types'

const message = useMessage()
const dialog = useDialog()

const users = ref<User[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await client.get<User[]>('/users')
    users.value = data
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to load users')
  } finally {
    loading.value = false
  }
}
onMounted(load)

const roleOptions = [
  { label: 'Member', value: 'member' },
  { label: 'Tenant Admin', value: 'tenant_admin' },
]

// --- create ---
const showCreate = ref(false)
const createForm = ref({ email: '', password: '', displayName: '', role: 'member' })

function openCreate() {
  createForm.value = { email: '', password: '', displayName: '', role: 'member' }
  showCreate.value = true
}

async function submitCreate() {
  try {
    await client.post('/users', createForm.value)
    message.success('User created')
    showCreate.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to create user')
  }
}

// --- edit ---
const showEdit = ref(false)
const editTarget = ref<User | null>(null)
const editForm = ref({ displayName: '', role: 'member' })

function openEdit(u: User) {
  editTarget.value = u
  editForm.value = { displayName: u.displayName, role: u.role }
  showEdit.value = true
}

async function submitEdit() {
  if (!editTarget.value) return
  try {
    await client.patch(`/users/${editTarget.value.id}`, editForm.value)
    message.success('User updated')
    showEdit.value = false
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update user')
  }
}

// --- disable/enable ---
async function toggleStatus(u: User) {
  const status = u.status === 'disabled' ? 'active' : 'disabled'
  try {
    await client.patch(`/users/${u.id}`, { status })
    message.success(status === 'disabled' ? 'User disabled' : 'User enabled')
    await load()
  } catch (e: any) {
    message.error(e.response?.data?.error ?? 'Failed to update status')
  }
}

// --- delete ---
function confirmDelete(u: User) {
  dialog.warning({
    title: 'Delete user',
    content: `Delete user "${u.email}"? This cannot be undone.`,
    positiveText: 'Delete',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await client.delete(`/users/${u.id}`)
        message.success('User deleted')
        await load()
      } catch (e: any) {
        message.error(e.response?.data?.error ?? 'Failed to delete user')
      }
    },
  })
}

const columns: DataTableColumns<User> = [
  { title: 'Email', key: 'email' },
  { title: 'Name', key: 'displayName' },
  { title: 'Role', key: 'role' },
  {
    title: 'Status',
    key: 'status',
    render: (row) => h(NTag, { type: row.status === 'disabled' ? 'error' : 'success', size: 'small' }, { default: () => row.status }),
  },
  {
    title: '',
    key: 'actions',
    render: (row) =>
      h('div', { style: 'display:flex; gap:8px' }, [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit' }),
        h(NButton, { size: 'small', onClick: () => toggleStatus(row) }, { default: () => (row.status === 'disabled' ? 'Enable' : 'Disable') }),
        h(NButton, { size: 'small', type: 'error', onClick: () => confirmDelete(row) }, { default: () => 'Delete' }),
      ]),
  },
]
</script>

<template>
  <n-card title="Users">
    <template #header-extra>
      <n-button type="primary" size="small" @click="openCreate">+ Add User</n-button>
    </template>
    <n-data-table :columns="columns" :data="users" :loading="loading" :bordered="false" />
  </n-card>

  <n-modal v-model:show="showCreate" preset="card" title="Add User" style="width: 420px">
    <n-form>
      <n-form-item label="Email"><n-input v-model:value="createForm.email" /></n-form-item>
      <n-form-item label="Password"><n-input v-model:value="createForm.password" type="password" /></n-form-item>
      <n-form-item label="Display name"><n-input v-model:value="createForm.displayName" /></n-form-item>
      <n-form-item label="Role"><n-select v-model:value="createForm.role" :options="roleOptions" /></n-form-item>
      <n-button type="primary" block @click="submitCreate">Create</n-button>
    </n-form>
  </n-modal>

  <n-modal v-model:show="showEdit" preset="card" :title="`Edit — ${editTarget?.email}`" style="width: 420px">
    <n-form>
      <n-form-item label="Display name"><n-input v-model:value="editForm.displayName" /></n-form-item>
      <n-form-item label="Role"><n-select v-model:value="editForm.role" :options="roleOptions" /></n-form-item>
      <n-button type="primary" block @click="submitEdit">Save</n-button>
    </n-form>
  </n-modal>
</template>
