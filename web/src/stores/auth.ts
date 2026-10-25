import { defineStore } from 'pinia'
import client from '../api/client'
import type { MenuItem, User } from '../types'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as User | null,
    menu: [] as MenuItem[],
    token: localStorage.getItem('token'),
    ready: false, // true once initial session restore (or lack thereof) is resolved
  }),
  getters: {
    isAuthenticated: (state) => state.token !== null && state.user !== null,
  },
  actions: {
    async login(email: string, password: string): Promise<{ ok: true } | { ok: false; error: string }> {
      try {
        const { data } = await client.post('/auth/login', { email, password })
        this.token = data.token
        this.user = data.user
        localStorage.setItem('token', data.token)
        await this.fetchMe()
        return { ok: true }
      } catch (e: any) {
        return { ok: false, error: e.response?.data?.error ?? 'Login failed' }
      }
    },
    async fetchMe() {
      const { data } = await client.get('/auth/me')
      this.user = data.user
      this.menu = data.menu ?? []
    },
    // Called once at app startup to restore a session from a stored token.
    async init() {
      if (this.token) {
        try {
          await this.fetchMe()
        } catch {
          this.logout()
        }
      }
      this.ready = true
    },
    logout() {
      this.user = null
      this.menu = []
      this.token = null
      localStorage.removeItem('token')
    },
  },
})
