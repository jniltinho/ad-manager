<script setup>
// Form dialog in the .pnl style: opens with showModal() on mount (fading in like the pages); Esc, ✕ and Cancel
// fade it out and then emit "close"; submitting emits "submit".
import { onMounted, ref } from 'vue'
import { Check } from 'lucide-vue-next'
import { t } from '../i18n'

// foot: false leaves the buttons to the content (a wizard with its own bar per step); ✕ and Esc still close
defineProps({ title: String, wide: Boolean, busy: Boolean, ok: { type: String, default: '' }, foot: { type: Boolean, default: true } })
const emit = defineEmits(['close', 'submit'])
const dlg = ref(null)
onMounted(() => dlg.value.showModal())
// close fades the dialog out first (the .closing animation), unless the system asks for less motion
function close() {
  const el = dlg.value
  if (!el || el.classList.contains('closing')) return
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) return emit('close')
  el.classList.add('closing')
  setTimeout(() => emit('close'), 150)
}
</script>

<template>
  <dialog ref="dlg" class="pnl" :class="wide ? 'w-[760px] max-w-[94vw]' : 'w-[460px] max-w-[94vw]'" @cancel.prevent="close">
    <form @submit.prevent="emit('submit')">
      <div class="pnl-head flex items-center justify-between">
        <span>{{ title }}</span>
        <button
          type="button"
          class="cursor-pointer px-1 text-on-dialog-head/70 hover:text-on-dialog-head"
          :aria-label="t('Close')"
          @click="close"
        >
          ✕
        </button>
      </div>
      <div class="pnl-body max-h-[calc(var(--view-h)*0.72)] overflow-y-auto"><slot /></div>
      <div v-if="foot" class="pnl-foot">
        <slot name="foot" />
        <button v-if="ok !== null" type="submit" class="btn" :disabled="busy">
          <Check :size="14" /> {{ busy ? t('Please wait…') : ok || t('Save') }}
        </button>
        <button type="button" class="btn-ghost" @click="close">{{ ok === null ? t('Close') : t('Cancel') }}</button>
      </div>
    </form>
  </dialog>
</template>
