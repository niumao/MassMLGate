<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme } from 'naive-ui'
import { useThemeStore } from './stores/theme'

const theme = useThemeStore()
</script>

<template>
  <n-config-provider :theme="theme.mode === 'dark' ? darkTheme : null">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
