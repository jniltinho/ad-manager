<script setup>
// Logs: the audit trail (every change made through the panel, CLI or agent —
// the server's audit.log), the HTTP log of the panel API and the Samba logs of
// this host and of each file server.
import { Download, ExternalLink, Globe, RefreshCw, ScrollText, Server } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { auth, get } from '../api'
import { t } from '../i18n'
import { toast } from '../toasts'
import { downloadCsv, formatDate, spinner } from '../util'
import SambaLogs from '../components/SambaLogs.vue'
import HttpLog from '../components/HttpLog.vue'
import LoadingRow from '../components/LoadingRow.vue'

// tabs: 'audit', 'http', 'samba' (this host) or an agent id
const tab = ref('audit')
const servers = ref([])
get('/agents')
  .then((r) => (servers.value = r))
  .catch(() => {})

const list = ref([])
const search = ref('')
const action = ref('')
const failures = ref(false)
const loading = ref(false)
const spin = spinner(loading) // the refresh icon turns while loading, at least briefly
async function load() {
  loading.value = true
  try {
    list.value = await get('/audit?limit=2000')
  } catch (e) {
    toast.error(e.message)
  } finally {
    loading.value = false
  }
}
load()

const actions = computed(() => [...new Set(list.value.map((e) => e.action))].sort())
const visible = computed(() => {
  const q = search.value.trim().toLowerCase()
  return list.value
    .filter((e) => !q || [e.actor, e.ip, e.target, e.result].some((v) => v?.toLowerCase().includes(q)))
    .filter((e) => !action.value || e.action === action.value)
    .filter((e) => !failures.value || e.result !== 'ok')
})
</script>

<template>
  <div class="subtabs">
    <button
      v-for="[key, label, icon] in [
        ['audit', t('Audit'), ScrollText],
        ['http', t('HTTP — panel'), Globe],
        ['samba', t('Samba — this server'), Server],
        ...servers.map((a) => [a.id, t('Samba — {name}', { name: a.name }), Server]),
      ]"
      :key="key"
      type="button"
      class="subtab"
      :class="{ on: tab === key }"
      @click="tab = key"
    >
      <component :is="icon" :size="13" /> {{ label }}
    </button>
  </div>

  <HttpLog v-if="tab === 'http'" />
  <SambaLogs v-else-if="tab === 'samba'" base="/samba/logs" />
  <SambaLogs v-else-if="tab !== 'audit'" :key="tab" :base="`/agents/${tab}/samba/logs`" />
  <template v-else>
    <div class="mb-3 flex flex-wrap items-center gap-2">
      <input v-model="search" class="inp w-64" :placeholder="t('Filter by operator, target, IP…')" :aria-label="t('Filter')" />
      <select v-model="action" class="inp w-56" :aria-label="t('Action')">
        <option value="">{{ t('All actions') }}</option>
        <option v-for="a in actions" :key="a" :value="a">{{ a }}</option>
      </select>
      <label class="flex items-center gap-1 text-[12px]"><input v-model="failures" type="checkbox" /> {{ t('failures only') }}</label>
      <span class="text-[12px] text-fg-muted">{{ t('{n} of {total} (newest first)', { n: visible.length, total: list.length }) }}</span>
      <div class="ml-auto flex gap-2">
        <a v-if="auth.asynqmon" href="/asynqmon" target="_blank" rel="noopener" class="btn-ghost"
          ><ExternalLink :size="13" /> {{ t('Queues (asynqmon)') }}</a
        >
        <button class="btn-ghost" :disabled="loading" @click="load">
          <RefreshCw :class="spin && 'motion-safe:animate-spin'" :size="13" /> {{ t('Refresh') }}
        </button>
        <button
          class="btn-ghost"
          @click="
            downloadCsv(t('audit.csv'), visible, [
              [t('Date'), (e) => formatDate(e.time)],
              [t('Operator'), 'actor'],
              [t('Source'), 'ip'],
              [t('Action'), 'action'],
              [t('Target'), 'target'],
              [t('Result'), 'result'],
            ])
          "
        >
          <Download :size="13" /> {{ t('Export CSV') }}
        </button>
      </div>
    </div>
    <div class="max-h-[calc(var(--view-h)-306px)] overflow-auto border border-line">
      <table class="grid-t border-0">
        <thead>
          <tr>
            <th>{{ t('Date') }}</th>
            <th>{{ t('Operator') }}</th>
            <th>{{ t('Source') }}</th>
            <th>{{ t('Action') }}</th>
            <th>{{ t('Target') }}</th>
            <th>{{ t('Result') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(e, i) in visible" :key="i">
            <td class="whitespace-nowrap">{{ formatDate(e.time) }}</td>
            <td class="font-semibold text-fg-strong">{{ e.actor }}</td>
            <td class="whitespace-nowrap">{{ e.ip }}</td>
            <td class="whitespace-nowrap font-mono text-[11px]">{{ e.action }}</td>
            <td>{{ e.target }}</td>
            <td>
              <span v-if="e.result === 'ok'" class="tag bg-ok-soft text-on-ok-soft">ok</span>
              <span v-else class="text-err">{{ e.result }}</span>
            </td>
          </tr>
          <LoadingRow v-if="loading && !visible.length" :cols="6" />
          <tr v-else-if="!visible.length">
            <td colspan="6" class="py-4 text-center text-fg-muted">{{ t('No events.') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </template>
</template>
