<script setup>
import { Download, Globe, Power, Trash2, Users } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { auth, get, post, del, enc } from '../api'
import { toast } from '../toasts'
import { askConfirm, confirmTyped } from '../dialogs'
import { downloadCsv, formatDate, inactive } from '../util'
import { t } from '../i18n'
import SmbSessionsDialog from '../components/dialogs/SmbSessionsDialog.vue'
import DnsRecordsDialog from '../components/dialogs/DnsRecordsDialog.vue'
import LoadingRow from '../components/LoadingRow.vue'

const list = ref([])
const loaded = ref(false) // the first reply arrived: until then the table shows it is loading
const search = ref('')
const os = ref('')
const onlyInactive = ref(false)
async function load() {
  try {
    list.value = await get('/computers')
  } catch (e) {
    toast.error(e.message)
  }
  loaded.value = true
}
load()

const systems = computed(() => [...new Set(list.value.map((c) => c.os || ''))].sort())
const visible = computed(() => {
  const q = search.value.trim().toLowerCase()
  return list.value
    .filter((c) => !q || c.name.toLowerCase().includes(q) || c.dns_name?.toLowerCase().includes(q))
    .filter((c) => !os.value || c.os === os.value)
    .filter((c) => !onlyInactive.value || inactive(c.last_logon))
    .sort((a, b) => a.name.localeCompare(b.name))
})
const legacy = (os) => /XP|2003|Windows Server® 2008 Standard|Windows 7/.test(os || '')

function exportCsv() {
  const yesNo = (v) => (v ? t('yes') : t('no'))
  downloadCsv(t('computers.csv'), visible.value, [
    [t('Name'), 'name'],
    ['DNS', 'dns_name'],
    [t('System'), 'os'],
    [t('Version'), 'os_version'],
    [t('Active'), (c) => yesNo(c.enabled)],
    ['DC', (c) => yesNo(c.is_dc)],
    [t('Last logon'), (c) => formatDate(c.last_logon)],
  ])
}

async function toggle(c) {
  if (
    c.enabled &&
    !(await askConfirm(t('Disable the computer {name}? Users will not be able to log on to it with a domain account.', { name: c.name }), {
      ok: t('Disable'),
    }))
  )
    return
  try {
    await post(`/computers/${enc(c.sam)}/${c.enabled ? 'disable' : 'enable'}`)
    toast.success(t(c.enabled ? '{name} disabled.' : '{name} enabled.', { name: c.name }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
// ── SMB sessions of a server (SmbSessionsDialog) and the DNS of the domain (DnsRecordsDialog)
const sess = ref(null) // the host whose sessions are shown
const dns = ref(false)

async function remove(c) {
  if (
    !(await confirmTyped(t('Delete the computer account {name}? It leaves the domain and will have to rejoin.', { name: c.name }), c.name))
  )
    return
  try {
    await del(`/computers/${enc(c.sam)}`)
    toast.removed(t('{name} deleted.', { name: c.name }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <input v-model="search" class="inp w-56" :placeholder="t('Filter by name…')" :aria-label="t('Filter')" />
    <select v-model="os" class="inp w-64" :aria-label="t('System')">
      <option value="">{{ t('All systems') }}</option>
      <option v-for="s in systems" :key="s" :value="s">{{ s || t('not reported') }}</option>
    </select>
    <label class="flex items-center gap-1 text-[12px]"
      ><input v-model="onlyInactive" type="checkbox" /> {{ t('no logon for {n}+ days', { n: auth.settings?.inactive_days || 90 }) }}</label
    >
    <span class="text-[12px] text-fg-muted">{{ t('{n} of {total}', { n: visible.length, total: list.length }) }}</span>
    <button class="btn-ghost ml-auto" @click="dns = true"><Globe :size="13" /> DNS</button>
    <button class="btn-ghost" @click="exportCsv"><Download :size="13" /> {{ t('Export CSV') }}</button>
  </div>
  <div class="max-h-[calc(var(--view-h)-260px)] overflow-auto border border-line">
    <table class="grid-t border-0">
      <thead>
        <tr>
          <th>{{ t('Name') }}</th>
          <th>{{ t('System') }}</th>
          <th>{{ t('Version') }}</th>
          <th>{{ t('Status') }}</th>
          <th>{{ t('Last logon (approx.)') }}</th>
          <th class="text-right">{{ t('Actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <LoadingRow v-if="!loaded" :cols="6" />
        <tr v-for="c in visible" :key="c.name" :class="c.enabled ? '' : 'text-fg-subtle'">
          <td class="font-semibold">{{ c.name }}</td>
          <td
            :class="legacy(c.os) ? 'text-err' : ''"
            :title="legacy(c.os) ? t('Unsupported system: watch out for NTLMv1/SMB1/Netlogon') : ''"
          >
            {{ c.os }}
          </td>
          <td>{{ c.os_version }}</td>
          <td class="space-x-1">
            <span v-if="c.is_dc" class="tag bg-strong-fill text-on-strong-fill">DC</span
            ><span v-if="!c.enabled" class="tag bg-neutral-fill text-on-neutral-fill">{{ t('deactivated') }}</span>
          </td>
          <td :class="inactive(c.last_logon) ? 'text-warn' : ''">{{ formatDate(c.last_logon) }}</td>
          <td class="text-right whitespace-nowrap">
            <button v-if="c.enabled && (c.is_dc || /server/i.test(c.os || ''))" class="act" @click="sess = c.dns_name || c.name">
              <Users :size="12" /> {{ t('sessions') }}
            </button>
            <template v-if="!c.is_dc">
              <button class="act" @click="toggle(c)"><Power :size="12" /> {{ c.enabled ? t('disable') : t('enable') }}</button>
              <button class="act hover:!text-err" @click="remove(c)"><Trash2 :size="12" /> {{ t('delete') }}</button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <SmbSessionsDialog v-if="sess" :host="sess" @close="sess = null" />

  <DnsRecordsDialog v-if="dns" @close="dns = false" />
</template>
