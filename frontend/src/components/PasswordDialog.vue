<script setup>
// Password reset (ADUC style): generator, copy, "change at next logon" checked
// and "unlock too" when the account is locked. The "Send link" tab generates a
// single-use link for the user to choose the new password.
import { ref } from 'vue'
import { post, enc } from '../api'
import { toast } from '../toasts'
import { formatDate, generatePassword } from '../util'
import { t } from '../i18n'
import Modal from './Modal.vue'

const props = defineProps({ user: Object })
const emit = defineEmits(['close', 'done'])
const mode = ref('set')
const password = ref(generatePassword())
const visible = ref(true)
const mustChange = ref(true)
const unlock = ref(props.user.locked)
const busy = ref(false)

const validity = [
  [1, '1 hour'],
  [24, '24 hours'],
  [72, '3 days'],
  [168, '7 days'],
]
const hours = ref(24)
const link = ref(null) // { url, expires }

async function copy(text, msg) {
  await navigator.clipboard?.writeText(text)
  toast.success(t(msg))
}

async function save() {
  if (link.value) return emit('done')
  busy.value = true
  const sam = enc(props.user.sam)
  try {
    if (mode.value === 'link') {
      const r = await post(`/users/${sam}/password-link`, { hours: hours.value })
      link.value = { url: `${location.origin}/password/${r.token}`, expires: r.expires }
      busy.value = false
      return
    }
    await post(`/users/${sam}/password`, { password: password.value, must_change: mustChange.value })
    if (unlock.value) await post(`/users/${sam}/unlock`)
    toast.success(t('Password of {user} reset.', { user: props.user.sam }))
    emit('done')
  } catch (e) {
    toast.error(e.message)
    busy.value = false
  }
}
</script>

<template>
  <Modal
    :title="`${t('Reset password')} — ${user.name} (${user.sam})`"
    :busy="busy"
    :ok="link ? t('Close') : mode === 'link' ? t('Generate link') : t('Reset')"
    @close="link ? emit('done') : emit('close')"
    @submit="save"
  >
    <div class="cardtabs fit -mx-4 -mt-3 mb-3">
      <button
        v-for="[m, label] in [
          ['set', 'Set password'],
          ['link', 'Send link to the user'],
        ]"
        :key="m"
        type="button"
        class="cardtab"
        :class="{ on: mode === m }"
        :disabled="!!link"
        @click="mode = m"
      >
        {{ t(label) }}
      </button>
    </div>

    <template v-if="mode === 'set'">
      <label class="lbl" for="np">{{ t('New password') }}</label>
      <div class="flex">
        <input
          id="np"
          v-model="password"
          :type="visible ? 'text' : 'password'"
          class="inp font-mono"
          autocomplete="new-password"
          required
        />
        <button type="button" class="btn-ghost" @click="visible = !visible">{{ visible ? t('hide') : t('show') }}</button>
        <button type="button" class="btn-ghost" @click="password = generatePassword()">{{ t('generate') }}</button>
        <button type="button" class="btn-ghost" @click="copy(password, 'Password copied.')">{{ t('copy') }}</button>
      </div>
      <label class="mt-3 flex items-center gap-2"
        ><input v-model="mustChange" type="checkbox" /> {{ t('Require change at next logon') }}</label
      >
      <label class="mt-1 flex items-center gap-2" :class="user.locked ? '' : 'text-fg-subtle'">
        <input v-model="unlock" type="checkbox" :disabled="!user.locked" /> {{ t('Unlock the account too') }}
      </label>
    </template>

    <template v-else-if="!link">
      <label class="lbl" for="lv">{{ t('Valid for') }}</label>
      <select id="lv" v-model="hours" class="inp w-40">
        <option v-for="[h, label] in validity" :key="h" :value="h">{{ t(label) }}</option>
      </select>
      <p class="mt-3 border-l-4 border-accent bg-surface-2 px-3 py-2 text-[12px] leading-relaxed text-fg-muted">
        {{ t('The current password stops working now and the account is unlocked. The user sets the new one through the link.') }}
        {{ t('Only the first browser that opens the link can use it, within 30 minutes; it works once.') }}
      </p>
    </template>

    <template v-else>
      <label class="lbl" for="lu">{{ t('Link for {user}', { user: user.sam }) }}</label>
      <div class="flex">
        <input id="lu" :value="link.url" class="inp font-mono text-[12px]" readonly @focus="$event.target.select()" />
        <button type="button" class="btn-ghost" @click="copy(link.url, 'Link copied.')">{{ t('copy') }}</button>
      </div>
      <p class="mt-2 text-[12px] text-fg-muted">
        {{ t('Valid until {date}. Send it to the user through a trusted channel.', { date: formatDate(link.expires) }) }}
      </p>
    </template>
  </Modal>
</template>
