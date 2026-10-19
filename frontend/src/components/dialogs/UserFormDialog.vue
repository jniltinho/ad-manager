<script setup>
// Create or edit a user. "new" (blank or copied from a template user) asks for the login, OU, initial password and
// the template's groups; "edit" has an Account tab with the details and a Groups tab whose changes apply at once.
// Emits "done" after creating or saving the details.
import { ref } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import { post, put, del, enc } from '../../api'
import { toast } from '../../toasts'
import { formatDate, generatePassword } from '../../util'
import { t } from '../../i18n'
import Modal from '../Modal.vue'
import Suggestions from '../Suggestions.vue'

// initial: the form seed built by the page (mode "new" or "edit"); ous: OU list; groups: every group name
const props = defineProps({ initial: Object, ous: Array, groups: Array, onDone: { type: Function, default: null } })
// the page hears the result through callback props (@done): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])
const form = ref({ ...props.initial })
const busy = ref(false)

const fields = [
  ['name', 'Display name', true],
  ['given_name', 'First name'],
  ['surname', 'Last name'],
  ['mail', 'E-mail'],
  ['phone', 'Phone'],
  ['title', 'Job title'],
  ['department', 'Department'],
  ['description', 'Description'],
]

async function save() {
  const f = form.value
  busy.value = true
  try {
    if (f.mode === 'new') {
      const u = await post('/users', {
        sam: f.sam,
        name: f.name,
        given_name: f.given_name,
        surname: f.surname,
        mail: f.mail,
        phone: f.phone,
        title: f.title,
        department: f.department,
        description: f.description,
        ou: f.ou,
        password: f.password,
        must_change: f.must_change,
      })
      for (const g of f.groups) await post(`/groups/${enc(g)}/members`, { member: u.sam }).catch((e) => toast.warn(`${g}: ${e.message}`))
      toast.success(t('User {user} created.', { user: u.sam }))
    } else {
      await put(`/users/${enc(f.sam)}`, Object.fromEntries(fields.map(([k]) => [k, f[k] || ''])))
      toast.success(t('Details saved.'))
    }
    props.onDone?.()
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}

async function member(g, add) {
  const f = form.value
  try {
    if (add) await post(`/groups/${enc(g)}/members`, { member: f.sam })
    else await del(`/groups/${enc(g)}/members/${enc(f.sam)}`)
    f.groups = add ? [...f.groups, g].sort() : f.groups.filter((x) => x !== g)
    f.newGroup = ''
    toast.success(t(add ? 'Added to {group}.' : 'Removed from {group}.', { group: g }))
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <Modal
    :title="form.heading"
    wide
    :busy="busy"
    :ok="form.mode === 'new' ? t('Create') : form.tab === 'account' ? t('Save') : ''"
    @close="emit('close')"
    @submit="save"
  >
    <div v-if="form.mode === 'edit'" class="cardtabs fit -mx-4 -mt-3 mb-3">
      <button
        v-for="[k, label] in [
          ['account', t('Account')],
          ['groups', t('Groups ({n})', { n: form.groups.length })],
        ]"
        :key="k"
        type="button"
        class="cardtab"
        :class="{ on: form.tab === k }"
        @click="form.tab = k"
      >
        {{ label }}
      </button>
    </div>

    <div v-if="form.mode === 'new' || form.tab === 'account'" class="grid grid-cols-2 gap-x-4 gap-y-3">
      <div v-if="form.mode === 'new'">
        <label class="lbl" for="sam">{{ t('Login (sAMAccountName)') }}</label>
        <input id="sam" v-model.trim="form.sam" class="inp" maxlength="20" required autofocus />
      </div>
      <div v-else>
        <span class="lbl">{{ t('Login') }}</span>
        <div class="py-1 font-mono">{{ form.sam }} · {{ form.upn }}</div>
      </div>
      <div v-for="[k, label, req] in fields" :key="k">
        <label class="lbl" :for="k">{{ t(label) }}</label>
        <input :id="k" v-model="form[k]" class="inp" :type="k === 'mail' ? 'email' : 'text'" :required="req" />
      </div>
      <template v-if="form.mode === 'new'">
        <div class="col-span-2">
          <label class="lbl" for="ou">{{ t('OU') }}</label>
          <select id="ou" v-model="form.ou" class="inp">
            <option v-for="o in ous" :key="o.dn" :value="o.dn">{{ o.dn }}</option>
          </select>
        </div>
        <div class="col-span-2">
          <label class="lbl" for="pw">{{ t('Initial password') }}</label>
          <div class="flex">
            <input id="pw" v-model="form.password" class="inp font-mono" required autocomplete="new-password" />
            <button type="button" class="btn-ghost" @click="form.password = generatePassword()">{{ t('generate') }}</button>
          </div>
          <label class="mt-2 flex items-center gap-2"
            ><input v-model="form.must_change" type="checkbox" /> {{ t('Require change at next logon') }}</label
          >
        </div>
        <div v-if="form.groups.length" class="col-span-2">
          <span class="lbl">{{ t('Groups (from the template)') }}</span>
          <span
            v-for="g in form.groups"
            :key="g"
            class="mr-1 mb-1 inline-flex items-center gap-1 border border-line bg-surface-3 px-2 py-0.5 text-[11px]"
          >
            {{ g }}
            <button
              type="button"
              class="act px-0"
              :aria-label="t('remove {group}', { group: g })"
              @click="form.groups = form.groups.filter((x) => x !== g)"
            >
              ✕
            </button>
          </span>
        </div>
      </template>
      <div v-else class="col-span-2 grid grid-cols-3 gap-2 border-t border-line pt-3 text-[12px] text-fg-muted">
        <div>{{ t('Created: {date}', { date: formatDate(form.created) }) }}</div>
        <div>{{ t('Last logon: {date}', { date: formatDate(form.last_logon) }) }}</div>
        <div>{{ t('Password changed: {date}', { date: formatDate(form.pwd_last_set) }) }}</div>
        <div class="col-span-3 font-mono text-[11px] break-all">{{ form.dn }} · {{ form.sid }}</div>
      </div>
    </div>

    <div v-else>
      <div class="mb-3 flex gap-2">
        <Suggestions
          v-model="form.newGroup"
          :options="groups.filter((x) => !form.groups.includes(x))"
          :placeholder="t('Add to a group…')"
          :aria-label="t('Group')"
        />
        <button type="button" class="btn" :disabled="!form.newGroup" @click="member(form.newGroup, true)">{{ t('Add') }}</button>
      </div>
      <table class="grid-t">
        <thead>
          <tr>
            <th>{{ t('Group') }}</th>
            <th class="text-right">{{ t('Action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in form.groups" :key="g">
            <td>{{ g }}</td>
            <td class="text-right">
              <button type="button" class="act" @click="member(g, false)"><Trash2 :size="12" /> {{ t('remove') }}</button>
            </td>
          </tr>
          <tr v-if="!form.groups.length">
            <td colspan="2" class="text-fg-muted">{{ t('Only the primary group (Domain Users).') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </Modal>
</template>
