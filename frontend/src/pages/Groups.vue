<script setup>
import { Download, Plus, Trash2, UsersRound } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { get, del, enc } from '../api'
import { toast } from '../toasts'
import { confirmTyped } from '../dialogs'
import { downloadCsv } from '../util'
import { t } from '../i18n'
import NewGroupDialog from '../components/dialogs/NewGroupDialog.vue'
import GroupDetailDialog from '../components/dialogs/GroupDetailDialog.vue'
import LoadingRow from '../components/LoadingRow.vue'

const list = ref([])
const loaded = ref(false) // the first reply arrived: until then the table shows it is loading
const ous = ref([])
const logins = ref([])
const search = ref('')
const kind = ref('')

async function load() {
  try {
    list.value = await get('/groups')
  } catch (e) {
    toast.error(e.message)
  }
  loaded.value = true
}
load()
get('/ous')
  .then((r) => (ous.value = r))
  .catch(() => {})
get('/users')
  .then((r) => (logins.value = r.map((u) => ({ sam: u.sam, name: u.name }))))
  .catch(() => {})

const scopes = { global: 'Global', domainlocal: 'Domain local', universal: 'Universal', builtin: 'Built-in' }
const typeName = (type) => t(type === 'security' ? 'Security' : 'Distribution')
const visible = computed(() => {
  const q = search.value.trim().toLowerCase()
  return list.value
    .filter((g) => !kind.value || g.type === kind.value)
    .filter((g) => !q || g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name))
})

function exportCsv() {
  downloadCsv(t('groups.csv'), visible.value, [
    [t('Group'), 'name'],
    [t('Group type'), 'type'],
    [t('Scope'), 'scope'],
    [t('Members'), 'member_count'],
    [t('Description'), 'description'],
  ])
}

// ── new group (the fields live in NewGroupDialog)
const draft = ref(null)
function created() {
  draft.value = null
  load()
}

// ── detail: description and members (edited in GroupDetailDialog)
const detail = ref(null)
async function open(g) {
  try {
    detail.value = await get(`/groups/${enc(g.name)}`)
  } catch (e) {
    toast.error(e.message)
  }
}
function saved() {
  detail.value = null
  load()
}
async function remove(g) {
  if (!(await confirmTyped(t('Delete the group {name}? Its members lose the permissions it grants.', { name: g.name }), g.name))) return
  try {
    await del(`/groups/${enc(g.name)}`)
    toast.removed(t('Group {name} deleted.', { name: g.name }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <input v-model="search" class="inp w-64" :placeholder="t('Filter by name or description…')" :aria-label="t('Filter')" />
    <select v-model="kind" class="inp w-40" :aria-label="t('Group type')">
      <option value="">{{ t('All types') }}</option>
      <option value="security">{{ t('Security') }}</option>
      <option value="distribution">{{ t('Distribution') }}</option>
    </select>
    <span class="text-[12px] text-fg-muted">{{ t('{n} of {total}', { n: visible.length, total: list.length }) }}</span>
    <div class="ml-auto flex gap-2">
      <button class="btn-ghost" @click="exportCsv"><Download :size="13" /> {{ t('Export CSV') }}</button>
      <button
        class="btn"
        @click="draft = { name: '', description: '', type: 'security', scope: 'global', ou: ous.find((o) => o.name === 'Users')?.dn || '' }"
      >
        <Plus :size="13" /> {{ t('New group') }}
      </button>
    </div>
  </div>

  <div class="max-h-[calc(var(--view-h)-260px)] overflow-auto">
    <table class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Group') }}</th>
          <th class="w-px">
            <span class="sr-only">{{ t('system') }}</span>
          </th>
          <th class="w-px">{{ t('Group type') }}</th>
          <th class="w-px">{{ t('Scope') }}</th>
          <th class="w-px text-right">{{ t('Members') }}</th>
          <th>{{ t('Description') }}</th>
          <th colspan="2" class="w-px text-center">{{ t('Actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <LoadingRow v-if="!loaded" :cols="8" />
        <tr v-for="g in visible" :key="g.name">
          <td class="whitespace-nowrap">
            <button class="act px-0 font-semibold" @click="open(g)">{{ g.name }}</button>
          </td>
          <!-- own column: the tags line up instead of following the name length -->
          <td class="w-px">
            <span v-if="g.protected" class="tag bg-strong-fill text-on-strong-fill" :title="t('Built-in domain group')">{{
              t('system')
            }}</span>
          </td>
          <td class="whitespace-nowrap">{{ typeName(g.type) }}</td>
          <td class="whitespace-nowrap">{{ scopes[g.scope] && t(scopes[g.scope]) }}</td>
          <td class="text-right tabular-nums">{{ g.member_count }}</td>
          <td class="max-w-0 w-full truncate text-fg-muted" :title="g.description">{{ g.description }}</td>
          <!-- one column per action, as in Users -->
          <td class="action">
            <button class="act" @click="open(g)"><UsersRound :size="12" /> {{ t('members') }}</button>
          </td>
          <td class="action">
            <button v-if="!g.protected" class="act hover:!text-err" @click="remove(g)"><Trash2 :size="12" /> {{ t('delete') }}</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <NewGroupDialog v-if="draft" :draft="draft" :ous="ous" @close="draft = null" @done="created" />

  <GroupDetailDialog v-if="detail" :group="detail" :logins="logins" @close="detail = null" @saved="saved" @changed="load" />
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
</style>
