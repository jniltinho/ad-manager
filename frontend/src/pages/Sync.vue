<script setup>
// Sync: folders brought from SMB shares into the file servers (rclone embedded in the agent). Jobs per server,
// preview (the complete plan), run, cancel, history and log; a mirror needs a typed confirmation and a clean
// preview of its current revision.
import { Archive, Eye, FolderSync, Pencil, Play, Plus, RefreshCw, ScrollText, Square, Trash2 } from 'lucide-vue-next'
import { computed, onUnmounted, ref } from 'vue'
import { del, enc, get, post } from '../api'
import { confirmTyped } from '../dialogs'
import { serverText, t } from '../i18n'
import { toast } from '../toasts'
import { agentHost, formatDate } from '../util'
import BackupTabs from '../components/BackupTabs.vue'
import SyncWizardDialog from '../components/dialogs/SyncWizardDialog.vue'
import QuarantineDialog from '../components/QuarantineDialog.vue'
import SyncRunDialog from '../components/dialogs/SyncRunDialog.vue'
import SyncLogDialog from '../components/dialogs/SyncLogDialog.vue'
import LoadingRow from '../components/LoadingRow.vue'

const agents = ref([])
const currentId = ref(0)
const data = ref(null) // { jobs, runs, roots, file_server, kerberos }
const error = ref('')
const loading = ref(false)
const agentsLoaded = ref(false)
let timer = null
onUnmounted(() => clearTimeout(timer))

async function loadAgents() {
  try {
    agents.value = await get('/agents')
    if (!currentId.value && agents.value.length) pick(agents.value[0].id)
  } catch (e) {
    toast.error(e.message)
  }
  agentsLoaded.value = true
}
async function load() {
  if (!currentId.value) return
  loading.value = true
  clearTimeout(timer)
  try {
    data.value = await get(`/agents/${currentId.value}/syncs`)
    error.value = ''
  } catch (e) {
    data.value = null
    error.value = e.message
  }
  loading.value = false
  if (data.value?.runs?.some((r) => r.status === 'queued' || r.status === 'running')) timer = setTimeout(load, 3000)
  else if (data.value?.file_server) loadQuarantines() // not on every poll: it walks the quarantines
}

// ── quarantines: what the runs deleted or replaced, per job, and the free space of the share roots
const qs = ref({ quarantines: [], space: [] })
async function loadQuarantines() {
  try {
    qs.value = await get(`/agents/${currentId.value}/syncs/quarantines`)
  } catch {
    qs.value = { quarantines: [], space: [] }
  }
}
const jobQ = (id) => qs.value.quarantines.filter((q) => q.job === id)
const jobQSize = (id) => jobQ(id).reduce((s, q) => s + q.size, 0)
const lowSpace = computed(() => qs.value.space.filter((s) => s.low))
const quarantine = ref(null) // { job, open }
function openQuarantine(id, open = '') {
  const job = data.value?.jobs.find((j) => j.id === id)
  if (job) quarantine.value = { job, open }
}
function pick(id) {
  currentId.value = id
  data.value = null
  load()
}
loadAgents()

const base = computed(() => `/agents/${currentId.value}`)
const jobName = (id) => data.value?.jobs?.find((j) => j.id === id)?.name || id
const unc = (s) => `\\\\${s.host}\\${s.share}${s.path ? '\\' + s.path.replaceAll('/', '\\') : ''}`
const size = (n) =>
  !n
    ? '0'
    : n >= 1 << 30
      ? (n / (1 << 30)).toFixed(1) + ' GiB'
      : n >= 1 << 20
        ? (n / (1 << 20)).toFixed(1) + ' MiB'
        : Math.ceil(n / 1024) + ' KiB'
const statusClass = (s) => (s === 'ok' ? 'text-ok' : s === 'error' ? 'text-err' : s === 'cancelled' ? 'text-fg-muted' : 'text-warn')
const statusText = (s) =>
  ({ ok: t('ok'), error: t('error'), cancelled: t('cancelled'), running: t('running'), queued: t('queued') })[s] || s
// what a running run is doing right now (so the panel never shows an opaque "running")
const phaseText = (p) => {
  switch (p?.phase) {
    case 'preparing':
      return t('preparing')
    case 'copying':
      return p.total ? t('copying {n} of {total}', { n: p.transfers || 0, total: p.total }) : t('copying')
    case 'acl':
      return t('applying Windows permissions') + (p.acl_items ? ` (${p.acl_items})` : '')
    case 'finalizing':
      return t('finalizing')
    default:
      return ''
  }
}
const lastRun = (id) => data.value?.runs?.find((r) => r.job === id)

// ── new and edit: the wizard
const wizard = ref(null) // { job } (null job: a new one)
const newJob = () => (wizard.value = { job: null })
const edit = (j) => (wizard.value = { job: j })
// a wizard closed before its preview answered only asks for the list again: it must not close a newer one
function saved(e) {
  if (!e?.closed) wizard.value = null
  load()
}
async function remove(j) {
  if (!(await confirmTyped(t('Delete the sync job {name}? The files already brought stay.', { name: j.name || j.id }), j.id))) return
  try {
    await del(`${base.value}/syncs/${enc(j.id)}`)
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
async function start(j, preview) {
  try {
    await post(`${base.value}/syncs/${enc(j.id)}/${preview ? 'preview' : 'run'}`)
    toast.success(preview ? t('Preview queued.') : t('Sync queued.'))
    setTimeout(load, 1500)
  } catch (e) {
    toast.error(e.message)
  }
}
async function cancel(r) {
  try {
    await post(`${base.value}/sync-runs/${enc(r.id)}/cancel`)
    setTimeout(load, 1000)
  } catch (e) {
    toast.error(e.message)
  }
}
const logRun = ref(null) // { run, text } for SyncLogDialog
async function showLog(r) {
  try {
    const res = await fetch(`/api${base.value}/sync-runs/${enc(r.id)}/log`, { credentials: 'same-origin' })
    logRun.value = { run: r, text: await res.text() }
  } catch (e) {
    toast.error(e.message)
  }
}

// ── details of a run (the plan, the result)
const details = ref(null)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <BackupTabs />
    <div class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[260px_1fr] lg:grid-rows-[minmax(0,1fr)]">
      <aside class="flex min-h-0 flex-col gap-5">
        <section class="card">
          <div class="card-h flex items-center justify-between">
            {{ t('Servers') }}
            <button class="act normal-case" :disabled="loading" @click="load"><RefreshCw :size="12" /> {{ t('refresh') }}</button>
          </div>
          <ul class="side-nav divide-y divide-line text-[12px]">
            <li
              v-for="a in agents"
              :key="a.id"
              class="cursor-pointer px-3 py-2 font-semibold"
              :class="currentId === a.id ? 'nav-on bg-nav-on text-on-nav-on shadow-(--shadow-nav-on)' : 'hover:bg-row-hover'"
              :aria-current="currentId === a.id || null"
              @click="pick(a.id)"
            >
              {{ a.name }}
              <span class="block text-[11px] font-normal">{{ agentHost(a.url) }}</span>
            </li>
            <li v-if="!agents.length" class="px-3 py-3 text-fg-muted">{{ t('No servers.') }}</li>
          </ul>
        </section>
        <p class="text-[11px] text-fg-muted">
          {{
            t(
              "The server's agent brings folders from an SMB share (Windows or Samba) with the rclone engine built into ad-manager: signed SMB, with the server's machine account (Kerberos) or a user and password kept encrypted. Files take the destination folder's Windows permissions.",
            )
          }}
        </p>
      </aside>

      <section class="flex min-h-0 flex-col gap-5">
        <div class="card flex max-h-[40%] min-h-0 shrink-0 flex-col">
          <div class="card-h flex items-center gap-3">
            <FolderSync :size="14" /> {{ t('Sync jobs') }}
            <button v-if="data?.file_server" class="act ml-auto normal-case" @click="newJob"><Plus :size="12" /> {{ t('new') }}</button>
          </div>
          <p v-for="s in lowSpace" :key="s.root" class="border-b border-line px-3 py-2 text-warn">
            {{ t('Little free space on {root}: {free} of {total}.', { root: s.root, free: size(s.free), total: size(s.total) }) }}
          </p>
          <p v-if="error" class="p-3 text-err">{{ serverText(error) }}</p>
          <p v-else-if="data && !data.file_server" class="p-3 text-fg-muted">{{ t('Folders are synced into a file server.') }}</p>
          <div v-else class="min-h-0 overflow-auto">
            <table class="grid-t border-x-0">
              <thead>
                <tr>
                  <th>{{ t('Name') }}</th>
                  <th>{{ t('Source') }}</th>
                  <th>{{ t('Destination') }}</th>
                  <th>{{ t('Mode') }}</th>
                  <th>{{ t('When (cron)') }}</th>
                  <th>{{ t('Last run') }}</th>
                  <th>{{ t('Quarantine') }}</th>
                  <th class="w-36"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="j in data?.jobs || []" :key="j.id">
                  <td class="max-w-56 truncate font-semibold whitespace-nowrap" :title="j.name || j.id">{{ j.name || j.id }}</td>
                  <td class="max-w-80 truncate font-mono text-[11px] whitespace-nowrap" :title="unc(j.source)">
                    {{ unc(j.source) }}
                    <span class="tag bg-surface-2 text-fg-muted">{{ j.source.auth === 'ntlm' ? 'NTLM' : 'Kerberos' }}</span>
                  </td>
                  <td class="max-w-64 truncate font-mono text-[11px] whitespace-nowrap" :title="j.dest">{{ j.dest }}</td>
                  <td>
                    {{ j.mode === 'mirror' ? t('mirror') : t('copy') }}
                    <span v-if="j.mode === 'mirror' && j.approved_rev !== j.rev" class="text-warn">· {{ t('preview needed') }}</span>
                  </td>
                  <td class="font-mono text-[11px]">{{ j.enabled && j.cron ? j.cron : '—' }}</td>
                  <td :class="statusClass(lastRun(j.id)?.status)">
                    <template v-if="lastRun(j.id)?.status === 'running' && phaseText(lastRun(j.id).progress)"
                      >{{ statusText('running') }} · {{ phaseText(lastRun(j.id).progress) }}</template
                    >
                    <template v-else-if="lastRun(j.id)"
                      >{{ statusText(lastRun(j.id).status) }} · {{ formatDate(lastRun(j.id).started) }}</template
                    >
                    <template v-else>—</template>
                  </td>
                  <td class="whitespace-nowrap">
                    <button v-if="jobQ(j.id).length" class="act px-0 normal-case" @click="openQuarantine(j.id)">
                      <Archive :size="12" /> {{ size(jobQSize(j.id)) }}
                    </button>
                    <template v-else>—</template>
                  </td>
                  <td class="space-x-2 text-right whitespace-nowrap">
                    <button class="act px-0" :title="t('Preview')" @click="start(j, true)"><Eye :size="13" /></button>
                    <button class="act px-0" :title="t('Run now')" @click="start(j, false)"><Play :size="13" /></button>
                    <button class="act px-0" :title="t('Edit')" @click="edit(j)"><Pencil :size="13" /></button>
                    <button class="act px-0" :title="t('Delete')" @click="remove(j)"><Trash2 :size="13" /></button>
                  </td>
                </tr>
                <LoadingRow v-if="!data && !error && (loading || !agentsLoaded)" :cols="8" />
                <tr v-if="data && !data.jobs?.length">
                  <td colspan="8" class="py-3 text-center text-fg-muted">{{ t('No sync jobs.') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="card flex min-h-0 flex-1 flex-col">
          <div class="card-h">{{ t('Runs') }}</div>
          <div class="min-h-0 flex-1 overflow-auto">
            <table class="grid-t border-x-0">
              <thead>
                <tr>
                  <th>{{ t('Date') }}</th>
                  <th>{{ t('Job') }}</th>
                  <th>{{ t('Kind') }}</th>
                  <th class="w-full">{{ t('Status') }}</th>
                  <th class="text-right">{{ t('Files') }}</th>
                  <th class="text-right">{{ t('Size') }}</th>
                  <th class="text-right">{{ t('Deleted') }}</th>
                  <th class="w-24"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in data?.runs || []" :key="r.id">
                  <td class="whitespace-nowrap" :title="r.id">{{ formatDate(r.started) }}</td>
                  <td class="max-w-56 truncate whitespace-nowrap" :title="jobName(r.job)">{{ jobName(r.job) }}</td>
                  <td class="text-fg-muted">{{ r.preview ? t('preview') : r.origin === 'schedule' ? t('scheduled') : t('run') }}</td>
                  <td class="max-w-0 truncate whitespace-nowrap" :class="statusClass(r.status)" :title="r.error ? serverText(r.error) : ''">
                    {{ statusText(r.status) }}<span v-if="r.error" class="text-fg-muted"> · {{ serverText(r.error) }}</span>
                    <span v-else-if="r.status === 'running' && phaseText(r.progress)" class="text-fg-muted">
                      · {{ phaseText(r.progress) }}</span
                    >
                  </td>
                  <td class="text-right tabular-nums whitespace-nowrap">
                    {{
                      r.preview
                        ? (r.result?.plan?.New || 0) + (r.result?.plan?.Changed || 0)
                        : (r.result?.transfers ?? r.progress?.transfers ?? '—')
                    }}
                  </td>
                  <td class="text-right tabular-nums whitespace-nowrap">
                    {{
                      r.preview
                        ? size((r.result?.plan?.NewBytes || 0) + (r.result?.plan?.ChangedBytes || 0))
                        : size(r.result?.bytes ?? r.progress?.bytes)
                    }}
                  </td>
                  <td class="text-right tabular-nums whitespace-nowrap">
                    {{ r.preview ? (r.result?.plan?.Delete ?? '—') : (r.result?.deleted ?? '—') }}
                  </td>
                  <td class="space-x-2 text-right whitespace-nowrap">
                    <button v-if="r.result" class="act px-0" :title="t('Details')" @click="details = r"><Eye :size="13" /></button>
                    <button class="act px-0" :title="t('Log')" @click="showLog(r)"><ScrollText :size="13" /></button>
                    <button v-if="r.status === 'running'" class="act px-0" :title="t('Cancel')" @click="cancel(r)">
                      <Square :size="13" />
                    </button>
                  </td>
                </tr>
                <LoadingRow v-if="!data && !error && (loading || !agentsLoaded)" :cols="8" />
                <tr v-if="data && !data.runs?.length">
                  <td colspan="8" class="py-3 text-center text-fg-muted">{{ t('No runs yet.') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </div>
  </div>

  <SyncWizardDialog
    v-if="wizard"
    :agent="currentId"
    :roots="data?.roots || []"
    :kerberos="!!data?.kerberos"
    :machine="data?.machine || ''"
    :job="wizard.job"
    @close="wizard = null"
    @saved="saved"
  />

  <QuarantineDialog
    v-if="quarantine"
    :agent="currentId"
    :job="quarantine.job"
    :quarantines="jobQ(quarantine.job.id)"
    :open="quarantine.open"
    @close="quarantine = null"
    @changed="loadQuarantines"
  />

  <SyncRunDialog
    v-if="details"
    :run="details"
    :job-name="jobName(details.job)"
    :quarantined="jobQ(details.job).some((q) => q.run === details.id)"
    @close="details = null"
    @open-quarantine="openQuarantine(details.job, details.job + '-' + details.id)"
  />

  <SyncLogDialog v-if="logRun" :run="logRun.run" :text="logRun.text" :job-name="jobName(logRun.run.job)" @close="logRun = null" />
</template>
