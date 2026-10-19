import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import './style.css'
import App from './App.vue'
import { installNativeReplacements } from './native'
import { t } from './i18n'

const pages = [
  ['/', 'Home', () => import('./pages/Home.vue')],
  ['/users', 'Users', () => import('./pages/Users.vue')],
  ['/groups', 'Groups', () => import('./pages/Groups.vue')],
  ['/ous', 'OUs', () => import('./pages/OrgUnits.vue')],
  ['/computers', 'Computers', () => import('./pages/Computers.vue')],
  ['/shares', 'Shares', () => import('./pages/Shares.vue')],
  ['/password-policy', 'Password policy', () => import('./pages/PasswordPolicy.vue')],
  ['/import-export', 'Import/export', () => import('./pages/ImportExport.vue')],
  ['/backup', 'Backup', () => import('./pages/Backup.vue')],
  ['/sync', 'Sync', () => import('./pages/Sync.vue')],
  ['/logs', 'Logs', () => import('./pages/Logs.vue')],
  ['/settings', 'Settings', () => import('./pages/Settings.vue')],
]
// Portuguese paths from 0.3.0 and earlier keep working (bookmarks).
const legacy = {
  '/usuarios': '/users',
  '/grupos': '/groups',
  '/computadores': '/computers',
  '/compartilhamentos': '/shares',
  '/politica': '/password-policy',
  '/importar': '/import-export',
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...pages.map(([path, title, component]) => ({ path, component, meta: { title } })),
    ...Object.entries(legacy).map(([path, redirect]) => ({ path, redirect })),
    { path: '/password/:token', component: { render: () => null }, meta: { title: 'Change password' } }, // App shows Login

    { path: '/:p(.*)*', redirect: '/' },
  ],
})
router.afterEach((to) => (document.title = `${t(to.meta.title || '')} · AD Manager`))

installNativeReplacements()
createApp(App).use(router).mount('#app')
