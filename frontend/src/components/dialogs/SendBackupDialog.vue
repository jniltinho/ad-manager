<script setup>
// Send a backup (.tar.gz) to a server: streamed, with progress. Calls "done" with the server's answer once stored.
import { ref } from 'vue'
import { enc, platform } from '../../api'
import { t } from '../../i18n'
import { sendFile } from '../../util'
import { toast } from '../../toasts'
import Modal from '../Modal.vue'

const props = defineProps({
  base: { type: String, required: true }, // the server's backups route
  kinds: { type: Array, required: true }, // the kinds the server takes
  maxMib: Number,
  kindName: { type: Function, required: true },
  // a prop rather than an emitted event: the upload goes on when the dialog closes, and the page still reloads
  onDone: { type: Function, default: null },
})
const emit = defineEmits(['close'])

const kind = ref(props.kinds[0])
const file = ref(null)
const progress = ref(-1)

// the web sends the file picked below; the desktop picks and streams it in Go (sendFile)
const desktop = !!platform.sendFile
async function send() {
  if (!file.value && !desktop) return
  progress.value = 0
  try {
    const data = await sendFile(`/api${props.base}/upload?kind=${enc(kind.value)}`, file.value, (p) => (progress.value = p))
    if (data) props.onDone?.(data)
    else progress.value = -1 // the desktop's file dialog was cancelled
  } catch (e) {
    progress.value = -1
    toast.error(e.message)
  }
}
</script>

<template>
  <Modal :title="t('Send a backup')" :busy="progress >= 0" :ok="t('Send')" @close="emit('close')" @submit="send">
    <div class="space-y-3 text-[12px]">
      <div>
        <label class="lbl" for="sk">{{ t('Kind') }}</label>
        <select id="sk" v-model="kind" class="inp">
          <option v-for="k in kinds" :key="k" :value="k">{{ kindName(k) }}</option>
        </select>
      </div>
      <div>
        <label class="lbl" for="sfile">{{ t('File (.tar.gz downloaded from a Backup page)') }}</label>
        <input
          v-if="!desktop"
          id="sfile"
          type="file"
          accept=".gz,.tgz,application/gzip"
          class="inp"
          required
          @change="file = $event.target.files[0]"
        />
        <p v-else id="sfile" class="text-fg-muted">{{ t('Send asks for the file.') }}</p>
      </div>
      <div v-if="desktop && progress >= 0">
        <button type="button" class="btn-ghost" @click="platform.cancelSend()">{{ t('Stop sending') }}</button>
      </div>
      <p class="text-fg-muted">
        {{
          t('The server checks the content for the kind and lists it as sent; then it restores like the others. Up to {n} MiB.', {
            n: maxMib,
          })
        }}
      </p>
      <div v-if="progress >= 0" class="h-2 bg-surface-2">
        <div class="h-2 bg-ok-fill transition-[width]" :style="{ width: progress + '%' }"></div>
      </div>
    </div>
  </Modal>
</template>
