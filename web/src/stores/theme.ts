import { defineStore } from 'pinia'

type ThemeMode = 'light' | 'dark'

// No stored preference yet -> follow the OS's prefers-color-scheme instead
// of hardcoding light, so a dark-mode user isn't flashed a bright UI first.
function initialMode(): ThemeMode {
  const stored = localStorage.getItem('theme')
  if (stored === 'light' || stored === 'dark') return stored
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export const useThemeStore = defineStore('theme', {
  state: () => ({
    mode: initialMode(),
  }),
  actions: {
    toggle() {
      this.mode = this.mode === 'dark' ? 'light' : 'dark'
      localStorage.setItem('theme', this.mode)
    },
  },
})
