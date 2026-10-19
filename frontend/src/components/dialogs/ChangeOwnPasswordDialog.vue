<script setup>
// Change the operator's own password: current, new (with generator, show/hide and copy) and confirmation.
// Closes itself on success.
import { ref } from 'vue'
import { Copy, Eye, EyeOff, Wand2 } from 'lucide-vue-next'
import { post } from '../../api'
import { toast } from '../../toasts'
import { generatePassword } from '../../util'
import { t } from '../../i18n'
import Modal from '../Modal.vue'

const emit = defineEmits(['close'])
const own = ref({ current: '', next: '', confirm: '' })

// generateOwn fills new + confirm with a strong password and shows it
function generateOwn() {
  const p = generatePassword()
  Object.assign(own.value, { next: p, confirm: p, visible: true })
}
async function copyOwn() {
  await navigator.clipboard?.writeText(own.value.next)
  toast.success(t('Password copied.'))
}
async function changeOwnPassword() {
  if (own.value.next !== own.value.confirm) return toast.error(t('The confirmation does not match.'))
  own.value.busy = true
  try {
    await post('/me/password', { old: own.value.current, new: own.value.next })
    toast.success(t('Your password was changed.'))
    emit('close')
  } catch (e) {
    toast.error(e.message)
    own.value.busy = false
  }
}
</script>

<template>
  <Modal :title="t('Change my password')" :busy="own.busy" :ok="t('Change')" @close="emit('close')" @submit="changeOwnPassword">
    <label class="lbl" for="own-current">{{ t('Current password') }}</label>
    <input id="own-current" v-model="own.current" type="password" class="inp mb-3" autocomplete="current-password" required />
    <label class="lbl" for="own-next">{{ t('New password') }}</label>
    <div class="mb-3 flex">
      <input
        id="own-next"
        v-model="own.next"
        :type="own.visible ? 'text' : 'password'"
        class="inp"
        :class="own.visible && 'font-mono'"
        autocomplete="new-password"
        required
      />
      <button type="button" class="btn-ghost" :title="t('Generate a strong password')" @click="generateOwn">
        <Wand2 :size="13" /> {{ t('generate') }}
      </button>
      <button type="button" class="btn-ghost" @click="own.visible = !own.visible">
        <component :is="own.visible ? EyeOff : Eye" :size="13" /> {{ own.visible ? t('hide') : t('show') }}
      </button>
      <button type="button" class="btn-ghost" :disabled="!own.next" @click="copyOwn"><Copy :size="13" /> {{ t('copy') }}</button>
    </div>
    <label class="lbl" for="own-confirm">{{ t('Confirm') }}</label>
    <input
      id="own-confirm"
      v-model="own.confirm"
      :type="own.visible ? 'text' : 'password'"
      class="inp"
      :class="own.visible && 'font-mono'"
      autocomplete="new-password"
      required
    />
    <p class="mt-2 text-[11px] text-fg-muted">{{ t('Copy the generated password before changing: it is not shown again.') }}</p>
  </Modal>
</template>
