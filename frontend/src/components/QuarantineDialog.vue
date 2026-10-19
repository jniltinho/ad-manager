<script setup>
// The quarantines of a sync job: what its runs deleted or replaced. Files go back to the run's destination (a file
// there already swaps places with the one given back only when "replace" is checked), one downloads, and the whole
// quarantine of the job empties with a typed confirmation.
import { Download, RotateCcw, Trash2 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { enc, get, post } from '../api'
import { confirmTyped, showAlert } from '../dialogs'
import { serverText, t } from '../i18n'
import { toast } from '../toasts'
import { formatDate } from '../util'
import Modal from './Modal.vue'

const props = defineProps({
  agent: { type: Number, required: true },
  job: { type: Object, required: true },
  quarantines: { type: Array, required: true }, // of this job, newest first
  open: { type: String, default: '' }, // the quarantine to show first
})
const emit = defineEmits(['close', 'changed'])
const base = `/agents/${props.agent}/syncs`

const size = (n) =>
  n >= 1 << 30
    ? (n / (1 << 30)).toFixed(1) + ' GiB'
    : n >= 1 << 20
      ? (n / (1 << 20)).toFixed(1) + ' MiB'
      : Math.ceil((n || 0) / 1024) + ' KiB'
const current = ref(null)
const files = ref([])
const truncated = ref(false)
const picked = ref([])
const replace = ref(false)
const busy = ref(false)
const total = computed(() => props.quarantines.reduce((s, q) => s + q.size, 0))

let asked = 0 // the newest list asked for: an older answer that arrives late is dropped
async function show(q) {
  const n = ++asked
  current.value = q
  picked.value = []
  files.value = []
  try {
    const r = await get(`${base}/quarantines/${enc(q.name)}/files`)
    if (n !== asked) return
    files.value = r.files
    truncated.value = r.truncated
  } catch (e) {
    if (n === asked) toast.error(serverText(e.message))
  }
}
if (props.quarantines.length) show(props.quarantines.find((q) => q.name === props.open) || props.quarantines[0])

const fileUrl = (p) => `/api${base}/quarantines/${enc(current.value.name)}/file?path=${encodeURIComponent(p)}`

async function giveBack(all) {
  busy.value = true
  const q = current.value // the answer belongs to this one
  try {
    const r = await post(`${base}/quarantines/${enc(q.name)}/restore`, {
      paths: all ? [] : picked.value,
      replace: replace.value,
    })
    const lines = [t('{n} files given back.', { n: r.restored + r.swapped })]
    if (r.swapped) lines.push(t('{n} files of the destination went into the quarantine in their place.', { n: r.swapped }))
    if (r.conflict_count)
      lines.push(
        t('{n} files are in the destination already (check "replace" to swap them):', { n: r.conflict_count }),
        ...r.conflicts.slice(0, 20),
      )
    if (r.error_count) lines.push(t('Errors:') + ' ' + r.error_count, ...r.errors.slice(0, 20).map(serverText))
    await showAlert(lines.join('\n'), { title: t('Give back') })
    if (current.value === q) await show(q)
    emit('changed')
  } catch (e) {
    toast.error(serverText(e.message))
  }
  busy.value = false
}

async function empty() {
  const msg = t('Remove every quarantine of {name} ({size})? Nothing there can be given back afterwards.', {
    name: props.job.name || props.job.id,
    size: size(total.value),
  })
  if (!(await confirmTyped(msg, props.job.id))) return
  busy.value = true
  try {
    const r = await post(`${base}/${enc(props.job.id)}/quarantine/empty`, { confirm: props.job.id })
    toast.success(t('{n} quarantines removed ({size}).', { n: r.removed, size: size(r.freed) }))
    emit('changed')
    emit('close')
  } catch (e) {
    toast.error(serverText(e.message))
  }
  busy.value = false
}
</script>

<template>
  <Modal :title="t('Quarantine of {name}', { name: job.name || job.id })" wide :ok="null" @close="emit('close')">
    <div class="space-y-3 text-[12px]">
      <p class="text-fg-muted">
        {{
          t('What the runs deleted or replaced, kept {days} days{gib}. Files go back to where they were in the destination of that run.', {
            days: job.keep_days || 30,
            gib: job.keep_gib ? t(' and up to {n} GiB', { n: job.keep_gib }) : '',
          })
        }}
      </p>
      <p v-if="!quarantines.length" class="py-3 text-center text-fg-muted">{{ t('The quarantine is empty.') }}</p>
      <template v-else>
        <table class="grid-t">
          <thead>
            <tr>
              <th>{{ t('Run') }}</th>
              <th>{{ t('Destination') }}</th>
              <th class="text-right">{{ t('Files') }}</th>
              <th class="text-right">{{ t('Size') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="q in quarantines"
              :key="q.name"
              class="cursor-pointer"
              :class="current?.name === q.name ? 'bg-row-hover font-semibold' : 'hover:bg-row-hover'"
              @click="show(q)"
            >
              <td>{{ formatDate(q.created) }}</td>
              <td class="max-w-64 truncate font-mono text-[11px]" :title="q.dest">{{ q.dest }}</td>
              <td class="text-right">{{ q.files }}</td>
              <td class="text-right">{{ size(q.size) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="current" class="space-y-2">
          <div class="max-h-64 overflow-auto border border-line">
            <table class="grid-t border-0">
              <thead>
                <tr>
                  <th class="w-6"></th>
                  <th>{{ t('File') }}</th>
                  <th class="text-right">{{ t('Size') }}</th>
                  <th>{{ t('Modified') }}</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="f in files" :key="f.path">
                  <td><input v-model="picked" type="checkbox" :value="f.path" :aria-label="f.path" /></td>
                  <td class="font-mono text-[11px] break-all">
                    {{ f.path }}
                    <span v-if="f.in_dest" class="tag bg-surface-2 text-warn">{{ t('in the destination') }}</span>
                  </td>
                  <td class="text-right whitespace-nowrap">{{ size(f.size) }}</td>
                  <td class="whitespace-nowrap">{{ formatDate(f.modified) }}</td>
                  <td>
                    <a class="act px-0" :href="fileUrl(f.path)" :title="t('Download')" download><Download :size="13" /></a>
                  </td>
                </tr>
                <tr v-if="!files.length">
                  <td colspan="5" class="py-3 text-center text-fg-muted">{{ t('No files.') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="truncated" class="text-fg-muted">{{ t('Only the first 2000 files are listed; "give back all" takes every one.') }}</p>
          <label class="flex items-center gap-2"
            ><input v-model="replace" type="checkbox" />
            {{ t('Replace files in the destination (the current ones go into the quarantine in their place)') }}</label
          >
        </div>
      </template>
    </div>
    <template #foot>
      <button type="button" class="btn-danger mr-auto" :disabled="busy || !quarantines.length" @click="empty">
        <Trash2 :size="14" /> {{ t('Empty the quarantine') }}
      </button>
      <button type="button" class="btn-ghost" :disabled="busy || !picked.length" @click="giveBack(false)">
        <RotateCcw :size="14" /> {{ t('Give back selected') }}
      </button>
      <button type="button" class="btn" :disabled="busy || !files.length" @click="giveBack(true)">
        <RotateCcw :size="14" /> {{ t('Give back all') }}
      </button>
    </template>
  </Modal>
</template>
