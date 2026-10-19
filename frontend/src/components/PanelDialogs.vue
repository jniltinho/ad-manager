<script setup>
// Port of the earlier panel's PanelDialogs.vue: global confirm, typed confirm and
// alert, using <dialog> with showModal (backdrop and focus trap).
import { computed, ref, watch, nextTick } from 'vue'
import { dialogState } from '../dialogs'
import { t } from '../i18n'

const typedInput = ref(null)
const dlgConfirm = ref(null)
const dlgTyped = ref(null)
const dlgAlert = ref(null)

watch(
  () => dialogState.confirm,
  async (v) => {
    if (v) {
      await nextTick()
      dlgConfirm.value?.showModal()
    }
  },
)
watch(
  () => dialogState.alert,
  async (v) => {
    if (v) {
      await nextTick()
      dlgAlert.value?.showModal()
    }
  },
)
watch(
  () => dialogState.typed,
  async (t) => {
    if (t) {
      await nextTick()
      dlgTyped.value?.showModal()
      typedInput.value?.focus()
    }
  },
)

// the typed-confirm button only enables when the text matches
const typedOk = computed(() => {
  const d = dialogState.typed
  return !!d && d.value.trim().toLowerCase() === d.expected.trim().toLowerCase()
})

function closeConfirm(ok) {
  dialogState.confirm.resolve(ok)
  dialogState.confirm = null
}
function closeTyped(ok) {
  dialogState.typed.resolve(ok && typedOk.value)
  dialogState.typed = null
}
function closeAlert() {
  dialogState.alert.resolve()
  dialogState.alert = null
}
</script>

<template>
  <dialog v-if="dialogState.confirm" ref="dlgConfirm" class="pnl w-fit max-w-[min(640px,92vw)]" @cancel.prevent="closeConfirm(false)">
    <div class="pnl-head">{{ dialogState.confirm.title }}</div>
    <div class="pnl-body whitespace-pre-line">{{ dialogState.confirm.msg }}</div>
    <div class="pnl-foot">
      <button type="button" class="btn" @click="closeConfirm(true)">{{ dialogState.confirm.ok }}</button>
      <button type="button" class="btn-ghost" @click="closeConfirm(false)">{{ dialogState.confirm.cancel }}</button>
    </div>
  </dialog>

  <dialog v-if="dialogState.typed" ref="dlgTyped" class="pnl w-[440px] max-w-[92vw]" @cancel.prevent="closeTyped(false)">
    <form @submit.prevent="closeTyped(true)">
      <div class="pnl-head">{{ dialogState.typed.title }}</div>
      <div class="pnl-body">
        <p class="mb-2 font-semibold text-err">{{ dialogState.typed.msg }}</p>
        <p class="mb-1">
          {{ t('Type') }} <strong>{{ dialogState.typed.expected }}</strong> {{ t('to confirm:') }}
        </p>
        <input ref="typedInput" v-model="dialogState.typed.value" class="inp" autocomplete="off" />
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn-danger" :disabled="!typedOk">{{ t('Delete') }}</button>
        <button type="button" class="btn-ghost" @click="closeTyped(false)">{{ t('Cancel') }}</button>
      </div>
    </form>
  </dialog>

  <dialog v-if="dialogState.alert" ref="dlgAlert" class="pnl w-fit max-w-[min(640px,92vw)]" @cancel.prevent="closeAlert">
    <form @submit.prevent="closeAlert">
      <div class="pnl-head">{{ dialogState.alert.title }}</div>
      <div class="pnl-body whitespace-pre-line">{{ dialogState.alert.msg }}</div>
      <div class="pnl-foot">
        <button type="submit" class="btn">{{ dialogState.alert.ok }}</button>
      </div>
    </form>
  </dialog>
</template>
