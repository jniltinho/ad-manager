<script setup>
// Export/import OUs, groups, users, computers and the file servers' folders and
// permissions as CSV (Excel style: ";" + BOM). Import only creates and updates:
// it never deletes nor removes members; folders are created empty (no files).
import { computed, nextTick, ref, watch } from 'vue'
import { toast } from '../toasts'
import { auth, del, get, post } from '../api'
import { serverText, t } from '../i18n'
import { DatabaseZap, Download, Eye, FolderOpen, KeyRound, PlugZap, Trash2 } from 'lucide-vue-next'
import { askConfirm } from '../dialogs'
import { downloadCsv } from '../util'

const mode = ref('csv') // csv | ad | folders
const files = ref({ ous: null, groups: null, users: null, computers: null })
const exportables = [
  ['ous', 'OUs'],
  ['groups', 'Groups'],
  ['users', 'Users'],
  ['computers', 'Computers'],
]
// CSV import fields and the file names operators know them by
const importables = [
  ['ous', 'ous.csv'],
  ['groups', 'groups.csv'],
  ['users', 'users.csv'],
  ['computers', 'computers.csv'],
]
// file servers (agents) for folders and permissions
const servers = ref([])
const server = ref('')
const foldersFile = ref(null)
get('/agents')
  .then((r) => {
    servers.value = r
    if (r.length) server.value = shareSrc.value.agent = r[0].id
  })
  .catch(() => {})
const source = ref({ url: 'ldap://', domain: '', user: '', password: '', insecure: true, save_as: '' })
// what "From another AD" brings: objects (users, groups, OUs, computers), GPOs, the shares of one file server
const what = ref({ objects: true, gpos: false, dns: false, dynamic: false, zones: false, shares: false, passwords: false })
const shareSrc = ref({ host: '', agent: null, map: 'E:\\=/srv/shares' })
// "· source: 24 users, 57 groups, …" with what was read
const sourceSummary = computed(() => {
  const c = sourceCounts.value || {}
  const parts = [
    ['users', '{n} users'],
    ['groups', '{n} groups'],
    ['ous', '{n} OUs'],
    ['computers', '{n} computers'],
    ['gpos', '{n} GPOs'],
    ['shares', '{n} shares'],
    ['dns', '{n} DNS records'],
  ]
    .filter(([k]) => c[k] != null)
    .map(([k, label]) => t(label, { n: c[k] }))
  return '· ' + t('source:') + ' ' + parts.join(', ')
})
function adRequest() {
  const body = {
    ...source.value,
    objects: what.value.objects,
    gpos: what.value.gpos,
    dns: what.value.dns,
    include_dynamic: what.value.dynamic,
    create_zones: what.value.zones,
    passwords: what.value.passwords,
  }
  if (what.value.shares) body.shares = { ...shareSrc.value, agent: shareSrc.value.agent || server.value }
  return body
}
const profiles = ref([])
const sourceCounts = ref(null)
get('/ad-sources')
  .then((r) => (profiles.value = r))
  .catch(() => {})
function switchMode(k) {
  mode.value = k
  plan.value = null
}
function useProfile(p) {
  Object.assign(source.value, { url: p.url, domain: p.domain, user: p.user, insecure: p.insecure, save_as: p.name, password: '' })
}
async function removeProfile(p) {
  if (!(await askConfirm(t('Delete the profile {name}?', { name: p.name })))) return
  try {
    await del('/ad-sources/' + p.id)
    profiles.value = profiles.value.filter((x) => x.id !== p.id)
  } catch (e) {
    toast.error(e.message)
  }
}
async function sendAD(apply) {
  busy.value = true
  if (apply) logStart(t('From another AD'), t('Importing the objects…'))
  try {
    const data = await post('/import/ad/' + (apply ? 'apply' : 'plan'), adRequest())
    plan.value = data.plan || []
    sourceCounts.value = data.counts
    applied.value = apply
    passwords.value = data.passwords && Object.keys(data.passwords).length ? data.passwords : null
    if (source.value.save_as) get('/ad-sources').then((r) => (profiles.value = r))
    logFromPlan(plan.value, apply)
    if (apply) toast.success(t('Import finished.'))
  } catch (e) {
    logError(e.message)
    toast.error(e.message)
  }
  busy.value = false
}
const plan = ref(null)
const applied = ref(false)
const passwords = ref(null)
const busy = ref(false)
const kind = ref('')

// Live log/status modal for a running import (CSV, From another AD, Folders): a phase while the request runs, then
// the per-action result and the final status. The request returns the whole plan at once, so the detailed log
// appears when it finishes; the phase shows the stage while it is in flight.
const syncLog = ref(null) // { title, phase, status: 'running'|'done'|'error', steps:[{label,target,detail,ok}], ok, errors, error }
const dlgLog = ref(null)
watch(syncLog, async (v) => {
  if (v) {
    await nextTick()
    if (!dlgLog.value?.open) dlgLog.value?.showModal()
  }
})
function logStart(title, phase) {
  syncLog.value = { title, phase, status: 'running', steps: [], ok: 0, errors: 0, error: '' }
}
function logPhase(phase) {
  if (syncLog.value) syncLog.value.phase = phase
}
function logFromPlan(planArr, applied) {
  if (!syncLog.value) return
  const steps = (planArr || [])
    .filter((a) => a.kind !== 'skip')
    // password rows carry "outcome — reason" in detail; keep it (and the reason) so the modal/tooltip explain why
    .map((a) => ({
      label: kindName(a.kind),
      target: a.target,
      detail: a.kind === 'password' || !applied ? a.detail : a.result,
      ok: !applied || a.ok,
    }))
  const errors = applied ? steps.filter((s) => !s.ok).length : 0
  Object.assign(syncLog.value, { steps, status: errors ? 'error' : 'done', ok: steps.length - errors, errors, phase: '' })
}
function logError(msg) {
  if (syncLog.value) Object.assign(syncLog.value, { status: 'error', error: msg, phase: '' })
}
function logClose() {
  dlgLog.value?.close()
  syncLog.value = null
}

const kindNames = {
  'ou.create': 'Create OU',
  'group.create': 'Create group',
  'group.update': 'Update group',
  'user.create': 'Create user',
  'user.update': 'Update user',
  'user.disable': 'Disable user',
  'computer.create': 'Create computer',
  'share.create': 'Create share',
  'share.update': 'Update share',
  'gpo.import': 'Import GPO',
  password: 'Password',
  'dns.add': 'Add DNS record',
  'dns.conflict': 'DNS conflict',
  'zone.missing': 'Missing zone',
  'member.add': 'Add member',
  skip: 'Skipped',
}
const kindName = (k) => (kindNames[k] ? t(kindNames[k]) : k)
const counts = computed(() => (plan.value || []).reduce((m, a) => ((m[a.kind] = (m[a.kind] || 0) + 1), m), {}))
const visible = computed(() => (plan.value || []).filter((a) => !kind.value || a.kind === kind.value))
const pending = computed(() => (plan.value || []).filter((a) => a.kind !== 'skip').length)

function pickFile(field, ev) {
  files.value[field] = ev.target.files[0] || null
  plan.value = null
  applied.value = false
}

async function send(apply) {
  const fd = new FormData()
  for (const [k, f] of Object.entries(files.value)) if (f) fd.append(k, f)
  busy.value = true
  if (apply) logStart(t('Import CSV'), t('Writing to the domain…'))
  try {
    const r = await fetch('/api/import/' + (apply ? 'apply' : 'plan'), { method: 'POST', body: fd, credentials: 'same-origin' })
    if (r.status === 401) auth.user = null
    const data = await r.json().catch(() => ({}))
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`)
    plan.value = data.plan || []
    applied.value = apply
    passwords.value = data.passwords && Object.keys(data.passwords).length ? data.passwords : null
    logFromPlan(plan.value, apply)
    if (apply) {
      const errors = plan.value.filter((a) => a.kind !== 'skip' && !a.ok).length
      if (errors) toast.warn(t('Import finished with {n} error(s).', { n: errors }))
      else toast.success(t('Import finished.'))
    }
  } catch (e) {
    logError(e.message)
    toast.error(e.message)
  }
  busy.value = false
}

function pickFoldersFile(ev) {
  foldersFile.value = ev.target.files[0] || null
  plan.value = null
}
async function sendFolders(apply) {
  const fd = new FormData()
  fd.append('shares', foldersFile.value)
  busy.value = true
  if (apply) logStart(t('Folders'), t('Sending to the server…'))
  try {
    const r = await fetch(`/api/agents/${server.value}/import/${apply ? 'apply' : 'plan'}`, {
      method: 'POST',
      body: fd,
      credentials: 'same-origin',
    })
    if (r.status === 401) auth.user = null
    const data = await r.json().catch(() => ({}))
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`)
    plan.value = data.plan || []
    applied.value = apply
    passwords.value = null
    logFromPlan(plan.value, apply)
    if (apply) toast.success(t("Shares sent to the server's queue."))
  } catch (e) {
    logError(e.message)
    toast.error(e.message)
  }
  busy.value = false
}

async function apply() {
  if (mode.value === 'folders') {
    const name = servers.value.find((x) => x.id === server.value)?.name
    if (
      await askConfirm(
        t('Create/update {n} share(s) on {server}?\nFolders are created empty, with the permissions only.', {
          n: pending.value,
          server: name,
        }),
        { ok: t('Send') },
      )
    )
      sendFolders(true)
    return
  }
  if (
    !(await askConfirm(
      t('Write {n} change(s) to the domain?\nNew accounts get a generated password and must change it at first logon.', {
        n: pending.value,
      }),
      { ok: t('Apply') },
    ))
  )
    return
  if (mode.value === 'ad') sendAD(true)
  else send(true)
}

function downloadPasswords() {
  downloadCsv(
    t('generated-passwords.csv'),
    Object.entries(passwords.value).map(([login, password]) => ({ login, password })),
    [
      ['login', 'login'],
      [t('initial_password'), 'password'],
    ],
  )
}
</script>

<template>
  <!-- from lg up it fills <main>: only the plan table scrolls (the left column only on a short window); on a narrow
       window the two cards stack and the page scrolls -->
  <div class="grid gap-5 lg:h-full lg:min-h-0 lg:grid-cols-[360px_1fr] lg:grid-rows-[minmax(0,1fr)]">
    <div class="space-y-5 lg:min-h-0 lg:overflow-y-auto">
      <section class="card">
        <div class="card-h">{{ t('Export from the current domain') }}</div>
        <div class="space-y-2 p-3 text-[12px]">
          <div class="grid grid-cols-2 gap-2">
            <a
              v-for="[f, label] in exportables"
              :key="f"
              :href="`/api/export/${f}`"
              class="btn-ghost w-full justify-between"
              :title="f + '.csv'"
              download
            >
              {{ t(label) }} <Download :size="13" />
            </a>
          </div>
          <div v-if="servers.length" class="flex gap-1 border-t border-line pt-2">
            <select v-model="server" class="inp" :aria-label="t('File server')">
              <option v-for="a in servers" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
            <a :href="`/api/agents/${server}/export`" class="btn-ghost shrink-0" download :title="t('Folders and permissions (no files)')">
              <FolderOpen :size="13" /> {{ t('Folders') }} <Download :size="13" />
            </a>
          </div>
          <p class="text-fg-muted">
            {{ t('No passwords or files. From the CLI:') }} <code>ad-manager export -d {{ t('folder') }}</code>
            {{ t('(also from the 2008 AD) and') }} <code>ad-manager migrate shares --csv</code>
            {{ t('(folders and permissions of a Windows server).') }}
          </p>
        </div>
      </section>
      <section class="card">
        <div class="cardtabs">
          <button
            type="button"
            v-for="[k, label] in [
              ['csv', 'Import CSV'],
              ['ad', 'From another AD'],
              ['folders', 'Folders'],
            ]"
            :key="k"
            class="cardtab whitespace-nowrap"
            :class="{ on: mode === k }"
            @click="switchMode(k)"
          >
            {{ t(label) }}
          </button>
        </div>
        <div v-if="mode === 'csv'" class="space-y-3 p-3">
          <div v-for="[f, fileName] in importables" :key="f">
            <label class="lbl" :for="'f-' + f">{{ fileName }}</label>
            <input
              :id="'f-' + f"
              type="file"
              accept=".csv,text/csv"
              class="block w-full text-[12px] file:mr-2 file:h-7 file:cursor-pointer file:border file:border-line file:bg-surface file:px-3 file:text-xs file:text-fg-strong"
              @change="pickFile(f, $event)"
            />
          </div>
          <button class="btn w-full justify-center" :disabled="busy || !Object.values(files).some(Boolean)" @click="send(false)">
            <Eye :size="14" /> {{ busy && !plan ? t('Analyzing…') : t('Preview') }}
          </button>
          <p class="text-[11px] text-fg-muted">
            {{ t('Only creates and updates — never deletes objects nor removes members. Key: login / group name / OU path.') }}
          </p>
        </div>
        <div v-else-if="mode === 'folders'" class="space-y-3 p-3">
          <p v-if="!servers.length" class="text-[12px] text-fg-muted">
            {{ t('Register a file server under') }} <router-link to="/shares" class="underline">{{ t('Shares') }}</router-link
            >.
          </p>
          <template v-else>
            <div>
              <label class="lbl" for="srv">{{ t('Target server') }}</label>
              <select id="srv" v-model="server" class="inp" @change="plan = null">
                <option v-for="a in servers" :key="a.id" :value="a.id">{{ a.name }}</option>
              </select>
            </div>
            <div>
              <label class="lbl" for="f-folders">shares.csv</label>
              <input
                id="f-folders"
                type="file"
                accept=".csv,text/csv"
                class="block w-full text-[12px] file:mr-2 file:h-7 file:cursor-pointer file:border file:border-line file:bg-surface file:px-3 file:text-xs file:text-fg-strong"
                @change="pickFoldersFile"
              />
            </div>
            <button class="btn w-full justify-center" :disabled="busy || !foldersFile" @click="sendFolders(false)">
              <Eye :size="14" /> {{ busy && !plan ? t('Analyzing…') : t('Preview') }}
            </button>
            <p class="text-[11px] text-fg-muted">
              {{
                t(
                  'Structure and permissions only: each share becomes an empty folder with the ACLs of the groups and users, published on Samba by the agent. It does not copy files and never removes shares.',
                )
              }}
            </p>
          </template>
        </div>
        <form v-else class="space-y-3 p-3" @submit.prevent="sendAD(false)">
          <div v-if="profiles.length">
            <span class="lbl">{{ t('Saved profiles') }}</span>
            <span
              v-for="p in profiles"
              :key="p.id"
              class="tag mr-1 inline-flex items-center gap-1 border border-line bg-surface-3 text-fg-strong"
            >
              <button type="button" class="cursor-pointer" @click="useProfile(p)">{{ p.name }}</button>
              <button
                type="button"
                class="cursor-pointer text-fg-muted hover:text-danger"
                :title="t('Delete the profile {name}?', { name: p.name })"
                @click="removeProfile(p)"
              >
                <Trash2 class="h-3.5 w-3.5" />
              </button>
            </span>
          </div>
          <div>
            <label class="lbl" for="ou">{{ t('Source DC URL') }}</label
            ><input id="ou" v-model.trim="source.url" class="inp font-mono" required />
          </div>
          <div class="grid grid-cols-2 gap-2">
            <div>
              <label class="lbl" for="od">{{ t('Domain') }}</label
              ><input id="od" v-model.trim="source.domain" class="inp" required />
            </div>
            <div>
              <label class="lbl" for="ous">{{ t('User') }}</label
              ><input id="ous" v-model.trim="source.user" class="inp" autocomplete="off" required />
            </div>
          </div>
          <div>
            <label class="lbl" for="op">{{ t('Password') }}</label
            ><input id="op" v-model="source.password" type="password" class="inp" autocomplete="off" required />
          </div>
          <label class="flex items-center gap-2 text-[12px]"
            ><input v-model="source.insecure" type="checkbox" /> {{ t('Accept self-signed certificate') }}</label
          >
          <fieldset class="border-t border-line pt-2">
            <legend class="lbl">{{ t('Import') }}</legend>
            <div class="flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
              <label class="flex items-center gap-1.5"><input v-model="what.objects" type="checkbox" /> {{ t('Objects') }}</label>
              <label class="flex items-center gap-1.5"><input v-model="what.gpos" type="checkbox" /> GPOs</label>
              <label class="flex items-center gap-1.5"><input v-model="what.dns" type="checkbox" /> DNS</label>
              <label class="flex items-center gap-1.5"><input v-model="what.shares" type="checkbox" /> {{ t('Shares') }}</label>
              <label class="flex items-center gap-1.5"><input v-model="what.passwords" type="checkbox" /> {{ t('Passwords') }}</label>
            </div>
            <p v-if="what.passwords" class="mt-1 text-[11px] text-fg-muted">
              {{
                t(
                  'New-domain cutover only: the DC agent reads the source password hashes over a read-only DRSUAPI and sets them on matching users, using the login and password entered above. Needs the agent password-migration mode on; the source is left unchanged.',
                )
              }}
            </p>
            <p v-if="what.gpos" class="mt-1 text-[11px] text-fg-muted">
              {{ t('New GPOs, not linked, created by the agent of a DC in its GPO restore mode; names already in use are skipped.') }}
            </p>
            <div v-if="what.dns" class="mt-1 text-[11px] text-fg-muted">
              <label class="flex items-center gap-1.5 text-[12px] text-fg"
                ><input v-model="what.dynamic" type="checkbox" /> {{ t('Also records registered by clients (dynamic)') }}</label
              >
              <label class="flex items-center gap-1.5 text-[12px] text-fg"
                ><input v-model="what.zones" type="checkbox" />
                {{ t('Create the zones missing on this DC (primary, integrated with the directory)') }}</label
              >
              {{ t('Only A, CNAME and PTR; never records of domain controllers, SOA, NS or SRV.') }}
            </div>
            <div v-if="what.shares" class="mt-2 space-y-2">
              <div class="grid grid-cols-2 gap-2">
                <div>
                  <label class="lbl" for="shh">{{ t('Source server') }}</label
                  ><input id="shh" v-model.trim="shareSrc.host" class="inp font-mono" required placeholder="fs01.example.local" />
                </div>
                <div>
                  <label class="lbl" for="sha">{{ t('Target server') }}</label>
                  <select id="sha" v-model="shareSrc.agent" class="inp" required>
                    <option v-for="a in servers" :key="a.id" :value="a.id">{{ a.name }}</option>
                  </select>
                </div>
              </div>
              <div>
                <label class="lbl" for="shm">{{ t('Path map (Windows=Linux)') }}</label
                ><input id="shm" v-model.trim="shareSrc.map" class="inp font-mono" required />
              </div>
            </div>
          </fieldset>
          <div>
            <label class="lbl" for="os">{{ t('Save as profile (without the password)') }}</label
            ><input id="os" v-model.trim="source.save_as" class="inp" :placeholder="t('e.g. legacy-2008')" />
          </div>
          <button
            class="btn w-full justify-center"
            :disabled="busy || (!what.objects && !what.gpos && !what.dns && !what.shares && !what.passwords)"
          >
            <PlugZap :size="14" /> {{ busy && !plan ? t('Reading the source AD…') : t('Connect and preview') }}
          </button>
          <p class="text-[11px] text-fg-muted">
            {{ t('Read-only on the source') }} (<code>ldap://</code> {{ t('uses NTLM).') }}
            {{ t('Writes to the current domain only after confirmation.') }}
          </p>
        </form>
      </section>
    </div>

    <section class="card flex min-h-0 min-w-0 flex-col">
      <div class="card-h flex items-center gap-3">
        {{ applied ? t('Result') : t('Plan preview') }}
        <span v-if="plan" class="font-normal normal-case text-fg-muted">{{
          t('{n} actions, {changes} change(s)', { n: plan.length, changes: pending })
        }}</span>
        <span v-if="plan && mode === 'ad' && sourceCounts" class="font-normal normal-case text-fg-muted">{{ sourceSummary }}</span>
      </div>
      <div v-if="!plan" class="p-6 text-center text-fg-muted">{{ t('Choose the files and click preview.') }}</div>
      <template v-else>
        <div class="flex flex-wrap items-center gap-2 p-3">
          <button
            v-for="(n, k) in counts"
            :key="k"
            class="tag cursor-pointer border border-line"
            :class="kind === k ? 'bg-selection text-on-selection' : 'bg-surface-3 text-fg-strong'"
            @click="kind = kind === k ? '' : k"
          >
            {{ kindName(k) }}: {{ n }}
          </button>
          <div class="ml-auto flex gap-2">
            <button v-if="passwords" class="btn-ghost" @click="downloadPasswords">
              <KeyRound :size="13" /> {{ t('generated passwords ({n})', { n: Object.keys(passwords).length }) }}
            </button>
            <button v-if="!applied" class="btn" :disabled="busy || !pending" @click="apply">
              <DatabaseZap :size="14" /> {{ busy ? t('Saving…') : mode === 'folders' ? t('Send to the server') : t('Write to the domain') }}
            </button>
          </div>
        </div>
        <div class="max-h-[calc(var(--view-h)-330px)] min-h-0 flex-1 overflow-auto lg:max-h-none">
          <table class="grid-t border-x-0">
            <thead>
              <tr>
                <th>{{ t('Action') }}</th>
                <th>{{ t('Target') }}</th>
                <th>{{ t('Detail') }}</th>
                <th v-if="applied">{{ t('Result') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(a, i) in visible" :key="i" :class="a.kind === 'skip' ? 'text-fg-subtle' : ''">
                <td class="whitespace-nowrap">{{ kindName(a.kind) }}</td>
                <td class="font-semibold">{{ a.target }}</td>
                <td class="break-all" :title="serverText(a.detail)">{{ serverText(a.detail) }}</td>
                <td v-if="applied" :class="a.ok || a.kind === 'skip' ? 'text-ok' : 'text-err'" :title="serverText(a.detail)">
                  {{ serverText(a.result) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="passwords" class="border-t border-line bg-warn-soft px-3 py-2 text-[12px]">
          {{ t('Download the generated passwords now: they are not kept on the server. Each user changes the password at first logon.') }}
        </p>
      </template>
    </section>

    <!-- Live log/status of a running import (CSV, From another AD, Folders) -->
    <dialog v-if="syncLog" ref="dlgLog" class="pnl w-[min(680px,94vw)]" @cancel.prevent="syncLog.status !== 'running' && logClose()">
      <div class="pnl-head flex items-center gap-2">
        <span
          class="inline-block h-2.5 w-2.5"
          :class="{
            'animate-pulse bg-warn': syncLog.status === 'running',
            'bg-ok': syncLog.status === 'done',
            'bg-err': syncLog.status === 'error',
          }"
        ></span>
        {{ t('Import') }} — {{ syncLog.title }}
      </div>
      <div class="pnl-body">
        <p v-if="syncLog.status === 'running'" class="mb-2 text-[13px] text-fg-muted">{{ syncLog.phase }}</p>
        <p v-else class="mb-2 text-[13px]" :class="syncLog.status === 'error' ? 'text-err' : 'text-ok'">
          {{
            syncLog.status === 'error'
              ? syncLog.error || t('Finished with {n} error(s).', { n: syncLog.errors })
              : t('Finished: {n} action(s).', { n: syncLog.ok })
          }}
        </p>
        <div v-if="syncLog.steps.length" class="max-h-[46vh] overflow-auto border border-line">
          <table class="w-full text-[12px]">
            <tbody>
              <tr v-for="(s, i) in syncLog.steps" :key="i" class="border-b border-line/60 last:border-0">
                <td class="px-2 py-1 whitespace-nowrap font-mono" :class="s.ok ? 'text-ok' : 'text-err'">{{ s.ok ? '✓' : '✗' }}</td>
                <td class="px-2 py-1 whitespace-nowrap">{{ s.label }}</td>
                <td class="px-2 py-1 font-mono">{{ s.target }}</td>
                <td class="px-2 py-1 text-fg-muted" :title="serverText(s.detail)">{{ serverText(s.detail) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <div class="pnl-foot">
        <button type="button" class="btn" :disabled="syncLog.status === 'running'" @click="logClose()">{{ t('Close') }}</button>
      </div>
    </dialog>
  </div>
</template>
