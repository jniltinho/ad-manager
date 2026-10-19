<script setup>
// SMB sessions of a server, read on open with the operator's credential (read only).
import { ref } from 'vue'
import { get, enc } from '../../api'
import { t } from '../../i18n'
import Modal from '../Modal.vue'
import LoadingNote from '../LoadingNote.vue'

const props = defineProps({ host: { type: String, required: true } })
const emit = defineEmits(['close'])

const sess = ref({ loading: true })
get(`/computers/sessions?host=${enc(props.host)}`)
  .then((r) => (sess.value = { list: r.sessions }))
  .catch((e) => (sess.value = { error: e.message }))
// 0xFFFFFFFF: the server did not report it
const duration = (s) =>
  s >= 0xffffffff ? '—' : s >= 3600 ? `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min` : `${Math.floor(s / 60)} min`
</script>

<template>
  <Modal wide :title="t('SMB sessions on {host}', { host })" :ok="null" @close="emit('close')">
    <LoadingNote v-if="sess.loading" />
    <p v-else-if="sess.error" class="text-err">{{ sess.error }}</p>
    <table v-else class="grid-t table-fixed">
      <thead>
        <tr>
          <th>{{ t('User') }}</th>
          <th>{{ t('Client') }}</th>
          <th class="w-28 text-right">{{ t('Open files') }}</th>
          <th class="w-28 text-right">{{ t('Connected for') }}</th>
          <th class="w-28 text-right">{{ t('Idle for') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="x in sess.list" :key="x.user + x.client">
          <td class="truncate font-semibold">{{ x.user }}</td>
          <td class="truncate font-mono text-[11px]">{{ x.client }}</td>
          <td class="text-right tabular-nums">{{ x.open_files }}</td>
          <td class="text-right tabular-nums">{{ duration(x.active_seconds) }}</td>
          <td class="text-right tabular-nums">{{ duration(x.idle_seconds) }}</td>
        </tr>
        <tr v-if="!sess.list.length">
          <td colspan="5" class="py-3 text-center text-fg-muted">{{ t('Nobody connected.') }}</td>
        </tr>
      </tbody>
    </table>
    <p class="mt-2 text-[11px] text-fg-muted">{{ t('Read with your account: it needs to be an administrator of that server.') }}</p>
  </Modal>
</template>
