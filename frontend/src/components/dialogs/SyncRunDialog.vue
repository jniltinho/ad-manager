<script setup>
// Details of a sync run: the plan (counts and a sample of each group), what was transferred, its quarantine and the
// notes. Read only; "open-quarantine" asks the page to show the run's quarantine.
import { Archive } from 'lucide-vue-next'
import { serverText, t } from '../../i18n'
import { formatDate } from '../../util'
import Modal from '../Modal.vue'

defineProps({
  run: { type: Object, required: true }, // with its result
  jobName: String,
  quarantined: Boolean, // the run's quarantine is still there to open
})
const emit = defineEmits(['close', 'open-quarantine'])

const groups = [
  ['new', 'New'],
  ['changed', 'Changed'],
  ['delete', 'To delete'],
  ['error', 'Errors'],
]
const size = (n) =>
  !n
    ? '0'
    : n >= 1 << 30
      ? (n / (1 << 30)).toFixed(1) + ' GiB'
      : n >= 1 << 20
        ? (n / (1 << 20)).toFixed(1) + ' MiB'
        : Math.ceil(n / 1024) + ' KiB'
const statusClass = (s) => (s === 'ok' ? 'text-ok' : s === 'error' ? 'text-err' : s === 'cancelled' ? 'text-fg-muted' : 'text-warn')
const statusText = (s) =>
  ({ ok: t('ok'), error: t('error'), cancelled: t('cancelled'), running: t('running'), queued: t('queued') })[s] || s
</script>

<template>
  <Modal :title="run.preview ? t('Preview {id}', { id: run.id }) : t('Run {id}', { id: run.id })" wide :ok="null" @close="emit('close')">
    <div class="space-y-3 text-[12px]">
      <p>
        <b>{{ jobName }}</b> · {{ formatDate(run.started) }} ·
        <span :class="statusClass(run.status)">{{ statusText(run.status) }}</span>
        <span v-if="run.result?.seconds"> · {{ run.result.seconds.toFixed(1) }} s</span>
      </p>
      <table class="grid-t">
        <tbody>
          <tr>
            <th class="w-48">{{ t('New') }}</th>
            <td>{{ run.result.plan.New }} ({{ size(run.result.plan.NewBytes) }})</td>
          </tr>
          <tr>
            <th>{{ t('Changed') }}</th>
            <td>{{ run.result.plan.Changed }} ({{ size(run.result.plan.ChangedBytes) }})</td>
          </tr>
          <tr>
            <th>{{ t('To delete') }}</th>
            <td>{{ run.result.plan.Delete }} ({{ size(run.result.plan.DeleteBytes) }})</td>
          </tr>
          <tr>
            <th>{{ t('Unchanged') }}</th>
            <td>{{ run.result.plan.Same }}</td>
          </tr>
          <tr v-if="!run.preview">
            <th>{{ t('Transferred') }}</th>
            <td>
              {{ run.result.transfers }} ({{ size(run.result.bytes) }}) ·
              {{
                run.result.content_verified ? t('content compared') : run.result.checked ? t('sizes and dates compared') : t('not compared')
              }}
            </td>
          </tr>
          <tr v-if="run.quarantine">
            <th>{{ t('Quarantine') }}</th>
            <td class="font-mono text-[11px]">
              {{ run.quarantine }} ({{ (run.result.quarantined || []).length }})
              <button v-if="quarantined" class="act ml-2 px-0 normal-case" @click="emit('open-quarantine')">
                <Archive :size="12" /> {{ t('open') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <template v-for="[g, label] in groups" :key="g">
        <div v-if="run.result.plan.sample?.[g]?.length">
          <div class="lbl">{{ t(label) }} ({{ t('first {n}', { n: run.result.plan.sample[g].length }) }})</div>
          <pre class="max-h-32 overflow-auto bg-surface-2 p-2 text-[11px]">{{ run.result.plan.sample[g].join('\n') }}</pre>
        </div>
      </template>
      <ul v-if="run.notes?.length" class="list-disc pl-5 text-fg-muted">
        <li v-for="n in run.notes" :key="n">{{ serverText(n) }}</li>
      </ul>
    </div>
  </Modal>
</template>
