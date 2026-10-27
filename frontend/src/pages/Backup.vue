<script setup>
// Backup: each server's backups (made and kept by its agent; the panel's own here), a backup now, download, delete,
// restore (or send a backup and restore it) and the schedules with their retention (kept and run by the agent, with
// the panel off too).
import { Archive, CalendarClock, Download, History, Plus, RefreshCw, RotateCcw, Save, ScrollText, Trash2, Upload } from 'lucide-vue-next'
import { computed, onUnmounted, ref } from 'vue'
import { del, get, post, put, enc } from '../api'
import { confirmTyped, showAlert } from '../dialogs'
import { serverText, t } from '../i18n'
import { toast } from '../toasts'
import { formatDate, saveFile, spinner } from '../util'
import BackupTabs from '../components/BackupTabs.vue'
import RestoreBackupDialog from '../components/dialogs/RestoreBackupDialog.vue'
import SendBackupDialog from '../components/dialogs/SendBackupDialog.vue'
import LoadingNote from '../components/LoadingNote.vue'

const servers = ref([])
const loading = ref(false)
const spin = spinner(loading) // the refresh icon turns while loading, at least briefly
const currentId = ref(0)
const current = computed(() => servers.value.find((s) => s.id === currentId.value) || servers.value[0])
async function load() {
  loading.value = true
  try {
    servers.value = await get('/backups')
  } catch (e) {
    toast.error(e.message)
  }
  loading.value = false
  clearTimeout(reload)
  // a restoration or backup still running: look again shortly
  if (servers.value.some((s) => s.restore?.status === 'running' || s.backups.some((b) => b.status === 'running')))
    reload = setTimeout(load, 4000)
}
let reload = null
onUnmounted(() => clearTimeout(reload))

const kindNames = {
  dc: 'Domain controller',
  'fileserver-config': 'Configuration and permissions',
  'fileserver-data': 'Data folders',
  panel: 'Panel',
  'samba-files': 'Samba files (before the setup)',
  all: 'Complete (configuration + all share roots, full)',
}
const kindName = (k) => (kindNames[k] ? t(kindNames[k]) : k)
const size = (n) =>
  n >= 1 << 30 ? (n / (1 << 30)).toFixed(1) + ' GiB' : n >= 1 << 20 ? (n / (1 << 20)).toFixed(1) + ' MiB' : Math.ceil(n / 1024) + ' KiB'
const origins = { manual: 'manual', upload: 'sent', 'pre-restore': 'before a restoration', setup: 'before the setup' }
const origin = (o) => (origins[o] ? t(origins[o]) : o.replace('schedule:', t('schedule') + ' '))
const base = (s) => (s.id === 0 ? '/backups/panel' : `/agents/${s.id}/backups`)

// ── backup now
const now = ref({ kind: '', folders: '', full: false })
const busy = ref(false)
async function backupNow() {
  const s = current.value
  const kind = now.value.kind || s.kinds[0]
  busy.value = true
  try {
    if (s.id === 0) await post('/backups/panel')
    else
      await post(base(s), {
        kind,
        full: kind === 'fileserver-data' && now.value.full,
        folders:
          kind === 'fileserver-data'
            ? now.value.folders
                .split('\n')
                .map((f) => f.trim())
                .filter(Boolean)
            : [],
      })
    toast.success(s.id === 0 ? t('Backup done.') : t('Backup queued on {server}: it shows in the list when it ends.', { server: s.name }))
    setTimeout(load, s.id === 0 ? 0 : 3000)
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}

async function remove(b) {
  const s = current.value
  const extra = b.full && b.kind === 'fileserver-data' ? '\n' + t('The incremental backups of this chain go too.') : ''
  if (!(await confirmTyped(t('Delete the backup {id}?', { id: b.id }) + extra, b.id.slice(-8)))) return
  try {
    const r = await del(`${base(s)}/${enc(b.id)}`)
    toast.success(t('{n} backup(s) deleted.', { n: r.deleted.length }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}

function download(b) {
  saveFile({ name: b.file || b.id + '.tar.gz', apiPath: `/api${base(current.value)}/${enc(b.id)}/download` })
}

function showLog(b) {
  showAlert([...(b.log || []), b.error ? t('Error') + ': ' + serverText(b.error) : ''].filter(Boolean).join('\n') || t('No log.'), {
    title: b.id,
  })
}

// ── restore: what changes is shown first; anything that replaces what is in use asks for the end of the id
const restoring = ref(null) // the backup
function openRestore(b) {
  if (b.kind === 'dc')
    return showAlert(
      t(
        'A domain controller backup is not restored over a DC in use: install ad-manager on a new server and, in the initial setup, choose "Restore a DC" with this backup (docs/backup.md).',
      ),
    )
  restoring.value = b
}
function restored(out) {
  restoring.value = null
  if (out?.restart) {
    toast.success(t('The panel restarts with the restored database; this page reloads in a few seconds.'))
    setTimeout(() => location.reload(), 6000)
    return
  }
  toast.success(t('Restoration queued on {server}: follow it under Last restoration.', { server: current.value.name }))
  setTimeout(load, 2000)
}
function showRestoreLog(r) {
  showAlert([...(r.log || []), r.error ? t('Error') + ': ' + serverText(r.error) : ''].filter(Boolean).join('\n') || t('No log.'), {
    title: r.backup,
  })
}

// ── send a backup (.tar.gz) to a server: streamed, with progress
const sending = ref(false)
function sent(data) {
  sending.value = false
  toast.success(t('Backup sent: {id}', { id: data.id }))
  load()
}

// ── schedules (agents only)
const schedules = ref(null)
const presets = [
  ['0 2 * * *', 'Every day at 02:00'],
  ['0 3 * * 0', 'Sundays at 03:00'],
  ['0 3 1 * *', 'Day 1 of each month at 03:00'],
]
async function loadSchedules() {
  schedules.value = null
  const s = current.value
  if (!s || s.id === 0 || s.error || !s.kinds.length) return
  try {
    schedules.value = (await get(`/agents/${s.id}/backup-schedules`)).map((x) => ({ ...x, foldersText: (x.folders || []).join('\n') }))
  } catch (e) {
    toast.error(e.message)
  }
}
function pick(s) {
  currentId.value = s.id
  now.value = { kind: s.kinds[0] || '', folders: '', full: false }
  loadSchedules()
}
function addSchedule() {
  const s = current.value
  schedules.value.push({
    id: 'backup-' + (schedules.value.length + 1),
    kind: s.kinds[0],
    cron: '0 2 * * *',
    foldersText: '',
    full_every_days: 7,
    retention: { chains: 4, days: 0 },
    enabled: true,
  })
}
async function saveSchedules() {
  const s = current.value
  try {
    const body = schedules.value.map(({ foldersText, ...x }) => ({
      ...x,
      folders:
        x.kind === 'fileserver-data'
          ? foldersText
              .split('\n')
              .map((f) => f.trim())
              .filter(Boolean)
          : [],
      full_every_days: Number(x.full_every_days) || 0,
      retention: { chains: Number(x.retention.chains) || 0, days: Number(x.retention.days) || 0 },
    }))
    await put(`/agents/${s.id}/backup-schedules`, body)
    toast.success(t('Schedules saved on {server}.', { server: s.name }))
    loadSchedules()
  } catch (e) {
    toast.error(e.message)
  }
}
load().then(() => current.value && pick(current.value))
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <BackupTabs />
    <div class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[300px_1fr] lg:grid-rows-[minmax(0,1fr)]">
      <aside class="flex min-h-0 flex-col gap-5">
        <section class="card">
          <div class="card-h flex items-center justify-between">
            {{ t('Servers') }}
            <button class="act normal-case" :disabled="loading" @click="load">
              <RefreshCw :class="spin && 'motion-safe:animate-spin'" :size="12" /> {{ t('refresh') }}
            </button>
          </div>
          <ul class="side-nav divide-y divide-line text-[12px]">
            <li
              v-for="s in servers"
              :key="s.id"
              class="flex cursor-pointer items-center gap-2 px-3 py-2"
              :class="current?.id === s.id ? 'nav-on bg-nav-on text-on-nav-on shadow-(--shadow-nav-on)' : 'hover:bg-row-hover'"
              :aria-current="current?.id === s.id || null"
              @click="pick(s)"
            >
              <span
                class="h-2 w-2"
                :class="s.error ? 'bg-err-fill' : 'bg-ok-fill'"
                :title="s.error ? serverText(s.error) : t('online')"
              ></span>
              <span class="font-semibold">{{ s.id === 0 ? t('This panel') : s.name }}</span>
              <span v-if="s.dc" class="tag bg-surface-2 text-fg-muted">DC</span>
              <!-- on the selected item it keeps the selection's text color (muted there falls under 7:1 on Cristal) -->
              <span class="ml-auto" :class="current?.id === s.id ? '' : 'text-fg-muted'">{{ s.backups.length }}</span>
            </li>
            <li v-if="!servers.length && loading" class="px-3 py-3"><LoadingNote /></li>
            <li v-if="!servers.length && !loading" class="px-3 py-3 text-fg-muted">{{ t('No servers.') }}</li>
          </ul>
        </section>
        <p class="text-[11px] text-fg-muted">
          {{
            t(
              "Each agent makes and keeps its server's backups in its backup folder (default /var/backups/ad-manager); the schedules run on the agent, with the panel off too. A DC backup holds the domain's secrets.",
            )
          }}
        </p>
      </aside>

      <section v-if="current" class="flex min-h-0 flex-col gap-5">
        <div class="card flex min-h-0 flex-1 flex-col">
          <div class="card-h flex items-center gap-3">
            <Archive :size="14" /> {{ current.id === 0 ? t('This panel') : current.name }}
            <span class="font-normal normal-case text-fg-muted">{{ t('{n} backup(s)', { n: current.backups.length }) }}</span>
          </div>
          <p v-if="current.error" class="p-3 text-err">{{ serverText(current.error) }}</p>
          <p v-else-if="!current.kinds.length" class="p-3 text-fg-muted">
            {{ t('This server makes no backup: the agent is read only here (role not known).') }}
          </p>
          <form v-else class="flex flex-wrap items-end gap-2 p-3" @submit.prevent="backupNow">
            <div v-if="current.kinds.length > 1">
              <label class="lbl" for="bk">{{ t('Kind') }}</label>
              <select id="bk" v-model="now.kind" class="inp w-96">
                <option v-for="k in current.kinds" :key="k" :value="k">{{ kindName(k) }}</option>
              </select>
            </div>
            <div v-if="(now.kind || current.kinds[0]) === 'fileserver-data'" class="min-w-64 flex-1">
              <label class="lbl" for="bf">{{
                t('Folders (one per line, inside {roots})', { roots: (current.roots || []).join(', ') })
              }}</label>
              <textarea id="bf" v-model="now.folders" class="inp h-14 font-mono" required></textarea>
              <label class="mt-1 flex items-center gap-2 text-[12px]">
                <input v-model="now.full" type="checkbox" /> {{ t('Full backup (starts a new chain)') }}
              </label>
            </div>
            <button class="btn" :disabled="busy"><Archive :size="14" /> {{ busy ? '…' : t('Back up now') }}</button>
            <button v-if="current.upload_kinds?.length" type="button" class="btn-ghost" @click="sending = true">
              <Upload :size="14" /> {{ t('Send a backup') }}
            </button>
          </form>
          <div
            v-if="current.restore"
            class="flex flex-wrap items-center gap-2 border-t border-line px-3 py-2 text-[12px]"
            :class="current.restore.status === 'error' ? 'text-err' : current.restore.status === 'ok' ? 'text-ok' : 'text-warn'"
          >
            <History :size="13" />
            <span class="font-semibold">{{ t('Last restoration') }}:</span>
            <span class="text-fg">{{ kindName(current.restore.kind) }} · {{ formatDate(current.restore.started) }}</span>
            <span v-if="current.restore.dest" class="font-mono text-fg-muted">→ {{ current.restore.dest }}</span>
            <span>{{
              current.restore.status === 'ok'
                ? t('ok')
                : current.restore.status === 'error'
                  ? serverText(current.restore.error)
                  : t('running')
            }}</span>
            <button class="act ml-auto px-0" :title="t('Log')" @click="showRestoreLog(current.restore)"><ScrollText :size="13" /></button>
          </div>
          <div class="min-h-0 flex-1 overflow-auto">
            <table class="grid-t border-x-0">
              <thead>
                <tr>
                  <th>{{ t('Date') }}</th>
                  <th>{{ t('Kind') }}</th>
                  <th>{{ t('Origin') }}</th>
                  <th class="text-right">{{ t('Size') }}</th>
                  <th>{{ t('Status') }}</th>
                  <th class="w-40"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="b in current.backups" :key="b.id">
                  <td class="whitespace-nowrap" :title="b.id">{{ formatDate(b.started) }}</td>
                  <td>
                    {{ kindName(b.kind) }}
                    <span v-if="b.kind === 'fileserver-data'" class="text-fg-muted">· {{ b.full ? t('full') : t('incremental') }}</span>
                  </td>
                  <td class="text-fg-muted">{{ origin(b.origin) }}{{ b.by ? ' · ' + b.by : '' }}</td>
                  <td class="text-right tabular-nums">{{ b.status === 'ok' ? size(b.size) : '—' }}</td>
                  <td :class="b.status === 'ok' ? 'text-ok' : b.status === 'error' ? 'text-err' : 'text-warn'">
                    {{ b.status === 'ok' ? t('ok') : b.status === 'error' ? t('error') : t('running') }}
                  </td>
                  <td class="space-x-2 text-right whitespace-nowrap">
                    <button class="act px-0" :title="t('Log')" @click="showLog(b)"><ScrollText :size="13" /></button>
                    <button v-if="b.status === 'ok'" class="act px-0" :title="t('Download')" @click="download(b)">
                      <Download :size="13" />
                    </button>
                    <button v-if="b.status === 'ok'" class="act px-0" :title="t('Restore')" @click="openRestore(b)">
                      <RotateCcw :size="13" />
                    </button>
                    <button class="act px-0" :title="t('Delete')" @click="remove(b)"><Trash2 :size="13" /></button>
                  </td>
                </tr>
                <tr v-if="!current.backups.length">
                  <td colspan="6" class="py-3 text-center text-fg-muted">{{ t('No backups yet.') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="schedules" class="card">
          <div class="card-h flex items-center gap-3">
            <CalendarClock :size="14" /> {{ t('Schedules') }}
            <button class="act ml-auto normal-case" @click="addSchedule"><Plus :size="12" /> {{ t('new') }}</button>
          </div>
          <div class="overflow-auto">
            <table class="grid-t border-x-0">
              <thead>
                <tr>
                  <th>{{ t('Name') }}</th>
                  <th>{{ t('Kind') }}</th>
                  <th>{{ t('When (cron)') }}</th>
                  <th>{{ t('Keep chains') }}</th>
                  <th>{{ t('Keep days') }}</th>
                  <th>{{ t('New full every (days)') }}</th>
                  <th>{{ t('On') }}</th>
                  <th class="w-8"></th>
                </tr>
              </thead>
              <tbody>
                <template v-for="(sc, i) in schedules" :key="i">
                  <tr>
                    <td><input v-model.trim="sc.id" class="inp w-32 font-mono" :aria-label="t('Name')" /></td>
                    <td>
                      <select v-model="sc.kind" class="inp w-52" :aria-label="t('Kind')">
                        <option v-for="k in current.kinds" :key="k" :value="k">{{ kindName(k) }}</option>
                      </select>
                    </td>
                    <td>
                      <input v-model.trim="sc.cron" class="inp w-28 font-mono" list="cron-presets" :aria-label="t('When (cron)')" />
                    </td>
                    <td><input v-model="sc.retention.chains" type="number" min="0" class="inp w-16" :aria-label="t('Keep chains')" /></td>
                    <td><input v-model="sc.retention.days" type="number" min="0" class="inp w-16" :aria-label="t('Keep days')" /></td>
                    <td>
                      <input
                        v-model="sc.full_every_days"
                        type="number"
                        min="0"
                        class="inp w-16"
                        :disabled="sc.kind !== 'fileserver-data'"
                        :aria-label="t('New full every (days)')"
                      />
                    </td>
                    <td><input v-model="sc.enabled" type="checkbox" :aria-label="t('On')" /></td>
                    <td>
                      <button class="act px-0" :title="t('Delete')" @click="schedules.splice(i, 1)"><Trash2 :size="13" /></button>
                    </td>
                  </tr>
                  <tr v-if="sc.kind === 'fileserver-data'">
                    <td colspan="8">
                      <textarea
                        v-model="sc.foldersText"
                        class="inp h-12 font-mono"
                        :placeholder="t('Folders (one per line, inside {roots})', { roots: (current.roots || []).join(', ') })"
                      ></textarea>
                    </td>
                  </tr>
                </template>
                <tr v-if="!schedules.length">
                  <td colspan="8" class="py-3 text-center text-fg-muted">{{ t('No schedules.') }}</td>
                </tr>
              </tbody>
            </table>
            <datalist id="cron-presets">
              <option v-for="[c, label] in presets" :key="c" :value="c">{{ t(label) }}</option>
            </datalist>
          </div>
          <div class="flex items-center gap-2 border-t border-line p-3">
            <button class="btn" @click="saveSchedules"><Save :size="14" /> {{ t('Save schedules') }}</button>
            <span class="text-[11px] text-fg-muted">
              {{ t('Retention deletes whole chains only (a full backup and its incrementals), never the backup just made; 0 = no limit.') }}
            </span>
          </div>
        </div>
      </section>
    </div>
  </div>

  <RestoreBackupDialog
    v-if="restoring"
    :backup="restoring"
    :base="base(current)"
    :kind-name="kindName"
    @close="restoring = null"
    @done="restored"
  />

  <SendBackupDialog
    v-if="sending"
    :base="base(current)"
    :kinds="current.upload_kinds"
    :max-mib="current.upload_max_mib"
    :kind-name="kindName"
    @close="sending = false"
    @done="sent"
  />
</template>
