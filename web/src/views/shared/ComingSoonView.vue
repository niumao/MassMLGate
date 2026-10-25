<script setup lang="ts">
import { NCard, NEmpty } from 'naive-ui'

defineProps<{ title: string }>()
</script>

<template>
  <n-card :title="title">
    <n-empty description="Not built yet — coming in a later pass." />
  </n-card>
</template>
