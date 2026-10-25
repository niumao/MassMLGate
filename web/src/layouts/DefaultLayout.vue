<script setup lang="ts">
import { NLayout, NLayoutSider, NLayoutHeader, NLayoutContent, NMenu, NDropdown, NAvatar, NSpace, NText } from 'naive-ui'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()

const menuOptions = computed(() =>
  auth.menu.map((item) => ({
    label: item.label,
    key: item.path,
  })),
)

function handleMenuSelect(key: string) {
  router.push(key)
}

const userDropdownOptions = [{ label: 'Logout', key: 'logout' }]

function handleUserAction(key: string) {
  if (key === 'logout') {
    auth.logout()
    router.push('/login')
  }
}
</script>

<template>
  <n-layout style="height: 100vh" has-sider>
    <n-layout-sider bordered :width="220">
      <div class="brand">Tenant Service</div>
      <n-menu :options="menuOptions" @update:value="handleMenuSelect" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="header">
        <n-space justify="end" align="center" style="width: 100%">
          <n-text depth="3">{{ auth.user?.role }}</n-text>
          <n-dropdown :options="userDropdownOptions" @select="handleUserAction">
            <n-space align="center" style="cursor: pointer">
              <n-avatar round size="small">{{ auth.user?.displayName.charAt(0) }}</n-avatar>
              <n-text>{{ auth.user?.displayName }}</n-text>
            </n-space>
          </n-dropdown>
        </n-space>
      </n-layout-header>
      <n-layout-content class="content">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.brand {
  padding: 16px;
  font-weight: 600;
}
.header {
  height: 56px;
  display: flex;
  align-items: center;
  padding: 0 24px;
}
.content {
  padding: 24px;
}
</style>
