<script setup>
// A table row with the loading spinner, shown until the first reply instead of an empty table; screen readers hear
// "Loading…" once (role="status").
import LoadingNote from './LoadingNote.vue'

defineProps({ cols: { type: Number, required: true } })
</script>

<template>
  <tr>
    <td :colspan="cols" class="py-6"><LoadingNote class="justify-center" /></td>
  </tr>
</template>
