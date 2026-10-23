<script setup>
// Raw log of a sync run, in a dialog whose summary header and footer stay while only the monospace log body scrolls
// (a dry-run can print thousands of lines). Read only; Copy puts the whole log on the clipboard.
import { ref } from 'vue'
import { Copy } from 'lucide-vue-next'
import { t } from '../../i18n'
import { formatDate } from '../../util'
import { toast } from '../../toasts'
import Modal from '../Modal.vue'

const props = defineProps({
  run: { type: Object, required: true },
  text: { type: String, default: '' },
  jobName: String,
})
defineEmits(['close'])

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

const copied = ref(false)
async function copy() {
  try {
    await navigator.clipboard.writeText(props.text)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <Modal :title="t('Log {id}', { id: run.id })" wide :ok="null" @close="$emit('close')">
    <div class="space-y-2">
      <!-- summary: stays above the scrolling log -->
      <p class="text-[12px]">
        <b>{{ jobName }}</b> · {{ formatDate(run.started) }} ·
        <span :class="statusClass(run.status)">{{ statusText(run.status) }}</span>
        <template v-if="run.result?.plan">
          · {{ t('New') }} {{ run.result.plan.New }} ({{ size(run.result.plan.NewBytes) }}) · {{ t('Changed') }}
          {{ run.result.plan.Changed }} ({{ size(run.result.plan.ChangedBytes) }}) · {{ t('To delete') }} {{ run.result.plan.Delete }}
          <template v-if="!run.preview"> · {{ t('Transferred') }} {{ run.result.transfers }} ({{ size(run.result.bytes) }})</template>
        </template>
      </p>
      <!-- v-text, not {{ }}: Vue preserves whitespace inside <pre>, so a line-wrapped interpolation would show a blank line -->
      <pre
        class="max-h-[calc(var(--view-h)*0.6)] overflow-auto whitespace-pre-wrap break-words bg-surface-2 p-2 font-mono text-[11px]"
        v-text="text || t('No log.')"
      />
    </div>
    <template #foot>
      <button type="button" class="btn-ghost" @click="copy"><Copy :size="14" /> {{ copied ? t('Copied') : t('Copy') }}</button>
    </template>
  </Modal>
</template>
