<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NCard, NForm, NFormItem, NInput, NButton, NAlert, useMessage } from 'naive-ui'
import { useAuthStore } from '../../stores/auth'

const DEMO_CREDENTIALS = [
  { email: 'admin@platform.local', password: 'admin123', role: 'System Admin' },
  { email: 'ta@acme.local', password: 'tenant123', role: 'Tenant Admin' },
  { email: 'jane@acme.local', password: 'member123', role: 'Member' },
]

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

const auth = useAuthStore()
const router = useRouter()
const message = useMessage()

async function submit() {
  loading.value = true
  error.value = ''
  const result = await auth.login(email.value, password.value)
  loading.value = false
  if (!result.ok) {
    error.value = result.error
    return
  }
  message.success(`Welcome, ${auth.user?.displayName}`)
  router.push('/dashboard')
}
</script>

<template>
  <div class="login-page">
    <n-card title="Tenant Service" class="login-card">
      <n-form @submit.prevent="submit">
        <n-form-item label="Email">
          <n-input v-model:value="email" placeholder="you@example.com" />
        </n-form-item>
        <n-form-item label="Password">
          <n-input v-model:value="password" type="password" placeholder="••••••••" @keyup.enter="submit" />
        </n-form-item>
        <n-alert v-if="error" type="error" :show-icon="false" style="margin-bottom: 16px">
          {{ error }}
        </n-alert>
        <n-button type="primary" block :loading="loading" @click="submit">Log in</n-button>
      </n-form>
      <template #footer>
        <div class="hint">
          Demo accounts:
          <ul>
            <li v-for="acc in DEMO_CREDENTIALS" :key="acc.email">
              {{ acc.role }} — {{ acc.email }} / {{ acc.password }}
            </li>
          </ul>
        </div>
      </template>
    </n-card>
  </div>
</template>

<style scoped>
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--n-color, #f5f5f5);
}
.login-card {
  width: 360px;
}
.hint {
  font-size: 12px;
  opacity: 0.7;
}
</style>
