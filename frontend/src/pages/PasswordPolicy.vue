<script setup>
import { Save } from 'lucide-vue-next'
import { ref } from 'vue'
import { get, put } from '../api'
import { t } from '../i18n'
import { toast } from '../toasts'
import { askConfirm } from '../dialogs'

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
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <form v-if="p" class="max-w-xl" @submit.prevent="save">
    <section class="card">
      <div class="card-h">{{ t("Domain's default password policy") }}</div>
      <div class="space-y-3 p-4">
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
      <div class="pnl-foot">
        <button class="btn" :disabled="busy"><Save :size="13" /> {{ t('Save policy') }}</button>
      </div>
    </section>
  </form>
</template>
