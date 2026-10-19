<script setup>
// Registration of a file server (ad-manager agent): name, URL, token and certificate check.
import { ref } from 'vue'
import { post, put } from '../../api'
import { t } from '../../i18n'
import { toast } from '../../toasts'
import Modal from '../Modal.vue'

const props = defineProps({ agent: Object, onSaved: { type: Function, default: null } }) // { id?, name, url, token, insecure }
// the page hears the result through callback props (@saved): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])
const ag = ref({ ...props.agent })
const busy = ref(false)

async function save() {
  busy.value = true
  try {
    const a = ag.value.id ? await put(`/agents/${ag.value.id}`, ag.value) : await post('/agents', ag.value)
    toast.success(t('Server {name} saved.', { name: a.name }))
    props.onSaved?.()
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <Modal :title="ag.id ? t('Server {name}', { name: ag.name }) : t('New file server')" :busy="busy" @close="emit('close')" @submit="save">
    <div class="space-y-3">
      <div>
        <label class="lbl" for="an">{{ t('Name') }}</label
        ><input id="an" v-model.trim="ag.name" class="inp" placeholder="fs01" required />
      </div>
      <div>
        <label class="lbl" for="au">{{ t('Agent URL') }}</label
        ><input id="au" v-model.trim="ag.url" class="inp font-mono" placeholder="https://fs01.example.local:9443" required />
      </div>
      <div>
        <label class="lbl" for="at">Token {{ ag.id ? t('(empty = keep the current one)') : '' }}</label>
        <input id="at" v-model="ag.token" type="password" class="inp font-mono" autocomplete="off" :required="!ag.id" />
        <p class="mt-1 text-[11px] text-fg-muted">{{ t('Generated on the server with') }} <code>sudo ad-manager agent token</code>.</p>
      </div>
      <label class="flex items-center gap-2"
        ><input v-model="ag.insecure" type="checkbox" /> {{ t('Accept self-signed certificate') }}</label
      >
    </div>
  </Modal>
</template>
