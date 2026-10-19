<script setup>
// Move a user to another OU. Emits "done" after the move.
import { ref } from 'vue'
import { post, enc } from '../../api'
import { toast } from '../../toasts'
import { t } from '../../i18n'
import Modal from '../Modal.vue'

// user: { sam, ou } with the current parent OU; ous: OU list
const props = defineProps({ user: Object, ous: Array, onDone: { type: Function, default: null } })
// the page hears the result through callback props (@done): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])
const ou = ref(props.user.ou)

async function save() {
  try {
    await post(`/users/${enc(props.user.sam)}/move`, { ou: ou.value })
    toast.success(t('User moved.'))
    props.onDone?.()
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <Modal :title="t('Move {user}', { user: user.sam })" :ok="t('Move')" @close="emit('close')" @submit="save">
    <label class="lbl" for="mou">{{ t('Destination') }}</label>
    <select id="mou" v-model="ou" class="inp">
      <option v-for="o in ous" :key="o.dn" :value="o.dn">{{ o.dn }}</option>
    </select>
  </Modal>
</template>
