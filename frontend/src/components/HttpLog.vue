<script setup>
// HTTP log of the panel: every API call (and /asynqmon) with operator, status
// and duration, from the server's access.log. Read-only.
import { Download, RefreshCw } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { get } from '../api'
import { t } from '../i18n'
import { toast } from '../toasts'
import { downloadCsv, formatDate } from '../util'
import LoadingRow from './LoadingRow.vue'

const list = ref([])
const search = ref('')
const klass = ref('') // '', '2', '3', '4', '5' (first digit of the status)
const errorsOnly = ref(false)
const loading = ref(false)
async function load() {
  loading.value = true
  try {
    list.value = await get('/http-log?limit=2000')
  } catch (e) {
    toast.error(e.message)
  } finally {
    loading.value = false
  }
}
load()

const visible = computed(() => {
  const q = search.value.trim().toLowerCase()
  return list.value
    .filter((r) => !q || [r.ip, r.user, r.method, r.path, r.error].some((v) => v?.toLowerCase().includes(q)))
    .filter((r) => !klass.value || String(r.status)[0] === klass.value)
    .filter((r) => !errorsOnly.value || r.status >= 400)
})

// statusTag colors a status like the other tags of the panel.
function statusTag(s) {
  if (s >= 500) return 'bg-err-fill text-on-err-fill'
  if (s >= 400) return 'bg-warn-fill text-on-warn-fill'
  if (s >= 300) return 'bg-surface-2 text-fg-muted'
  return 'bg-ok-soft text-on-ok-soft'
}
</script>

<template>
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <input v-model="search" class="inp w-64" :placeholder="t('Filter by path, operator, IP…')" :aria-label="t('Filter')" />
    <select v-model="klass" class="inp w-40" :aria-label="t('Code')">
      <option value="">{{ t('All codes') }}</option>
      <option value="2">2xx</option>
      <option value="3">3xx</option>
      <option value="4">4xx</option>
      <option value="5">5xx</option>
    </select>
    <label class="flex items-center gap-1 text-[12px]"><input v-model="errorsOnly" type="checkbox" /> {{ t('errors only') }}</label>
    <span class="text-[12px] text-fg-muted">{{ t('{n} of {total} (newest first)', { n: visible.length, total: list.length }) }}</span>
    <div class="ml-auto flex gap-2">
      <button class="btn-ghost" :disabled="loading" @click="load"><RefreshCw :size="13" /> {{ t('Refresh') }}</button>
      <button
        class="btn-ghost"
        @click="
          downloadCsv(t('http.csv'), visible, [
            [t('Date'), (r) => formatDate(r.time)],
            [t('Source'), 'ip'],
            [t('Operator'), 'user'],
            [t('Method'), 'method'],
            [t('Path'), 'path'],
            [t('Code'), 'status'],
            ['ms', 'ms'],
            [t('Error'), 'error'],
          ])
        "
      >
        <Download :size="13" /> {{ t('Export CSV') }}
      </button>
    </div>
  </div>
  <div class="max-h-[calc(var(--view-h)-260px)] overflow-auto border border-line">
    <!-- fixed columns, one line per request: long paths end in "…" and show in the tooltip -->
    <table class="grid-t border-0 table-fixed">
      <colgroup>
        <!-- the date and time in Verdana need about 150 px: narrower, it runs into the source -->
        <col class="w-38" />
        <col class="w-28" />
        <col class="w-32" />
        <col class="w-16" />
        <col />
        <col class="w-16" />
        <col class="w-16" />
        <col class="w-[18%]" />
      </colgroup>
      <thead>
        <tr>
          <th>{{ t('Date') }}</th>
          <th>{{ t('Source') }}</th>
          <th>{{ t('Operator') }}</th>
          <th>{{ t('Method') }}</th>
          <th>{{ t('Path') }}</th>
          <th>{{ t('Code') }}</th>
          <th class="text-right">ms</th>
          <th>{{ t('Error') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in visible" :key="i">
          <td class="whitespace-nowrap">{{ formatDate(r.time) }}</td>
          <td class="truncate" :title="r.ip">{{ r.ip }}</td>
          <td class="truncate font-semibold text-fg-strong" :title="r.user">{{ r.user || '—' }}</td>
          <td class="font-mono text-[11px]">{{ r.method }}</td>
          <td class="truncate font-mono text-[11px]" :title="r.path">{{ r.path }}</td>
          <td>
            <span class="tag w-10 text-center" :class="statusTag(r.status)">{{ r.status }}</span>
          </td>
          <td class="text-right tabular-nums">{{ r.ms.toLocaleString(undefined, { maximumFractionDigits: 1 }) }}</td>
          <td class="truncate text-err" :title="r.error">{{ r.error }}</td>
        </tr>
        <LoadingRow v-if="loading && !visible.length" :cols="8" />
        <tr v-else-if="!visible.length">
          <td colspan="8" class="py-4 text-center text-fg-muted">{{ t('No requests.') }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
