<script setup>
import { Copy, Download, FolderInput, KeyRound, LockOpen, Power, Trash2, UserPlus } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { auth, get, post, del, enc } from '../api'
import { toast } from '../toasts'
import { askConfirm, confirmTyped } from '../dialogs'
import { downloadCsv, formatDate, generatePassword, inactive } from '../util'
import { t } from '../i18n'
import PasswordDialog from '../components/PasswordDialog.vue'
import MoveUserDialog from '../components/dialogs/MoveUserDialog.vue'
import UserFormDialog from '../components/dialogs/UserFormDialog.vue'
import LoadingRow from '../components/LoadingRow.vue'

const route = useRoute()
const router = useRouter()
const list = ref([])
const ous = ref([])
const groups = ref([])
const loading = ref(false)
const search = ref(route.query.q || '')
const filter = ref(route.query.f || 'all')
const inactiveDays = computed(() => auth.settings?.inactive_days || 90)

async function load() {
  loading.value = true
  try {
    list.value = await get('/users')
  } catch (e) {
    toast.error(e.message)
  }
  loading.value = false
}
load()
get('/ous')
  .then((r) => (ous.value = r))
  .catch(() => {})
get('/groups')
  .then((r) => (groups.value = r.map((g) => g.name).sort()))
  .catch(() => {})

watch(
  () => route.query,
  (q) => {
    search.value = q.q || ''
    filter.value = q.f || 'all'
  },
)
watch([search, filter], ([q, f]) => router.replace({ query: { ...(q && { q }), ...(f !== 'all' && { f }) } }))

const filters = {
  all: () => true,
  enabled: (u) => u.enabled,
  disabled: (u) => !u.enabled,
  locked: (u) => u.locked,
  inactive: (u) => u.enabled && inactive(u.last_logon),
}
const visible = computed(() => {
  const q = search.value.trim().toLowerCase()
  return list.value
    .filter(filters[filter.value] || filters.all)
    .filter((u) => !q || [u.sam, u.name, u.mail, u.department].some((v) => v?.toLowerCase().includes(q)))
    .sort((a, b) => a.sam.localeCompare(b.sam))
})
const ouName = (dn) =>
  dn
    .split(/,(?=OU=|CN=|DC=)/)
    .slice(1)
    .find((p) => !p.startsWith('DC='))
    ?.replace(/^(OU|CN)=/, '') || '—'

function exportCsv() {
  const yesNo = (v) => (v ? t('yes') : t('no'))
  downloadCsv(t('users.csv'), visible.value, [
    [t('Login'), 'sam'],
    [t('Name'), 'name'],
    [t('E-mail'), 'mail'],
    [t('Department'), 'department'],
    [t('Job title'), 'title'],
    [t('Active'), (u) => yesNo(u.enabled)],
    [t('Locked out'), (u) => yesNo(u.locked)],
    [t('Last logon'), (u) => formatDate(u.last_logon)],
    [t('OU'), (u) => ouName(u.dn)],
    [t('Groups'), (u) => (u.groups || []).join(', ')],
  ])
}

// ── row actions
async function action(u, verb, msg) {
  try {
    await post(`/users/${enc(u.sam)}/${verb}`)
    toast.success(msg)
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
async function toggle(u) {
  if (
    u.enabled &&
    !(await askConfirm(t('Disable {name} ({user})? The user will no longer be able to log on.', { name: u.name, user: u.sam }), {
      ok: t('Disable'),
    }))
  )
    return
  action(u, u.enabled ? 'disable' : 'enable', t(u.enabled ? '{name} disabled.' : '{name} enabled.', { name: u.sam }))
}
async function remove(u) {
  if (!(await confirmTyped(t('Delete the account {name}? Prefer disabling it: deletion cannot be undone.', { name: u.name }), u.sam)))
    return
  try {
    await del(`/users/${enc(u.sam)}`)
    toast.removed(t('{name} deleted.', { name: u.sam }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}

// ── create / copy / edit
const form = ref(null)

// defaultOu is the configured OU for new users, else the Users container.
const defaultOu = () => auth.settings?.default_ou || ous.value.find((o) => o.name === 'Users')?.dn || ''

function create(template) {
  form.value = {
    mode: 'new',
    heading: template ? t('New user from {user}', { user: template.sam }) : t('New user'),
    sam: '',
    name: '',
    given_name: '',
    surname: '',
    mail: '',
    phone: '',
    title: template?.title || '',
    department: template?.department || '',
    description: template?.description || '',
    ou: template ? parentDn(template.dn) : defaultOu(),
    password: generatePassword(),
    must_change: true,
    groups: (template?.groups || []).filter((g) => g !== 'Domain Users'),
  }
}
const parentDn = (dn) => dn.replace(/^CN=(\\,|[^,])*,/, '')

async function edit(u) {
  try {
    const d = await get(`/users/${enc(u.sam)}`)
    form.value = { mode: 'edit', heading: `${d.name} (${d.sam})`, tab: 'account', ...d, groups: [...(d.groups || [])], newGroup: '' }
  } catch (e) {
    toast.error(e.message)
  }
}

function formDone() {
  form.value = null
  load()
}

// ── password / move
const passwordFor = ref(null)
const moving = ref(null)
function moveDone() {
  moving.value = null
  load()
}

function passwordDone() {
  passwordFor.value = null
  load()
}
// built-in accounts and the operator's own login cannot be disabled, moved or deleted
const editable = (u) => !u.protected && u.sam !== auth.user
</script>

<template>
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <input v-model="search" class="inp w-64" :placeholder="t('Filter by login, name, e-mail…')" :aria-label="t('Filter')" />
    <select v-model="filter" class="inp w-44" :aria-label="t('Status')">
      <option value="all">{{ t('All') }}</option>
      <option value="enabled">{{ t('Enabled') }}</option>
      <option value="disabled">{{ t('Disabled') }}</option>
      <option value="locked">{{ t('Locked') }}</option>
      <option value="inactive">{{ t('Inactive for {n}+ days', { n: inactiveDays }) }}</option>
    </select>
    <span class="text-[12px] text-fg-muted">{{ t('{n} of {total}', { n: visible.length, total: list.length }) }}</span>
    <div class="ml-auto flex gap-2">
      <button class="btn-ghost" @click="exportCsv"><Download :size="13" /> {{ t('Export CSV') }}</button>
      <button class="btn" @click="create()"><UserPlus :size="13" /> {{ t('New user') }}</button>
    </div>
  </div>

  <div class="max-h-[calc(var(--view-h)-260px)] overflow-auto">
    <table class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Login') }}</th>
          <th>{{ t('Name') }}</th>
          <th>{{ t('E-mail') }}</th>
          <th>{{ t('OU') }}</th>
          <th class="w-px">{{ t('Status') }}</th>
          <th class="w-px">
            <span class="sr-only">{{ t('system') }}</span>
          </th>
          <th>{{ t('Last logon') }}</th>
          <th colspan="4" class="text-center">{{ t('Actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <LoadingRow v-if="loading && !list.length" :cols="11" />
        <tr v-for="u in visible" :key="u.sam" :class="u.enabled ? '' : 'text-fg-subtle'">
          <td>
            <button class="act px-0 font-semibold" @click="edit(u)">{{ u.sam }}</button>
          </td>
          <td>{{ u.name }}</td>
          <td>{{ u.mail }}</td>
          <td>{{ ouName(u.dn) }}</td>
          <td class="whitespace-nowrap">
            <!-- clicking the status enables/disables the account (not built-in accounts nor the operator's own) -->
            <component
              :is="editable(u) ? 'button' : 'span'"
              class="tag status"
              :class="[
                !u.enabled
                  ? 'bg-neutral-fill text-on-neutral-fill'
                  : u.locked
                    ? 'bg-err-fill text-on-err-fill'
                    : 'bg-ok-soft text-on-ok-soft',
                editable(u) && 'toggleable',
              ]"
              :title="editable(u) ? (u.enabled ? t('Click to disable') : t('Click to enable')) : ''"
              @click="editable(u) && toggle(u)"
            >
              {{ !u.enabled ? t('deactivated') : u.locked ? t('locked') : t('active') }}
              <!-- the icon slot is always there, so every tag has the same width and centered text -->
              <Power :size="10" class="shrink-0" :class="editable(u) ? '' : 'invisible'" />
            </component>
          </td>
          <!-- own column: tags line up instead of following the status width -->
          <td class="whitespace-nowrap">
            <span v-if="u.protected" class="tag bg-strong-fill text-on-strong-fill" :title="t('Built-in domain account')">{{
              t('system')
            }}</span>
            <button v-if="u.locked" class="act" @click="action(u, 'unlock', t('{name} unlocked.', { name: u.sam }))">
              <LockOpen :size="12" /> {{ t('unlock') }}
            </button>
          </td>
          <td class="whitespace-nowrap" :class="u.enabled && inactive(u.last_logon) ? 'text-warn' : ''">
            {{ formatDate(u.last_logon) }}
          </td>
          <!-- one column per action: buttons stay aligned even when a row lacks some actions -->
          <td class="action">
            <button class="act" @click="passwordFor = u"><KeyRound :size="12" /> {{ t('password') }}</button>
          </td>
          <td class="action">
            <button class="act" @click="create(u)"><Copy :size="12" /> {{ t('copy') }}</button>
          </td>
          <td class="action">
            <button v-if="editable(u)" class="act" @click="moving = { sam: u.sam, ou: parentDn(u.dn) }">
              <FolderInput :size="12" /> {{ t('move') }}
            </button>
          </td>
          <td class="action">
            <button v-if="editable(u)" class="act hover:!text-err" @click="remove(u)"><Trash2 :size="12" /> {{ t('delete') }}</button>
          </td>
        </tr>
        <tr v-if="!loading && !visible.length">
          <td colspan="11" class="py-4 text-center text-fg-muted">{{ t('No users.') }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <UserFormDialog v-if="form" :initial="form" :ous="ous" :groups="groups" @close="form = null" @done="formDone" />

  <PasswordDialog v-if="passwordFor" :user="passwordFor" @close="passwordFor = null" @done="passwordDone" />

  <MoveUserDialog v-if="moving" :user="moving" :ous="ous" @close="moving = null" @done="moveDone" />
</template>

<style scoped>
td.action {
  width: 1%;
  padding-inline: 2px;
  white-space: nowrap;
}
td.action:last-child {
  padding-right: 8px;
}
.status {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  width: 104px;
}
.toggleable {
  cursor: pointer;
}
.toggleable:hover {
  box-shadow: 0 0 0 2px var(--color-accent);
}
</style>
