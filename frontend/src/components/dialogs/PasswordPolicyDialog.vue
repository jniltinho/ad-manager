<script setup>
// The domain's default password policy, opened from Users; saving asks for confirmation and closes the dialog.
import { ref } from 'vue'
import { get, put } from '../../api'
import { t } from '../../i18n'
import { toast } from '../../toasts'
import { askConfirm } from '../../dialogs'
import Modal from '../Modal.vue'

const emit = defineEmits(['close'])
const p = ref(null)
const busy = ref(false)
get('/policy')
  .then((r) => (p.value = r))
  .catch((e) => toast.error(e.message))

const fields = [
  ['min_length', 'Minimum length', 'characters', 0, 255],
  ['history', 'History', 'passwords remembered (0-24)', 0, 24],
  ['min_age_days', 'Minimum age', 'days', 0, 998],
  ['max_age_days', 'Maximum age', 'days (0 = never expires)', 0, 999],
  ['lockout_threshold', 'Lock out after', 'attempts (0 = never)', 0, 999],
  ['lockout_minutes', 'Lockout duration', 'minutes', 0, 99999],
  ['lockout_window_minutes', 'Counting window', 'minutes', 0, 99999],
]

async function save() {
  if (
    !p.value ||
    !(await askConfirm(
      t(
        "Change the domain's password policy? It applies to the next password changes.\nWhile there is a Windows DC, the Default Domain Policy may reapply the old values.",
      ),
      { ok: t('Change policy') },
    ))
  )
    return
  busy.value = true
  try {
    await put('/policy', p.value)
    toast.success(t('Policy updated.'))
    emit('close')
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <Modal
    :title="t('Domain\'s default password policy')"
    wide
    :busy="busy || !p"
    :locked="busy"
    :ok="t('Save policy')"
    @close="emit('close')"
    @submit="save"
  >
    <div v-if="p" class="space-y-3">
      <label class="flex items-center gap-2 font-semibold"
        ><input v-model="p.complexity" type="checkbox" /> {{ t('Require complexity (uppercase, lowercase, digit/symbol)') }}</label
      >
      <div v-for="[k, label, unit, min, max] in fields" :key="k" class="grid grid-cols-[180px_100px_1fr] items-center gap-3">
        <label class="lbl mb-0" :for="k">{{ t(label) }}</label>
        <input :id="k" v-model.number="p[k]" type="number" :min="min" :max="max" class="inp" required />
        <span class="text-[12px] text-fg-muted">{{ t(unit) }}</span>
      </div>
      <p v-if="!p.complexity || p.min_length < 8" class="border-l-4 border-warn bg-warn-soft px-3 py-2 text-[12px]">
        {{ t('Recommended: complexity on, at least 8 characters and lockout after 10 attempts — after the cutover to Samba.') }}
      </p>
    </div>
    <p v-else class="text-fg-muted">{{ t('Loading…') }}</p>
  </Modal>
</template>
