<script setup>
// A group's detail: its description (saved with the dialog's button, emitting "saved") and its direct members, added
// and removed one by one (each change emits "changed" so the page refreshes the member count).
import { Trash2 } from 'lucide-vue-next'
import { ref } from 'vue'
import { post, put, del, enc } from '../../api'
import { toast } from '../../toasts'
import { t } from '../../i18n'
import Modal from '../Modal.vue'
import Suggestions from '../Suggestions.vue'

const props = defineProps({
  onSaved: { type: Function, default: null },
  onChanged: { type: Function, default: null },
  group: { type: Object, required: true }, // as GET /groups/{name} answers
  logins: { type: Array, required: true }, // { sam, name } of the users, for suggestions and names
})
// the page hears the result through callback props (@saved/@changed): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])

const detail = ref({ ...props.group, members: props.group.members ?? [], add: '' })
const busy = ref(false)
async function saveDescription() {
  busy.value = true
  try {
    await put(`/groups/${enc(detail.value.name)}`, { description: detail.value.description || '' })
    toast.success(t('Description saved.'))
    props.onSaved?.()
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
async function member(m, add) {
  const g = detail.value
  try {
    if (add) await post(`/groups/${enc(g.name)}/members`, { member: m })
    else await del(`/groups/${enc(g.name)}/members/${enc(m)}`)
    g.members = add ? [...g.members, m] : g.members.filter((x) => x !== m)
    g.add = ''
    toast.success(t(add ? '{member} added.' : '{member} removed.', { member: m }))
    props.onChanged?.()
  } catch (e) {
    toast.error(e.message)
  }
}
const nameOf = (sam) => props.logins.find((u) => u.sam === sam)?.name || ''
</script>

<template>
  <Modal
    :title="t('Group {name}', { name: detail.name })"
    wide
    :busy="busy"
    :ok="t('Save description')"
    @close="emit('close')"
    @submit="saveDescription"
  >
    <label class="lbl" for="dd">{{ t('Description') }}</label>
    <input id="dd" v-model="detail.description" class="inp mb-4" />
    <div class="mb-2 flex gap-2">
      <Suggestions
        v-model="detail.add"
        :options="logins.filter((u) => !detail.members.includes(u.sam)).map((u) => ({ value: u.sam, label: u.name }))"
        :placeholder="t('New member\'s login (computers end with $)')"
        :aria-label="t('New member')"
      />
      <button type="button" class="btn" :disabled="!detail.add" @click="member(detail.add, true)">{{ t('Add') }}</button>
    </div>
    <table class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Member ({n})', { n: detail.members.length }) }}</th>
          <th>{{ t('Name') }}</th>
          <th class="text-right">{{ t('Action') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in [...detail.members].sort()" :key="m">
          <td class="font-mono">{{ m }}</td>
          <td>{{ nameOf(m) }}</td>
          <td class="text-right">
            <button type="button" class="act" @click="member(m, false)"><Trash2 :size="12" /> {{ t('remove') }}</button>
          </td>
        </tr>
        <tr v-if="!detail.members.length">
          <td colspan="3" class="text-fg-muted">{{ t('No direct members.') }}</td>
        </tr>
      </tbody>
    </table>
  </Modal>
</template>
