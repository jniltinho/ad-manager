<script setup>
// Send a backup (.tar.gz) to a server: streamed, with progress. Calls "done" with the server's answer once stored.
import { ref } from 'vue'
import { enc } from '../../api'
import { serverText, t } from '../../i18n'
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

function send() {
  if (!file.value) return
  progress.value = 0
  const req = new XMLHttpRequest()
  req.open('POST', `/api${props.base}/upload?kind=${enc(kind.value)}`)
  req.upload.onprogress = (e) => e.lengthComputable && (progress.value = Math.round((100 * e.loaded) / e.total))
  req.onload = () => {
    let data = {}
    try {
      data = JSON.parse(req.responseText)
    } catch {
      /* not JSON */
    }
    if (req.status !== 200) {
      progress.value = -1
      return toast.error(serverText(data.error) || `HTTP ${req.status}`)
    }
    props.onDone?.(data)
  }
  req.onerror = () => {
    progress.value = -1
    toast.error(t('The upload failed (connection).'))
  }
  req.send(file.value)
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
        <input id="sfile" type="file" accept=".gz,.tgz,application/gzip" class="inp" required @change="file = $event.target.files[0]" />
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
