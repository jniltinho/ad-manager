<script setup>
// Folders and shares on the file servers (ad-manager agent).
// Every change becomes a task in the server's queue; the page follows it until done.
import { FolderPlus, Plus, RefreshCw, Trash2, Users } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { auth, get, post, del, enc } from '../api'
import { locale, serverText, t } from '../i18n'
import { toast } from '../toasts'
import { confirmTyped } from '../dialogs'
import { agentHost, formatDate } from '../util'
import ShareSessionsDialog from '../components/dialogs/ShareSessionsDialog.vue'
import FileServerDialog from '../components/dialogs/FileServerDialog.vue'
import ShareFormDialog from '../components/dialogs/ShareFormDialog.vue'
import LoadingNote from '../components/LoadingNote.vue'
import LoadingRow from '../components/LoadingRow.vue'

const agents = ref([])
const current = ref(null)
const shares = ref([])
const roots = ref([])
const status = ref({}) // id → 'ok' | 'down'
const isDC = ref({}) // id → a domain controller without its file mode (read only: health instead of shares)
const dcTag = ref({}) // id → a domain controller (with or without files)
const tasks = ref([])
const adGroups = ref([])
const adUsers = ref([])
const loading = ref(false)
const agentsLoaded = ref(false) // until the server list arrives, the page says it is loading (not "no servers")

async function loadAgents() {
  try {
    agents.value = await get('/agents')
    if (!current.value && agents.value.length) current.value = agents.value[0]
    for (const a of agents.value)
      get(`/agents/${a.id}/health`)
        .then((h) => {
          status.value[a.id] = 'ok'
          isDC.value[a.id] = !!h.dc && !h.files
          dcTag.value[a.id] = !!h.dc
        })
        .catch(() => (status.value[a.id] = 'down'))
  } catch (e) {
    toast.error(e.message)
  }
  agentsLoaded.value = true
}
loadAgents()
get('/groups')
  .then(
    (gs) =>
      (adGroups.value = gs
        .filter((g) => g.type === 'security')
        .map((g) => `${auth.netbios}\\${g.name}`)
        .sort()),
  )
  .catch(() => {})
get('/users')
  .then((us) => (adUsers.value = us.map((u) => `${auth.netbios}\\${u.sam}`).sort()))
  .catch(() => {})

async function loadShares() {
  if (!current.value || isDC.value[current.value.id]) return // a DC shows its health instead
  const target = current.value.id // a reply from another server (quick switch) is dropped
  loading.value = true
  shares.value = []
  try {
    const r = await Promise.all([get(`/agents/${target}/shares`), get(`/agents/${target}/dirs`).then((r) => r.roots)])
    if (target !== current.value?.id) return
    shares.value = (r[0] || []).map((x) => ({ ...x, agent: target })) // an older agent answers null for none
    roots.value = r[1]
  } catch (e) {
    toast.error(e.message)
  }
  loading.value = false
  loadSizes(target)
}

// total size of each folder (du on the server, cached for 5 min); arrives after the list
const sizes = ref({})
const measuring = ref(false)
async function loadSizes(target, refresh = false) {
  measuring.value = true
  try {
    const r = await get(`/agents/${target}/sizes${refresh ? '?refresh=1' : ''}`)
    if (target === current.value?.id) sizes.value = r.sizes || {}
  } catch {
    sizes.value = {}
  }
  measuring.value = false
}
function bytes(n) {
  if (n == null) return measuring.value ? '…' : '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  for (; n >= 1024 && i < u.length - 1; i++) n /= 1024
  return `${n.toLocaleString(locale(), { maximumFractionDigits: i ? 1 : 0 })} ${u[i]}`
}
// access lists the groups and users with write ("rw", full control marked) or read-only ("ro") access.
const access = (s, kind) => {
  const full = kind === 'rw' ? [...(s.full_groups || []), ...(s.full_users || [])].map((n) => `${n} (${t('Full control')})`) : []
  return [...full, ...(s[`${kind}_groups`] || []), ...(s[`${kind}_users`] || [])].join(', ')
}
async function refresh() {
  await loadShares()
  if (current.value) loadSizes(current.value.id, true)
}
const total = computed(() => Object.values(sizes.value).reduce((a, b) => a + b, 0))
watch(current, loadShares)

// ── domain controller health (agent on a DC): replication and consistency checks, read only
const dc = ref(null)
// replication and the database check load on their own: a slow dbcheck does not hold the replication back, and a
// failure of one does not hide the other
async function loadDC(refresh = false) {
  const target = current.value?.id
  if (!target || !isDC.value[target]) return (dc.value = null)
  dc.value = { ...(refresh ? dc.value : {}), loading: true }
  const d = dc.value // the reactive proxy (what dc.value returns later), not the object assigned
  const part = async (key, path) => {
    try {
      const v = await get(path)
      if (dc.value === d) Object.assign(d, { [key]: v, [key + 'Error']: '' })
    } catch (e) {
      if (dc.value === d) Object.assign(d, { [key + 'Error']: e.message })
    }
  }
  await Promise.all([
    part('repl', `/agents/${target}/dc/replication`),
    part('checks', `/agents/${target}/dc/checks${refresh ? '?refresh=1' : ''}`),
  ])
  if (dc.value === d) d.loading = false
}
watch([current, isDC], () => loadDC(), { deep: true })

// ── sessions and open files of the current server (smbstatus), with disconnect
const sessions = ref(false)

// ── server registration
const ag = ref(null) // the server being registered or edited
async function agentSaved() {
  ag.value = null
  current.value = null
  await loadAgents()
}
async function removeAgent(a) {
  if (!(await confirmTyped(t('Remove the registration of server {name}? Nothing is deleted on the server.', { name: a.name }), a.name)))
    return
  await del(`/agents/${a.id}`).catch((e) => toast.error(e.message))
  current.value = null
  loadAgents()
}

// ── tasks: enqueue and follow
async function enqueue(type, payload, label) {
  track(await post(`/agents/${current.value.id}/tasks`, { type, payload }), label)
}
// track lists a queued task in this session and follows it
function track(task, label) {
  const item = { id: task.id, label, state: task.state, agent: current.value.id }
  tasks.value.unshift(item)
  follow(item)
}
// follow polls a task until it ends: every second for the first 40 s, then every 5 s (a task waits for the ones
// queued before it, and each may take up to the agent's 30-minute limit; 24 h at most)
async function follow(item) {
  for (let i = 0; i < 40 + 17280; i++) {
    await new Promise((r) => setTimeout(r, i < 40 ? 1000 : 5000))
    try {
      const task = await get(`/agents/${item.agent}/tasks/${enc(item.id)}`)
      item.state = task.state
      item.error = serverText(task.last_error)
      if (task.state === 'completed') {
        toast.success(t('{task}: done.', { task: item.label }))
        if (current.value?.id === item.agent) loadShares()
        return
      }
      if (task.state === 'archived') return toast.error(`${item.label}: ${serverText(task.last_error)}`)
    } catch {
      /* try again */
    }
  }
}

// ── new share, or edit of a published one: { share } (null for a new one)
const form = ref(null)
const openNew = () => (form.value = { share: null })
const openEdit = (s) => (form.value = { share: s })
async function remove(s) {
  if (s.agent !== current.value?.id) return toast.error(t('The list changed servers; refresh and try again.'))
  const unc = `\\\\${current.value.name}\\${s.name}`
  if (!(await confirmTyped(t('Unpublish {share}? The folder and its files stay on the server.', { share: unc }), s.name))) return
  try {
    await enqueue('share:delete', { name: s.name }, t('Remove {share}', { share: unc }))
  } catch (e) {
    toast.error(e.message)
  }
}
const stateNames = {
  pending: 'queued',
  active: 'running',
  retry: 'retrying',
  completed: 'done',
  archived: 'failed',
  scheduled: 'scheduled',
}
</script>

<template>
  <div class="grid h-full min-h-0 gap-5 xl:grid-cols-[300px_1fr] xl:grid-rows-[minmax(0,1fr)]">
    <aside class="flex min-h-0 flex-col gap-5">
      <section class="card">
        <div class="card-h flex items-center justify-between">
          {{ t('File servers') }}
          <button class="act normal-case" @click="ag = { name: '', url: 'https://', token: '', insecure: true }">
            <Plus :size="12" /> {{ t('new') }}
          </button>
        </div>
        <ul class="side-nav divide-y divide-line text-[12px]">
          <li
            v-for="a in agents"
            :key="a.id"
            class="flex cursor-pointer items-center gap-2 whitespace-nowrap px-3 py-2"
            :class="current?.id === a.id ? 'nav-on bg-nav-on text-on-nav-on shadow-(--shadow-nav-on)' : 'hover:bg-row-hover'"
            :aria-current="current?.id === a.id || null"
            @click="current = a"
          >
            <span
              class="h-2 w-2 shrink-0"
              :class="status[a.id] === 'ok' ? 'bg-ok-fill' : status[a.id] === 'down' ? 'bg-err-fill' : 'bg-neutral-fill'"
              :title="status[a.id] === 'down' ? t('unavailable') : t('online')"
            ></span>
            <!-- one line: the host gives way first, then the name (shrink is a weight; this one is big enough to act as a
                 priority, the name losing less than a layout unit while the host has room) -->
            <span class="min-w-0 truncate font-semibold" :title="a.name">{{ a.name }}</span>
            <span class="min-w-0 shrink-[1000000] truncate text-[11px]" :title="agentHost(a.url)">{{ agentHost(a.url) }}</span>
            <span v-if="dcTag[a.id]" class="tag shrink-0 bg-surface-2 text-fg-muted">DC</span>
            <span class="ml-auto shrink-0 space-x-1">
              <button
                class="act px-0 text-[11px]"
                :class="current?.id === a.id ? 'text-inherit' : ''"
                @click.stop="ag = { ...a, token: '' }"
              >
                {{ t('edit') }}
              </button>
              <button class="act px-0 text-[11px]" :class="current?.id === a.id ? 'text-inherit' : ''" @click.stop="removeAgent(a)">
                {{ t('remove') }}
              </button>
            </span>
          </li>
          <li v-if="!agentsLoaded" class="px-3 py-3"><LoadingNote /></li>
          <li v-else-if="!agents.length" class="px-3 py-3 text-fg-muted">
            {{ t('No servers. Install') }} <code>ad-manager agent</code> {{ t('on the file server and register it here.') }}
          </li>
        </ul>
      </section>
      <section class="card flex min-h-0 flex-1 flex-col">
        <div class="card-h flex items-center justify-between">
          {{ t('Tasks of this session') }}
          <a v-if="auth.asynqmon" href="/asynqmon" target="_blank" class="act normal-case">{{ t('queue') }} ↗</a>
        </div>
        <ul class="min-h-0 flex-1 divide-y divide-line overflow-auto text-[12px]">
          <li v-for="task in tasks" :key="task.id" class="px-3 py-1.5">
            <div class="flex justify-between gap-2">
              <span class="truncate" :title="task.label">{{ task.label }}</span>
              <span
                class="tag shrink-0"
                :class="
                  task.state === 'completed'
                    ? 'bg-ok-soft text-on-ok-soft'
                    : task.state === 'archived'
                      ? 'bg-err-fill text-on-err-fill'
                      : 'bg-surface-2 text-fg-strong'
                "
                >{{ stateNames[task.state] ? t(stateNames[task.state]) : task.state }}</span
              >
            </div>
            <div v-if="task.error" class="text-[11px] text-err">{{ task.error }}</div>
          </li>
          <li v-if="!tasks.length" class="px-3 py-2 text-fg-muted">{{ t('None.') }}</li>
        </ul>
      </section>
    </aside>

    <section v-if="current && isDC[current.id]" class="card flex min-h-0 min-w-0 flex-col">
      <div class="card-h flex items-center gap-3">
        <span>{{ t('Domain controller {server}', { server: current.name }) }}</span>
        <span class="font-normal normal-case text-fg-muted">{{ t('read only') }}</span>
        <div class="ml-auto flex gap-2 normal-case">
          <button class="btn-ghost" :disabled="dc?.loading" @click="loadDC(true)">
            <RefreshCw :size="13" /> {{ dc?.loading ? t('Checking…') : t('Check again') }}
          </button>
        </div>
      </div>
      <div class="min-h-0 flex-1 space-y-4 overflow-auto p-3 text-[12px]">
        <template v-if="dc">
          <p v-if="dc.replError" class="text-err">{{ dc.replError }}</p>
          <LoadingNote v-else-if="!dc.repl" />
          <div v-else>
            <span class="lbl"
              >{{ t('Replication') }} ·
              <span :class="dc.repl.ok ? 'text-ok' : 'text-err'">{{ dc.repl.ok ? t('healthy') : t('with failures') }}</span></span
            >
            <table class="grid-t table-fixed">
              <thead>
                <tr>
                  <th class="w-24">{{ t('Direction') }}</th>
                  <th>{{ t('Partition') }}</th>
                  <th>{{ t('Partner DC') }}</th>
                  <th class="w-44">{{ t('Last success') }}</th>
                  <th class="w-20 text-right">{{ t('Failures') }}</th>
                  <th>{{ t('Last attempt') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="l in dc.repl.links" :key="l.direction + l.nc + l.dsa">
                  <td>{{ l.direction === 'inbound' ? t('receives') : t('sends') }}</td>
                  <td class="truncate font-mono text-[11px]" :title="l.nc">{{ l.nc }}</td>
                  <td class="truncate" :title="l.dsa">{{ l.dsa }}{{ l.deleted ? ' (' + t('deleted') + ')' : '' }}</td>
                  <td class="truncate">{{ l.last_success === 'NTTIME(0)' ? t('never') : l.last_success }}</td>
                  <td class="text-right tabular-nums" :class="l.failures ? 'text-err' : ''">{{ l.failures }}</td>
                  <td class="truncate" :class="l.ok ? '' : 'text-err'" :title="l.message">{{ l.message }}</td>
                </tr>
                <tr v-if="!dc.repl.links.length">
                  <td colspan="6" class="py-3 text-center text-fg-muted">
                    {{ t('No other domain controller replicates with this one.') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="dc.checksError" class="text-err">{{ dc.checksError }}</p>
          <p v-else-if="!dc.checks" class="text-fg-muted">{{ t('Checking the directory database…') }}</p>
          <div v-else>
            <span class="lbl"
              >{{ t('Consistency') }} · {{ formatDate(dc.checks.at) }} ·
              <span :class="dc.checks.ok ? 'text-ok' : 'text-err'">{{ dc.checks.ok ? t('no problems') : t('problems found') }}</span></span
            >
            <div v-for="c in dc.checks.checks" :key="c.name" class="mb-2 border border-line bg-surface-3 px-3 py-2">
              <div class="flex items-center gap-2">
                <span class="font-semibold">{{
                  c.name === 'dbcheck' ? t('Directory database (dbcheck)') : t('Sysvol permissions (sysvolcheck)')
                }}</span>
                <span class="tag" :class="c.ok ? 'bg-ok-soft text-on-ok-soft' : 'bg-err-soft text-on-err-soft'">{{
                  c.ok ? 'OK' : t('problems')
                }}</span>
                <span class="text-fg-muted">{{ t('{n} objects, {e} errors', { n: c.objects ?? 0, e: c.errors ?? 0 }) }}</span>
              </div>
              <pre v-if="!c.ok && c.output" class="mt-1 max-h-40 overflow-auto font-mono text-[11px] whitespace-pre-wrap">{{
                c.output
              }}</pre>
            </div>
            <p class="text-[11px] text-fg-muted">{{ t('Read only: nothing is fixed here (samba-tool dbcheck without --fix).') }}</p>
          </div>
        </template>
      </div>
    </section>

    <section v-else class="card flex min-h-0 min-w-0 flex-col">
      <div class="card-h flex items-center gap-3">
        <span>{{ current ? t('Shares on {server}', { server: current.name }) : t('Shares') }}</span>
        <span v-if="current" class="font-normal normal-case text-fg-muted">{{ current.url }}</span>
        <div v-if="current" class="ml-auto flex items-center gap-2 normal-case">
          <span v-if="shares.length" class="text-[12px] text-fg-muted" :title="t('Sum of the folders (nested ones count again)')">{{
            t('total {size}', { size: measuring ? '…' : bytes(total) })
          }}</span>
          <button class="btn-ghost" :disabled="status[current.id] === 'down'" @click="sessions = true">
            <Users :size="13" /> {{ t('Sessions') }}
          </button>
          <button class="btn-ghost" @click="refresh"><RefreshCw :size="13" /> {{ t('Refresh') }}</button>
          <button class="btn" :disabled="status[current.id] === 'down'" @click="openNew">
            <FolderPlus :size="13" /> {{ t('New share') }}
          </button>
        </div>
      </div>
      <div class="min-h-0 flex-1 overflow-auto">
        <!-- fixed columns, one line per row: long text ends in "…" and shows in the tooltip -->
        <table class="grid-t table-fixed border-0">
          <colgroup>
            <col class="w-[16%]" />
            <col class="w-[19%]" />
            <col class="w-20" />
            <col class="w-[20%]" />
            <col class="w-[14%]" />
            <col />
            <col class="w-36" />
          </colgroup>
          <thead>
            <tr>
              <th>{{ t('Name') }}</th>
              <th>{{ t('Path') }}</th>
              <th class="text-right">{{ t('Size') }}</th>
              <th>{{ t('Write access') }}</th>
              <th>{{ t('Read access') }}</th>
              <th>{{ t('Comment') }}</th>
              <th class="text-right">{{ t('Actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <LoadingRow v-if="loading || !agentsLoaded" :cols="7" />
            <tr v-for="s in shares" :key="s.name">
              <td>
                <div class="flex items-center gap-1.5">
                  <button
                    class="act truncate px-0 font-semibold"
                    :title="s.name"
                    :disabled="status[current.id] === 'down'"
                    @click="openEdit(s)"
                  >
                    {{ s.name }}
                  </button>
                  <span v-if="!s.browseable" class="tag shrink-0 bg-surface-2 text-fg-muted">{{ t('hidden') }}</span>
                </div>
              </td>
              <td class="truncate font-mono text-[11px]" :title="s.path">{{ s.path }}</td>
              <td class="text-right whitespace-nowrap tabular-nums">{{ bytes(sizes[s.name]) }}</td>
              <td class="truncate" :title="access(s, 'rw')">{{ access(s, 'rw') }}</td>
              <td class="truncate" :title="access(s, 'ro')">{{ access(s, 'ro') }}</td>
              <td class="truncate text-fg-muted" :title="s.comment">{{ s.comment }}</td>
              <td class="text-right whitespace-nowrap">
                <button class="act hover:!text-err" @click="remove(s)"><Trash2 :size="12" /> {{ t('remove') }}</button>
              </td>
            </tr>
            <tr v-if="current && !loading && !shares.length">
              <td colspan="7" class="py-4 text-center text-fg-muted">{{ t('No shares published by this server.') }}</td>
            </tr>
            <tr v-if="!current && agentsLoaded">
              <td colspan="7" class="py-4 text-center text-fg-muted">{{ t('Choose a server.') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>

  <ShareSessionsDialog v-if="sessions" :server="current" @close="sessions = false" />
  <FileServerDialog v-if="ag" :agent="ag" @close="ag = null" @saved="agentSaved" />
  <ShareFormDialog
    v-if="form"
    :server="current"
    :share="form.share"
    :roots="roots"
    :groups="adGroups"
    :users="adUsers"
    @close="form = null"
    @queued="track"
  />
</template>
