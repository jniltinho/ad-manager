<script setup>
import { spinner } from '../util'
// Samba log viewer for one host: the files of the log directory on the left,
// the newest entries of the selected file on the right. `base` is the API path
// ('/samba/logs' for the panel's own host, '/agents/:id/samba/logs' for a file server).
import { RefreshCw } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { enc, get } from '../api'
import { locale, t } from '../i18n'
import LoadingRow from './LoadingRow.vue'

const props = defineProps({ base: { type: String, required: true } })

const dir = ref('')
const files = ref([])
const listError = ref('')
const current = ref('')
const entries = ref([])
const loading = ref(false)
const spin = spinner(loading) // the refresh icon turns while loading, at least briefly
const error = ref('')
const search = ref('')
const lines = ref(500)
const errorsOnly = ref(false)

async function loadFiles() {
  listError.value = ''
  try {
    const r = await get(props.base)
    dir.value = r.dir
    files.value = r.files || []
    listError.value = r.error || ''
    if (!files.value.some((f) => f.name === current.value)) current.value = files.value.find((f) => f.size > 0)?.name || ''
  } catch (e) {
    files.value = []
    listError.value = e.message
  }
}
async function loadEntries() {
  entries.value = []
  error.value = ''
  if (!current.value) return
  loading.value = true
  try {
    const q = new URLSearchParams({ lines: lines.value, q: search.value.trim() })
    entries.value = await get(`${props.base}/${enc(current.value)}?${q}`)
  } catch (e) {
    error.value = e.message
  }
  loading.value = false
}
async function refresh() {
  await loadFiles()
  await loadEntries()
}
watch(() => props.base, refresh, { immediate: true })
watch([current, lines], loadEntries)

const visible = computed(() => entries.value.filter((e) => !errorsOnly.value || e.level === 0))
const size = (n) => (n < 1024 ? `${n} B` : `${(n / 1024).toLocaleString(locale(), { maximumFractionDigits: 1 })} KB`)
const levelClass = (l) =>
  l === 0
    ? 'bg-err-fill text-on-err-fill'
    : l < 0
      ? 'bg-surface-2 text-fg-muted'
      : l <= 2
        ? 'bg-warn-soft text-on-warn-soft'
        : 'bg-surface-3 text-fg-strong'
</script>

<template>
  <div class="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
    <section class="card self-start">
      <div class="card-h">{{ t('Files') }}</div>
      <p class="truncate border-b border-line px-3 py-1 font-mono text-[11px] text-fg-muted" :title="dir">{{ dir }}</p>
      <p v-if="listError" class="px-3 py-2 text-[12px] text-err">{{ listError }}</p>
      <ul class="max-h-[calc(var(--view-h)-330px)] overflow-auto text-[12px]">
        <li v-for="f in files" :key="f.name">
          <button
            type="button"
            class="flex w-full cursor-pointer items-baseline justify-between gap-2 border-b border-line-soft px-3 py-1.5 text-left"
            :class="
              current === f.name ? 'bg-selection text-on-selection' : f.size ? 'hover:bg-row-hover' : 'text-fg-subtle hover:bg-row-hover'
            "
            @click="current = f.name"
          >
            <span class="truncate font-mono text-[11px]">{{ f.name }}</span>
            <span class="text-[10px] whitespace-nowrap">{{ size(f.size) }}</span>
          </button>
        </li>
        <li v-if="!files.length && !listError" class="px-3 py-2 text-fg-muted">{{ t('No log files.') }}</li>
      </ul>
    </section>

    <section class="min-w-0">
      <form class="mb-3 flex flex-wrap items-center gap-2" @submit.prevent="loadEntries">
        <input v-model="search" class="inp w-72" :placeholder="t('Search in the messages…')" :aria-label="t('Filter')" />
        <select v-model.number="lines" class="inp w-40" :aria-label="t('Entries')">
          <option v-for="n in [200, 500, 2000, 5000]" :key="n" :value="n">{{ t('last {n}', { n }) }}</option>
        </select>
        <label class="flex items-center gap-1 text-[12px]"
          ><input v-model="errorsOnly" type="checkbox" /> {{ t('level 0 only (errors)') }}</label
        >
        <span class="text-[12px] text-fg-muted">{{ t('{n} entries (newest first)', { n: visible.length }) }}</span>
        <button type="button" class="btn-ghost ml-auto" :disabled="loading" @click="refresh">
          <RefreshCw :class="spin && 'motion-safe:animate-spin'" :size="13" /> {{ t('Refresh') }}
        </button>
      </form>
      <p v-if="error" class="mb-2 border-l-4 border-err bg-err-soft px-3 py-2 text-[12px]">{{ error }}</p>
      <div class="max-h-[calc(var(--view-h)-300px)] overflow-auto border border-line">
        <table class="grid-t border-0">
          <thead>
            <tr>
              <th class="w-px">{{ t('Date') }}</th>
              <th class="w-px">{{ t('Level') }}</th>
              <th>{{ t('Message') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(e, i) in visible" :key="i">
              <td class="align-top font-mono text-[11px] whitespace-nowrap">{{ e.time || '—' }}</td>
              <td class="align-top">
                <span class="tag" :class="levelClass(e.level)">{{ e.level < 0 ? '—' : e.level }}</span>
              </td>
              <td class="align-top">
                <div class="font-mono text-[11px] break-words whitespace-pre-wrap">{{ e.message }}</div>
                <div v-if="e.source" class="mt-0.5 truncate text-[10px] text-fg-muted" :title="e.source">{{ e.source }}</div>
              </td>
            </tr>
            <LoadingRow v-if="loading && !visible.length" :cols="3" />
            <tr v-else-if="!visible.length">
              <td colspan="3" class="py-4 text-center text-fg-muted">
                {{ current ? t('No entries.') : t('Choose a file.') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="mt-2 text-[11px] text-fg-muted">
        {{ t('Times as written by Samba, in the server time zone. Read-only, never changed here.') }}
      </p>
    </section>
  </div>
</template>
