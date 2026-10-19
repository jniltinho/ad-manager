<script setup>
// Port of the earlier panel's Toasts.vue. The stack is a manual popover: it lives in the top layer, so a toast shows
// above an open modal <dialog> and its backdrop; it is shown again on every new toast to stay the topmost layer.
import { onMounted, ref, watch } from 'vue'
import { toastState, closeToast } from '../toasts'

const stack = ref(null)
const raise = () => {
  if (!stack.value?.showPopover) return
  if (stack.value.matches(':popover-open')) stack.value.hidePopover()
  stack.value.showPopover()
}
onMounted(raise)
watch(
  () => toastState.items.length,
  (n, old) => n > old && raise(),
)
</script>

<template>
  <div id="toasts" ref="stack" popover="manual" aria-live="polite">
    <div v-for="it in toastState.items" :key="it.id" class="toast" :class="[it.kind, { show: it.show }]" @click="closeToast(it.id)">
      <div class="toastHead">{{ it.head }}</div>
      <div class="toastBody">{{ it.msg }}</div>
    </div>
  </div>
</template>
