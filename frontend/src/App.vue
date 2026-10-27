<script setup>
import { ref, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { auth, brand, loadBrand, loadMe, logoUrl, onSessionChange, platform, post } from './api'
import { cancelDialogs } from './dialogs'
import { t } from './i18n'
import Login from './pages/Login.vue'
import Setup from './pages/Setup.vue'
import PanelDialogs from './components/PanelDialogs.vue'
import Toasts from './components/Toasts.vue'
import ChangeOwnPasswordDialog from './components/dialogs/ChangeOwnPasswordDialog.vue'
import defaultLogo from './assets/ad-manager-logo.png'
import lightLogo from './assets/ad-manager-logo-light.png'
import {
  Archive,
  ArrowLeftRight,
  BookOpen,
  Clock,
  FolderOpen,
  FolderTree,
  Home,
  KeyRound,
  LifeBuoy,
  ListChecks,
  LogOut,
  Monitor,
  ScrollText,
  Search,
  Server,
  Settings,
  Settings2,
  UserRound,
  Users,
  UsersRound,
} from 'lucide-vue-next'

// public page of a password link (no session needed)
const linkToken = location.pathname.match(/^\/password\/([0-9a-f]{64})$/)?.[1] || ''

const DEFAULT_SUPPORT = 'https://github.com/jniltinho/ad-manager/wiki'
const router = useRouter()
const route = useRoute()
// the Backup tab also holds the Sync page (BackupTabs)
const isActive = (to) =>
  to === '/' ? route.path === '/' : route.path.startsWith(to) || (to === '/backup' && route.path.startsWith('/sync'))
loadBrand()
loadMe()

// visual theme chosen in Settings (global). The server already sends the page with the saved theme; once the
// brand (with the theme registry) is loaded the app follows Settings. Until then — or if /api/health fails — the
// theme of the HTML stays. A value outside the registry falls back to the first (Default).
watchEffect(() => {
  const themes = brand.themes?.length ? brand.themes : platform.themes
  if (!themes.length) return
  // the desktop's own appearance (Configure) goes over the panel's
  const id = platform.appearance.theme || brand.theme
  const flat = platform.appearance.flat ? platform.appearance.flat === 'flat' : !!brand.flat
  const theme = themes.find((x) => x.id === id) || themes[0]
  document.documentElement.dataset.theme = theme.id
  document.documentElement.dataset.flat = String(flat)
  document.documentElement.style.colorScheme = theme.scheme // the served <html> carries it inline
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', theme.scheme)
})

// tabs whose name stays in English in every language (their translation as the tooltip): short and steady tab widths
const ENGLISH_TABS = new Set(['Users', 'Computers', 'Shares'])
const tabs = [
  ['/', 'Home', Home],
  ['/users', 'Users', Users],
  ['/groups', 'Groups', UsersRound],
  ['/ous', 'OUs', FolderTree],
  ['/computers', 'Computers', Monitor],
  ['/shares', 'Shares', FolderOpen],
  ['/import-export', 'Import/export', ArrowLeftRight],
  ['/backup', 'Backup', Archive],
  ['/logs', 'Logs', ScrollText],
  ['/settings', 'Settings', Settings],
]

// each page starts at the top of the content area
const mainEl = ref(null)
const scrollTop = () => mainEl.value?.scrollTo({ top: 0 })

// global search: opens Users filtered by the term
const term = ref('')
function search() {
  if (term.value.trim()) router.push({ path: '/users', query: { q: term.value.trim() } })
}

async function logout() {
  await post('/logout').catch(() => {})
  auth.user = null
}

// the operator's own password dialog
const ownPassword = ref(false)
// a new session (sign-in or sign-out) closes what the old one left open: nothing confirmed for it runs
onSessionChange(() => {
  cancelDialogs()
  ownPassword.value = false
})
</script>

<template>
  <PanelDialogs />
  <Toasts />
  <component :is="platform.screen" v-if="platform.screen" />
  <Setup v-else-if="brand.setup" />
  <Login v-else-if="linkToken" :token="linkToken" />
  <div v-else-if="!auth.checked" class="p-10 text-center text-fg-muted">{{ t('Loading…') }}</div>
  <Login v-else-if="!auth.user" />
  <div
    v-else
    class="mx-auto my-4 flex h-[calc(var(--view-h)-32px)] w-[calc(100%-32px)] max-w-(--app-max-w) flex-col overflow-hidden bg-surface text-fg shadow-(--shadow-app) outline outline-line"
  >
    <header class="flex flex-wrap items-center gap-4 bg-header bg-(image:--bg-header) px-5 py-3">
      <img v-if="brand.logo" :src="logoUrl()" :alt="brand.company || t('Company logo')" class="logo-plate h-10 max-w-64 object-contain" />
      <template v-else>
        <img :src="defaultLogo" alt="AD Manager" class="h-10 max-w-64 object-contain" style="display: var(--header-logo-dark-ink)" />
        <img :src="lightLogo" alt="AD Manager" class="h-10 max-w-64 object-contain" style="display: var(--header-logo-light-ink)" />
      </template>
      <div class="border-l-2 border-header-bar pl-3 leading-tight">
        <div class="text-[15px] font-semibold text-on-header">Active Directory</div>
        <div class="text-[11px] text-on-header-muted">{{ auth.domain }}</div>
      </div>
      <form class="ml-auto flex w-72" @submit.prevent="search">
        <input v-model="term" class="inp" :placeholder="t('Search user, login or e-mail…')" :aria-label="t('Global search')" />
        <button class="btn"><Search :size="14" /> {{ t('Search') }}</button>
      </form>
      <div class="text-right text-[12px] leading-tight">
        <div class="font-semibold text-on-header">{{ auth.user }}</div>
        <button class="act px-0 text-[11px] text-header-link hover:text-header-link-hover" @click="ownPassword = true">
          <KeyRound :size="11" /> {{ t('my password') }}
        </button>
        <span class="text-on-header-subtle"> · </span>
        <button class="act px-0 text-[11px] text-header-link hover:text-header-link-hover" @click="logout">
          <LogOut :size="11" /> {{ t('sign out') }}
        </button>
      </div>
    </header>
    <nav class="top-tabs flex flex-wrap bg-tabs bg-(image:--bg-tabs) shadow-(--shadow-tabs)">
      <router-link
        v-for="[to, name, icon] in tabs"
        :key="to"
        :to="to"
        class="tab flex items-center gap-1.5 px-4 py-2 text-[12px] font-semibold transition-colors duration-150"
        :class="isActive(to) ? 'on bg-tab-on bg-(image:--bg-tab-on) text-on-tab-on shadow-(--shadow-tab-on)' : 'bg-tab text-on-tab'"
        :title="ENGLISH_TABS.has(name) && t(name) !== name ? t(name) : undefined"
      >
        <component :is="icon" :size="15" :class="isActive(to) ? 'text-tab-on-icon' : ''" />
        {{ ENGLISH_TABS.has(name) ? name : t(name) }}
      </router-link>
      <div class="ml-auto flex">
        <a
          v-if="auth.settings?.swagger"
          href="/swagger"
          target="_blank"
          rel="noopener"
          class="tab-aux flex items-center gap-1.5 px-4 py-2 text-[12px] font-semibold text-on-tab-aux hover:text-on-tab-aux-hover"
          :title="t('API documentation (opens in a new tab)')"
        >
          <BookOpen :size="15" /> API
        </a>
        <a
          v-if="auth.asynqmon && !platform.configure"
          href="/asynqmon"
          target="_blank"
          rel="noopener"
          class="tab-aux flex items-center gap-1.5 px-4 py-2 text-[12px] font-semibold text-on-tab-aux hover:text-on-tab-aux-hover"
          :title="t('Agent queue monitor (opens in a new tab)')"
        >
          <ListChecks :size="15" /> {{ t('Queues') }}
        </a>
        <!-- the queue monitor needs a browser session to the panel: off in the desktop -->
        <span
          v-else-if="auth.asynqmon"
          class="tab-aux flex cursor-not-allowed items-center gap-1.5 px-4 py-2 text-[12px] font-semibold text-on-tab-aux opacity-50"
          aria-disabled="true"
          :title="t('The queue monitor is not available in the desktop app')"
        >
          <ListChecks :size="15" /> {{ t('Queues') }}
        </span>
        <button
          v-if="platform.configure"
          class="tab-aux flex cursor-pointer items-center gap-1.5 px-4 py-2 text-[12px] font-semibold text-on-tab-aux hover:text-on-tab-aux-hover"
          @click="platform.configure()"
        >
          <Settings2 :size="15" /> {{ t('Configure') }}
        </button>
      </div>
    </nav>
    <main ref="mainEl" class="min-h-0 flex-1 overflow-auto p-5">
      <!-- same page transition as the earlier panel: fade out, then fade in rising 6 px -->
      <router-view v-slot="{ Component }">
        <transition name="page" mode="out-in" @before-enter="scrollTop">
          <!-- keyed wrapper: several views have more than one root element -->
          <!-- h-full: pages with their own scrolling area (e.g. Shares) fill <main> -->
          <div :key="route.path" class="page-transition h-full">
            <component :is="Component" />
          </div>
        </transition>
      </router-view>
    </main>
    <footer
      class="app-footer flex flex-wrap items-center gap-y-1 border-t border-footer-bar bg-footer bg-(image:--bg-footer) px-5 py-2 text-[11px] text-on-footer"
    >
      <span class="font-semibold text-footer-strong">© {{ brand.company || 'AD Manager' }}</span>
      <a
        :href="brand.support_url || DEFAULT_SUPPORT"
        target="_blank"
        rel="noopener"
        class="inline-flex items-center gap-1 hover:text-footer-strong"
        ><LifeBuoy :size="12" /> {{ t('Support') }}</a
      >
      <span class="inline-flex items-center gap-1"
        ><Server :size="12" /> {{ auth.domain }}<template v-if="auth.netbios"> ({{ auth.netbios }})</template></span
      >
      <span class="ml-auto inline-flex items-center gap-1"><UserRound :size="12" /> {{ auth.user }}</span>
      <span class="inline-flex items-center gap-1"
        ><Clock :size="12" /> {{ t('session expires after {n} min idle', { n: auth.settings?.session_minutes }) }}</span
      >
      <span class="text-footer-strong">ad-manager {{ auth.version }}</span>
    </footer>
  </div>

  <ChangeOwnPasswordDialog v-if="ownPassword" @close="ownPassword = false" />
</template>

<style>
/* top tabs recipe: every tab gets the theme's divider; inactive ones its hover (the current one is styled by
   its classes) */
.top-tabs {
  gap: var(--tab-gap);
  padding: var(--tabs-pad) var(--tabs-pad) 0;
}
.top-tabs > .tab {
  border-left: var(--tab-divider-w) solid var(--color-tab-divider);
}
.top-tabs > .tab:not(.on) {
  margin-top: var(--tab-inset-top);
  box-shadow: var(--shadow-tab);
}
.top-tabs > .tab:not(.on):hover {
  color: var(--color-on-tab-hover);
  background: var(--color-tab-hover);
  filter: var(--tab-hover-filter);
}
/* footer recipe: the bar on top and, in themes that set them, status-bar panes (the 98 theme's sunken boxes) */
.app-footer {
  border-top-width: var(--footer-bar-w);
  column-gap: var(--footer-gap);
  box-shadow: var(--footer-line);
}
.app-footer > * {
  box-shadow: var(--footer-pane-relief);
  padding: var(--footer-pane-pad);
}
.page-enter-active,
.page-leave-active {
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
}
.page-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.page-leave-to {
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .page-enter-active,
  .page-leave-active {
    transition: none;
  }
}
</style>
