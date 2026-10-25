import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/auth/LoginView.vue'),
    },
    {
      path: '/',
      component: () => import('../layouts/DefaultLayout.vue'),
      children: [
        {
          path: '',
          redirect: '/dashboard',
        },
        {
          path: 'dashboard',
          name: 'dashboard',
          component: () => import('../views/dashboard/DashboardView.vue'),
        },
        {
          path: 'tenants',
          name: 'tenants',
          component: () => import('../views/tenants/TenantsView.vue'),
        },
        {
          path: 'users',
          name: 'users',
          component: () => import('../views/users/UsersView.vue'),
        },
        {
          path: 'teams',
          name: 'teams',
          component: () => import('../views/teams/TeamsView.vue'),
        },
        {
          path: 'workspaces',
          name: 'workspaces',
          component: () => import('../views/workspaces/WorkspacesView.vue'),
        },
        {
          path: 'size-presets',
          name: 'size-presets',
          component: () => import('../views/presets/SizePresetsView.vue'),
        },
        {
          path: 'token-quotas',
          name: 'token-quotas',
          component: () => import('../views/token-quotas/TokenQuotasView.vue'),
        },
        {
          path: 'test-runs',
          name: 'test-runs',
          component: () => import('../views/test-runs/TestRunsView.vue'),
        },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.name !== 'login' && !auth.isAuthenticated) {
    return { name: 'login' }
  }
  if (to.name === 'login' && auth.isAuthenticated) {
    return { name: 'dashboard' }
  }
  return true
})

export default router
