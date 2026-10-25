<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { NGrid, NGi, NCard, NStatistic, NButton, NProgress, NSpace, NText, NDataTable, NTag, type DataTableColumns } from 'naive-ui'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
import client from '../../api/client'

const auth = useAuthStore()
const router = useRouter()
const role = computed(() => auth.user?.role)

const clusterMetrics = ref<any>(null)
const tenantMetrics = ref<any>(null)

onMounted(async () => {
  if (role.value === 'system_admin') {
    const { data } = await client.get('/metrics/cluster')
    clusterMetrics.value = data
  } else if (role.value === 'tenant_admin') {
    const { data } = await client.get('/metrics/tenant')
    tenantMetrics.value = data
  }
})

// --- resource breakdown formatting ---
const resourceRows = computed(() => {
  if (!clusterMetrics.value) return []
  const r = clusterMetrics.value.resources
  const u = clusterMetrics.value.utilization
  return [
    { key: 'cpu', label: 'CPU', unit: 'cores', divisor: 1000, ...r.cpu, util: u.cpu },
    { key: 'memory', label: 'Memory', unit: 'GiB', divisor: 1024 ** 3, ...r.memory, util: u.memory },
    { key: 'gpu', label: 'GPU', unit: 'GPUs', divisor: 1, ...r.gpu, util: u.gpu },
  ]
})

function fmt(value: number, divisor: number) {
  return divisor === 1 ? value : (value / divisor).toFixed(1)
}

function distributedPct(row: { capacity: number; distributed: number }) {
  if (row.capacity <= 0) return 0
  return Math.min(100, Math.round((row.distributed / row.capacity) * 100))
}

function progressStatus(pct: number): 'success' | 'warning' | 'error' {
  if (pct >= 95) return 'error'
  if (pct >= 80) return 'warning'
  return 'success'
}

// --- per-node breakdown ---
interface NodeRow {
  name: string
  ready: boolean
  schedulable: boolean
  cpuMilli: number
  memory: number
  gpu: number
}

const nodeRows = computed<NodeRow[]>(() => clusterMetrics.value?.nodes ?? [])

const nodeColumns: DataTableColumns<NodeRow> = [
  { title: 'Node', key: 'name' },
  {
    title: 'Status',
    key: 'status',
    render: (row) => {
      const ok = row.ready && row.schedulable
      const label = !row.ready ? 'NotReady' : !row.schedulable ? 'Cordoned' : 'Ready'
      return h(NTag, { type: ok ? 'success' : 'warning', size: 'small' }, { default: () => label })
    },
  },
  { title: 'CPU (cores)', key: 'cpuMilli', render: (row) => fmt(row.cpuMilli, 1000) },
  { title: 'Memory (GiB)', key: 'memory', render: (row) => fmt(row.memory, 1024 ** 3) },
  { title: 'GPU', key: 'gpu' },
]

// --- resource distribution by tenant (from DB quota rows, not Kueue) ---
interface TenantShareRow {
  tenantId: string
  tenantName: string
  quotaCpu: number
  quotaMemory: number
  quotaGpu: number
}

const tenantShareRows = computed<TenantShareRow[]>(() => clusterMetrics.value?.tenantShares ?? [])

const tenantShareColumns: DataTableColumns<TenantShareRow> = [
  { title: 'Tenant', key: 'tenantName' },
  { title: 'CPU quota (cores)', key: 'quotaCpu', render: (row) => fmt(row.quotaCpu, 1000) },
  { title: 'Memory quota (GiB)', key: 'quotaMemory', render: (row) => fmt(row.quotaMemory, 1024 ** 3) },
  { title: 'GPU quota', key: 'quotaGpu' },
]
</script>

<template>
  <!-- System Admin -->
  <template v-if="role === 'system_admin' && clusterMetrics">
    <n-grid :cols="3" :x-gap="16" style="margin-bottom: 24px">
      <n-gi><n-card><n-statistic label="Tenants" :value="clusterMetrics.tenantCount" /></n-card></n-gi>
      <n-gi><n-card><n-statistic label="Total Users" :value="clusterMetrics.userCount" /></n-card></n-gi>
      <n-gi><n-card><n-statistic label="Provisioning Failed" :value="clusterMetrics.provisioningFailed" /></n-card></n-gi>
    </n-grid>

    <n-card title="Cluster Resources" style="margin-bottom: 24px">
      <n-grid :cols="3" :x-gap="24">
        <n-gi v-for="row in resourceRows" :key="row.key">
          <n-space vertical size="small">
            <n-text strong>{{ row.label }}</n-text>
            <n-progress
              type="line"
              :percentage="distributedPct(row)"
              :status="progressStatus(distributedPct(row))"
              :height="10"
            />
            <n-text depth="3" style="font-size: 12px">
              {{ fmt(row.distributed, row.divisor) }} / {{ fmt(row.capacity, row.divisor) }} {{ row.unit }}
              distributed · {{ fmt(row.free, row.divisor) }} {{ row.unit }} free
            </n-text>
            <n-text v-if="row.util.available" depth="2" style="font-size: 12px">
              Live utilization: {{ row.util.pct.toFixed(1) }}%
            </n-text>
            <n-text v-else depth="3" style="font-size: 12px; font-style: italic">
              Live utilization unavailable (Prometheus not configured or exporter missing)
            </n-text>
          </n-space>
        </n-gi>
      </n-grid>
    </n-card>

    <n-card title="Nodes" style="margin-bottom: 24px">
      <n-data-table :columns="nodeColumns" :data="nodeRows" :bordered="false" />
    </n-card>

    <n-card title="Resource Distribution by Tenant" style="margin-bottom: 24px">
      <p style="font-size: 12px; opacity: 0.7; margin-top: 0">
        Each tenant's quota, straight from its Postgres record — the same source already summed
        into "distributed" above, not a live Kueue/cluster query.
      </p>
      <n-data-table :columns="tenantShareColumns" :data="tenantShareRows" :bordered="false" />
    </n-card>

    <n-card title="Manage Tenants">
      <n-button type="primary" @click="router.push('/tenants')">Go to Tenants</n-button>
    </n-card>
  </template>

  <!-- Tenant Admin -->
  <template v-else-if="role === 'tenant_admin' && tenantMetrics">
    <n-grid :cols="4" :x-gap="16" style="margin-bottom: 24px">
      <n-gi><n-card><n-statistic label="Teams" :value="tenantMetrics.teamCount" /></n-card></n-gi>
      <n-gi><n-card><n-statistic label="Users" :value="tenantMetrics.userCount" /></n-card></n-gi>
      <n-gi><n-card><n-statistic label="GPU quota (tenant)" :value="`${tenantMetrics.quotaGpu.used}/${tenantMetrics.quotaGpu.total} used`" /></n-card></n-gi>
      <n-gi><n-card><n-statistic label="Active workspaces" :value="tenantMetrics.activeWorkspaces" /></n-card></n-gi>
    </n-grid>
    <n-card title="Manage">
      <div style="display:flex; gap:8px">
        <n-button type="primary" @click="router.push('/users')">Go to Users</n-button>
        <n-button type="primary" @click="router.push('/teams')">Go to Teams</n-button>
        <n-button type="primary" @click="router.push('/size-presets')">Go to Size Presets</n-button>
        <n-button type="primary" @click="router.push('/token-quotas')">Go to Token Quotas</n-button>
      </div>
    </n-card>
  </template>

  <!-- Member -->
  <template v-else-if="role === 'member'">
    <n-card title="My Workspaces">
      <p>Create and manage your AI dev workspaces (Jupyter / VSCode).</p>
      <n-button type="primary" @click="router.push('/workspaces')">Go to Workspaces</n-button>
    </n-card>
  </template>
</template>
