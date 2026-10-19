<script setup>
// New OU: name, description and the OU it goes inside (or the domain root); emits "done" once it is created.
import { ref } from 'vue'
import { post } from '../../api'
import { toast } from '../../toasts'
import { t } from '../../i18n'
import Modal from '../Modal.vue'

const props = defineProps({
  onDone: { type: Function, default: null },
  draft: { type: Object, required: true }, // the initial fields
  parents: { type: Array, required: true }, // the OUs a new one can go inside, in tree order
})
// the page hears the result through callback props (@done): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])

const form = ref({ ...props.draft })
const busy = ref(false)
async function create() {
  busy.value = true
  try {
    await post('/ous', form.value)
    toast.success(t('OU {name} created.', { name: form.value.name }))
    props.onDone?.()
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <Modal :title="t('New OU')" :busy="busy" :ok="t('Create')" @close="emit('close')" @submit="create">
    <div class="space-y-3">
      <div>
        <label class="lbl" for="on">{{ t('Name') }}</label
        ><input id="on" v-model.trim="form.name" class="inp" required autofocus />
      </div>
      <div>
        <label class="lbl" for="od">{{ t('Description') }}</label
        ><input id="od" v-model="form.description" class="inp" />
      </div>
      <div>
        <label class="lbl" for="op">{{ t('Inside') }}</label>
        <select id="op" v-model="form.parent" class="inp">
          <option value="">{{ t('domain root') }}</option>
          <option v-for="o in parents" :key="o.dn" :value="o.dn">{{ o.dn }}</option>
        </select>
      </div>
    </div>
  </Modal>
</template>
