<script setup>
// New share, or edit of a published one (same form, same share:create task). Edits also show the Windows
// permissions of the folder and the earlier ones that can be put back. Each change is queued as a task on the
// server; "queued" hands the task to the page, which follows it.
import { computed, ref, toRef, watch } from 'vue'
import { auth, get, post, enc } from '../../api'
import { t } from '../../i18n'
import { toast } from '../../toasts'
import { askConfirm } from '../../dialogs'
import { formatDate } from '../../util'
import Suggestions from '../Suggestions.vue'
import Modal from '../Modal.vue'
import LoadingNote from '../LoadingNote.vue'

const props = defineProps({
  onQueued: { type: Function, default: null },
  server: Object, // the file server (agent): id, name
  share: Object, // the share being edited; empty for a new one
  roots: Array, // folders where new shares may be created
  groups: Array, // suggestions: DOMAIN\group
  users: Array, // suggestions: DOMAIN\user
})
// the page hears the result through callback props (@queued): an emit after the dialog closed
// (Cancel while the request runs) would be dropped by Vue, and the page would miss it
const emit = defineEmits(['close'])
const busy = ref(false)

const groups = toRef(props, 'groups')
const users = toRef(props, 'users')
const listFields = [
  ['full', 'addFull', 'Groups with full control', 'group', groups],
  ['rw', 'addRw', 'Groups with write access', 'group', groups],
  ['ro', 'addRo', 'Read-only groups', 'group', groups],
  ['fullUsers', 'addFullUser', 'Users with full control', 'user', users],
  ['rwUsers', 'addRwUser', 'Users with write access', 'user', users],
  ['roUsers', 'addRoUser', 'Read-only users', 'user', users],
]
const blank = () => ({ addFull: '', addRw: '', addRo: '', addFullUser: '', addRwUser: '', addRoUser: '' })
// different (case-insensitive) from the full-control names
const notIn = (list, drop) => list.filter((n) => !drop.some((d) => d.toLowerCase() === n.toLowerCase()))
const s = props.share
const form = ref(
  s
    ? {
        ...blank(),
        editing: true,
        name: s.name,
        path: s.path,
        comment: s.comment || '',
        full: [...(s.full_groups || [])],
        rw: notIn(s.rw_groups || [], s.full_groups || []),
        ro: [...(s.ro_groups || [])],
        fullUsers: [...(s.full_users || [])],
        rwUsers: notIn(s.rw_users || [], s.full_users || []),
        roUsers: [...(s.ro_users || [])],
        hidden: !s.browseable,
        recycle: !!s.recycle,
        abe: !!s.abe,
        audit: !!s.audit,
        create_path: true, // re-applies the folder ACLs; the content is untouched
      }
    : {
        ...blank(),
        editing: false,
        name: '',
        root: props.roots[0] || '/srv/shares',
        folder: '',
        comment: '',
        full: [],
        rw: [],
        ro: [],
        fullUsers: [],
        rwUsers: [],
        roUsers: [],
        hidden: false,
        recycle: false,
        abe: false,
        audit: false,
        create_path: true,
      },
)
const path = computed(() => {
  const f = form.value
  return f.editing ? f.path : `${f.root.replace(/\/$/, '')}/${f.folder}`
})
watch(
  () => form.value.name,
  (n, old) => {
    const slug = (s) =>
      (s || '')
        .toLowerCase()
        .normalize('NFD')
        .replace(/[̀-ͯ]/g, '')
        .replace(/[^a-z0-9._-]+/g, '-')
    const f = form.value
    if (!f.editing && (!f.folder || f.folder === slug(old))) f.folder = slug(n)
  },
)
// Enter with no highlighted suggestion: add what was typed (without submitting the form)
function enterItem(ev, list, field) {
  ev.preventDefault()
  addItem(list, field)
}
function addItem(list, field) {
  const v = form.value[field].trim()
  if (v && !form.value[list].includes(v)) form.value[list].push(v)
  form.value[field] = ''
}

// queue a task on the server; the page follows it
async function enqueue(type, payload, label) {
  const task = await post(`/agents/${props.server.id}/tasks`, { type, payload })
  props.onQueued?.(task, label)
}

// Windows permissions of the folder being edited (read through the agent)
const acl = ref(null)
const history = ref([])
async function loadACL(s) {
  acl.value = { loading: true }
  history.value = []
  try {
    acl.value = await get(`/agents/${props.server.id}/acl?path=${enc(s.path)}`)
    // the earlier ACLs that can be put back (each change recorded the one it replaced)
    history.value = (await get(`/agents/${props.server.id}/acl/history?path=${enc(s.path)}`)).filter((h) => h.before)
  } catch (e) {
    if (acl.value?.loading) acl.value = { error: e.message }
  }
}
if (s) loadACL(s)
async function restoreACL(h) {
  const msg = t('Put back the permissions {path} had before {date}, also on the files and subfolders that inherit them?', {
    path: h.path,
    date: formatDate(h.time),
  })
  if (!(await askConfirm(msg, { ok: t('Restore') }))) return
  try {
    await enqueue('acl:restore', { path: h.path, time: h.time }, t('Restore the permissions of {path}', { path: h.path }))
    toast.success(t('Task queued.'))
    emit('close')
  } catch (e) {
    toast.error(e.message)
  }
}
const accessLabel = { full: 'Full control', rw: 'Modify', ro: 'Read & execute', custom: 'Special' }

async function submit() {
  const f = form.value
  const unc = `\\\\${props.server.name}\\${f.name}`
  busy.value = true
  try {
    await enqueue(
      'share:create',
      {
        name: f.name,
        path: path.value,
        comment: f.comment,
        full_groups: f.full,
        rw_groups: f.rw,
        ro_groups: f.ro,
        full_users: f.fullUsers,
        rw_users: f.rwUsers,
        ro_users: f.roUsers,
        hidden: f.hidden,
        recycle: f.recycle,
        abe: f.abe,
        audit: f.audit,
        create_path: f.create_path,
      },
      f.editing ? t('Edit {share}', { share: unc }) : t('Create {share}', { share: unc }),
    )
    toast.success(t('Task queued.'))
    emit('close')
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}
</script>

<template>
  <Modal
    :title="
      form.editing
        ? t('Edit share {name} on {server}', { name: form.name, server: server.name })
        : t('New share on {server}', { server: server.name })
    "
    wide
    :busy="busy"
    :ok="form.editing ? t('Save') : t('Create')"
    @close="emit('close')"
    @submit="submit"
  >
    <div class="grid grid-cols-2 gap-x-4 gap-y-3">
      <div>
        <label class="lbl" for="sn">{{ t('Share name') }}</label
        ><input id="sn" v-model.trim="form.name" class="inp" maxlength="80" required :readonly="form.editing" :autofocus="!form.editing" />
      </div>
      <div>
        <label class="lbl" for="sc">{{ t('Comment') }}</label
        ><input id="sc" v-model="form.comment" class="inp" maxlength="200" />
      </div>
      <div class="col-span-2">
        <label class="lbl" for="sp">{{ t('Folder') }}</label>
        <input v-if="form.editing" id="sp" :value="form.path" class="inp font-mono" readonly />
        <template v-else>
          <div class="flex">
            <select v-model="form.root" class="inp w-56 shrink-0" :aria-label="t('Root')">
              <option v-for="r in roots" :key="r" :value="r">{{ r }}/</option>
            </select>
            <input id="sp" v-model.trim="form.folder" class="inp font-mono" required />
          </div>
          <p class="mt-1 font-mono text-[11px] text-fg-muted">{{ path }}</p>
        </template>
      </div>
      <div v-for="[list, field, title, kind, options] in listFields" :key="list">
        <span class="lbl">{{ t(title) }}</span>
        <div class="flex">
          <Suggestions
            v-model="form[field]"
            :options="options.value"
            :placeholder="`${auth.netbios}\\${t(kind)}`"
            @pick="addItem(list, field)"
            @enter="(ev) => enterItem(ev, list, field)"
          />
          <button type="button" class="btn-ghost" @click="addItem(list, field)">+</button>
        </div>
        <div class="mt-1 min-h-6">
          <span
            v-for="g in form[list]"
            :key="g"
            class="mr-1 mb-1 inline-flex items-center gap-1 border border-line bg-surface-3 px-2 py-0.5 text-[11px]"
          >
            {{ g }}
            <button
              type="button"
              class="act px-0"
              :aria-label="t('remove {name}', { name: g })"
              @click="form[list] = form[list].filter((x) => x !== g)"
            >
              ✕
            </button>
          </span>
        </div>
      </div>
      <div v-if="form.editing && acl" class="border border-line bg-surface-3 px-3 py-2 text-[12px]">
        <span class="lbl">{{ t('Windows permissions of the folder') }}</span>
        <LoadingNote v-if="acl.loading" />
        <p v-else-if="acl.error" class="text-fg-muted">{{ acl.error }}</p>
        <table v-else class="w-full">
          <tbody>
            <tr v-for="a in acl.aces" :key="a.sid + a.mask + a.flags">
              <td class="truncate py-0.5" :title="a.sid">{{ a.name || a.sid }}</td>
              <td class="py-0.5 text-right whitespace-nowrap" :class="a.type === 'deny' ? 'text-err' : ''">
                {{ a.type === 'deny' ? t('Deny') + ' · ' : '' }}{{ t(accessLabel[a.access] || 'Special') }}
              </td>
            </tr>
          </tbody>
        </table>
        <p class="mt-1 text-[11px] text-fg-muted">{{ t('Saving re-applies these permissions from the lists above.') }}</p>
        <template v-if="history.length">
          <span class="lbl mt-2 block">{{ t('Earlier permissions') }}</span>
          <table class="w-full">
            <tbody>
              <tr v-for="h in history" :key="h.time">
                <td class="py-0.5" :title="h.before">{{ t('Before {date}', { date: formatDate(h.time) }) }}</td>
                <td class="py-0.5 text-right">
                  <button type="button" class="act" @click="restoreACL(h)">{{ t('Restore') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </template>
      </div>
      <label v-if="!form.editing" class="flex items-center gap-2"
        ><input v-model="form.create_path" type="checkbox" />
        {{ t('Create the folder (owner group = 1st with write access, inherited ACL)') }}</label
      >
      <label class="flex items-center gap-2"
        ><input v-model="form.hidden" type="checkbox" /> {{ t('Hidden on the network (not shown when browsing)') }}</label
      >
      <label class="flex items-center gap-2"
        ><input v-model="form.recycle" type="checkbox" /> {{ t('Recycle bin (deleted files go to .recycle, per user)') }}</label
      >
      <label class="flex items-center gap-2"
        ><input v-model="form.abe" type="checkbox" /> {{ t('Hide what the user cannot open (access-based enumeration)') }}</label
      >
      <label class="flex items-center gap-2"
        ><input v-model="form.audit" type="checkbox" />
        {{ t('Audit file changes (renamed and deleted files, new folders → syslog)') }}</label
      >
      <p v-if="form.editing" class="col-span-2 text-[11px] text-fg-muted">
        {{
          t(
            'Saving republishes the share and reapplies the folder permissions, also on the files and subfolders that inherit them; their content is not changed.',
          )
        }}
      </p>
    </div>
  </Modal>
</template>
