<script setup>
// New group: name, description, type, scope and OU; emits "done" once the group is created so the page reloads.
import { ref } from 'vue'
import { post } from '../../api'
import { toast } from '../../toasts'
import { t } from '../../i18n'
import Modal from '../Modal.vue'

const props = defineProps({
  onDone: { type: Function, default: null },
  draft: { type: Object, required: true }, // the initial fields
  ous: { type: Array, required: true },
})
// the page hears the result through callback props (@done): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])

const form = ref({ ...props.draft })
const busy = ref(false)
async function create() {
  busy.value = true
  try {
    await post('/groups', form.value)
    toast.success(t('Group {name} created.', { name: form.value.name }))
    props.onDone?.()
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <Modal :title="t('New group')" :busy="busy" :ok="t('Create')" @close="emit('close')" @submit="create">
    <div class="space-y-3">
      <div>
        <label class="lbl" for="gn">{{ t('Name') }}</label
        ><input id="gn" v-model.trim="form.name" class="inp" required autofocus />
      </div>
      <div>
        <label class="lbl" for="gd">{{ t('Description') }}</label
        ><input id="gd" v-model="form.description" class="inp" />
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="lbl" for="gt">{{ t('Group type') }}</label
          ><select id="gt" v-model="form.type" class="inp">
            <option value="security">{{ t('Security') }}</option>
            <option value="distribution">{{ t('Distribution') }}</option>
          </select>
        </div>
        <div>
          <label class="lbl" for="gs">{{ t('Scope') }}</label
          ><select id="gs" v-model="form.scope" class="inp">
            <option value="global">{{ t('Global') }}</option>
            <option value="domainlocal">{{ t('Domain local') }}</option>
            <option value="universal">{{ t('Universal') }}</option>
          </select>
        </div>
      </div>
      <div>
        <label class="lbl" for="go">{{ t('OU') }}</label
        ><select id="go" v-model="form.ou" class="inp">
          <option v-for="o in ous" :key="o.dn" :value="o.dn">{{ o.dn }}</option>
        </select>
      </div>
    </div>
  </Modal>
</template>
