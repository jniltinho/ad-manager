<script setup>
// Sessions and open files of a file server (smbstatus through the agent), with disconnect.
import { onUnmounted, ref } from 'vue'
import { get, post } from '../../api'
import { t } from '../../i18n'
import { toast } from '../../toasts'
import { askConfirm } from '../../dialogs'
import { formatDate } from '../../util'
import Modal from '../Modal.vue'
import LoadingNote from '../LoadingNote.vue'

const props = defineProps({ server: Object })
defineEmits(['close'])

const sess = ref({ loading: true })
let open = true // closed meanwhile: a late reply or reload is dropped
onUnmounted(() => (open = false))

async function load() {
  sess.value = { ...sess.value, loading: true }
  try {
    const r = await get(`/agents/${props.server.id}/sessions`)
    if (open) sess.value = r
  } catch (e) {
    if (open) sess.value = { error: e.message }
  }
}
load()

async function closeSession(s) {
  const files = sess.value.open_files.filter((f) => f.pid === s.pid).length
  let msg = t('Disconnect {user} ({machine})? Their open files ({n}) are closed and unsaved changes in them are lost.', {
    user: s.user,
    machine: s.machine,
    n: files,
  })
  // the server process may serve other sessions too: they end with it
  const others = sess.value.sessions.filter((x) => x.pid === s.pid && x.id !== s.id)
  if (others.length)
    msg += ' ' + t('Also disconnected (same connection): {who}.', { who: others.map((x) => `${x.user} (${x.machine})`).join(', ') })
  if (!(await askConfirm(msg, { ok: t('Disconnect') }))) return
  try {
    const r = await post(`/agents/${props.server.id}/sessions/close`, { pid: s.pid, session_id: s.id })
    toast.success(t('{user} disconnected.', { user: r.closed.map((x) => x.user).join(', ') }))
  } catch (e) {
    toast.error(e.message)
  }
  if (open) load()
}
</script>

<template>
  <Modal
    wide
    :title="t('Sessions and open files on {server}', { server: server?.name })"
    :ok="t('Refresh')"
    :busy="sess.loading"
    @close="$emit('close')"
    @submit="load"
  >
    <p v-if="sess.error" class="text-err">{{ sess.error }}</p>
    <template v-else-if="sess.sessions">
      <table class="grid-t table-fixed">
        <thead>
          <tr>
            <th>{{ t('User') }}</th>
            <th>{{ t('Machine') }}</th>
            <th>{{ t('Shares') }}</th>
            <th class="w-24">{{ t('Protocol') }}</th>
            <th class="w-36">{{ t('Since') }}</th>
            <th class="w-28"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in sess.sessions" :key="s.id">
            <td class="truncate font-semibold" :title="s.group">{{ s.user }}</td>
            <td class="truncate font-mono text-[11px]">{{ s.machine }}</td>
            <td class="truncate" :title="s.shares.join(', ')">{{ s.shares.join(', ') }}</td>
            <td class="truncate" :title="t('Signing: {s} · encryption: {e}', { s: s.signing, e: s.encryption })">{{ s.dialect }}</td>
            <td class="whitespace-nowrap">{{ formatDate(s.since) }}</td>
            <td class="text-right">
              <button type="button" class="act hover:!text-err" @click="closeSession(s)">{{ t('Disconnect') }}</button>
            </td>
          </tr>
          <tr v-if="!sess.sessions.length">
            <td colspan="6" class="py-3 text-center text-fg-muted">{{ t('Nobody connected.') }}</td>
          </tr>
        </tbody>
      </table>
      <span class="lbl mt-4 block">{{ t('Open files') }}</span>
      <table class="grid-t table-fixed">
        <thead>
          <tr>
            <th>{{ t('File') }}</th>
            <th class="w-28">{{ t('User') }}</th>
            <th class="w-28">{{ t('Machine') }}</th>
            <th class="w-16">{{ t('Access') }}</th>
            <th class="w-36">{{ t('Opened at') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in sess.open_files" :key="f.path + f.pid + f.opened_at">
            <td class="truncate font-mono text-[11px]" :title="f.path">{{ f.path }}</td>
            <td class="truncate">{{ f.user }}</td>
            <td class="truncate font-mono text-[11px]" :title="f.machine">{{ f.machine }}</td>
            <td>{{ f.access }}</td>
            <td class="whitespace-nowrap">{{ formatDate(f.opened_at) }}</td>
          </tr>
          <tr v-if="!sess.open_files.length">
            <td colspan="5" class="py-3 text-center text-fg-muted">{{ t('No open files.') }}</td>
          </tr>
        </tbody>
      </table>
      <p class="mt-2 text-[11px] text-fg-muted">
        {{ t('Samba cannot close a single file: disconnecting the user closes all of their files.') }}
      </p>
    </template>
    <LoadingNote v-else />
  </Modal>
</template>
