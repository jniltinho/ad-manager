<script setup>
// Sync wizard, four steps: source and access (with a step-by-step connection test) → what to copy (the source's
// shares and folders, with sizes) → where to (this server's folders, checked as they are picked) → how and when,
// with the summary. Saving starts a preview that is followed here; from it the job runs.
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Circle,
  Folder,
  FolderOpen,
  FolderPlus,
  HardDrive,
  KeyRound,
  Loader2,
  Play,
  PlugZap,
  Save,
  Server,
  XCircle,
} from 'lucide-vue-next'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { enc, get, post, put } from '../../api'
import { serverText, t } from '../../i18n'
import { toast } from '../../toasts'
import Modal from '../Modal.vue'

const props = defineProps({
  agent: { type: Number, required: true },
  roots: { type: Array, default: () => [] },
  job: { type: Object, default: null },
  kerberos: { type: Boolean, default: true }, // this server can sign in with its machine account
  machine: { type: String, default: '' }, // FS2$
  // @saved: a callback prop, so a save or run that ends after the dialog closed still reaches the page
  onSaved: { type: Function, default: null },
})
const emit = defineEmits(['close'])
const base = `/agents/${props.agent}`
const editing = !!props.job

const j = ref(
  props.job
    ? { ...JSON.parse(JSON.stringify(props.job)), password: '', excludeText: (props.job.exclude || []).join('\n') }
    : {
        id: '',
        name: '',
        source: { host: '', share: '', path: '', auth: props.kerberos ? 'kerberos' : 'ntlm', user: '', domain: '' },
        password: '',
        password_file: '',
        dest: '',
        mode: 'copy',
        excludeText: 'Thumbs.db\n~$*\n*.tmp',
        bwlimit: '',
        transfers: 4,
        max_delete: 100,
        max_delete_pct: 20,
        keep_days: 30,
        keep_gib: 0,
        verify: true,
        reinherit: true,
        cron: '',
        enabled: false,
      },
)
const steps = ['Source and access', 'What to copy', 'Where to', 'How and when', 'Preview of the copy']
const step = ref(editing ? 3 : 0)
const reached = ref(editing ? 3 : 0)
if (j.value.keep_days == null) j.value.keep_days = 30 // 0 and missing mean the agent's default, 30
const busy = ref(false)
const error = ref('')
const body = ref(null)
function go(n) {
  step.value = n
  reached.value = Math.max(reached.value, n)
  error.value = ''
  nextTick(() => body.value?.querySelector('input:not([type=checkbox]):not([disabled]), select')?.focus())
}
nextTick(() => go(step.value))
const creds = (extra = {}) => ({
  source: { ...j.value.source, ...extra },
  password: j.value.password,
  password_file: j.value.password_file,
  job: editing ? j.value.id : '',
})

// ── 1. source and access
const isIP = computed(() => /^\d{1,3}(\.\d{1,3}){3}$/.test(j.value.source.host))
function splitUser() {
  const u = j.value.source.user.trim()
  let m = u.match(/^([^\\]+)\\(.+)$/)
  if (m) Object.assign(j.value.source, { domain: m[1], user: m[2] })
  m = u.match(/^([^@]+)@(.+)$/)
  if (m) Object.assign(j.value.source, { user: m[1], domain: m[2] })
}
const test = ref(null) // { key, steps, ok }
const accessKey = computed(() =>
  JSON.stringify([j.value.source.host, j.value.source.auth, j.value.source.user, j.value.source.domain, j.value.password]),
)
const testNames = {
  credentials: 'Credentials',
  resolve: 'Name resolved',
  port: 'Port 445 answers',
  signin: 'Signed in (signed SMB)',
}
async function runTest() {
  busy.value = true
  error.value = ''
  try {
    const r = await post(`${base}/syncs/browse`, { ...creds({ share: '', path: '' }), check: true })
    const st = r.steps || []
    test.value = { key: accessKey.value, steps: st, ok: st.length > 0 && st.every((s) => s.ok) && st.at(-1).name === 'signin' }
  } catch (e) {
    test.value = { key: accessKey.value, steps: [], ok: false }
    error.value = e.message
  }
  busy.value = false
  return test.value.ok
}
const stepDetail = (s) => {
  if (s.name === 'signin') return t('{n} shares visible', { n: (s.items || []).length })
  if (s.name === 'port') return ''
  return s.detail
}
const missingSteps = computed(() => Object.keys(testNames).filter((n) => !test.value?.steps.some((s) => s.name === n)))
const shares = ref(null)
async function toSource() {
  if (!(test.value?.key === accessKey.value && test.value.ok) && !(await runTest())) return
  shares.value = test.value.steps.find((s) => s.name === 'signin')?.items || []
  if (j.value.source.share) await open(j.value.source.path)
  go(1)
}

// ── 2. what to copy
const folders = ref(null)
function toShares() {
  j.value.source.share = ''
  j.value.source.path = ''
  folders.value = null
}
async function pickShare(s) {
  j.value.source.share = s
  await open('')
}
async function open(path) {
  busy.value = true
  error.value = ''
  try {
    const r = await post(`${base}/syncs/browse`, creds({ path }))
    j.value.source.path = path
    folders.value = r.dirs
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
const sub = (f) => (j.value.source.path ? j.value.source.path + '/' + f : f)
const crumbs = computed(() => (j.value.source.path ? j.value.source.path.split('/') : []))
const unc = computed(() => {
  const s = j.value.source
  return `\\\\${s.host}${s.share ? '\\' + s.share : ''}${s.path ? '\\' + s.path.replaceAll('/', '\\') : ''}`
})
const sizes = ref({}) // "share/path" → 'busy' | { files, bytes } | error text
const sizeKey = (path) => j.value.source.share + '/' + path
async function sizeOf(path) {
  const k = sizeKey(path)
  if (sizes.value[k]) return
  sizes.value[k] = 'busy'
  try {
    sizes.value[k] = await post(`${base}/syncs/browse`, { ...creds({ path }), size: true })
  } catch (e) {
    sizes.value[k] = serverText(e.message)
  }
}
const bytes = (n) =>
  n >= 1 << 30
    ? (n / (1 << 30)).toFixed(1) + ' GiB'
    : n >= 1 << 20
      ? (n / (1 << 20)).toFixed(1) + ' MiB'
      : Math.ceil((n || 0) / 1024) + ' KiB'
function sizeText(path) {
  const v = sizes.value[sizeKey(path)]
  if (!v) return ''
  if (v === 'busy') return t('counting…')
  if (typeof v === 'string') return v
  return t('{n} files · {size}', { n: v.files.toLocaleString(), size: bytes(v.bytes) })
}
const sourceSize = computed(() => {
  const v = sizes.value[sizeKey(j.value.source.path)]
  return v && typeof v === 'object' ? v : null
})
const parentOf = (p) => p.slice(0, p.lastIndexOf('/')) || ''
function toDestination() {
  sizeOf(j.value.source.path) // counted while the destination is picked
  if (!j.value.dest) {
    const last = (j.value.source.path || j.value.source.share).split('/').pop()
    j.value.dest = `${props.roots[0] || '/srv/shares'}/${last}`
  }
  browseDest(parentOf(j.value.dest) || props.roots[0] || '/srv/shares')
  go(2)
}

// ── 3. where to
const destDir = ref('')
const destDirs = ref([])
async function browseDest(p) {
  busy.value = true
  try {
    const r = await get(`${base}/dirs?path=${enc(p)}`)
    destDir.value = p
    destDirs.value = r.dirs || []
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
const inRoot = computed(() => props.roots.find((r) => destDir.value === r || destDir.value.startsWith(r + '/')))
const destUp = computed(() => (inRoot.value && destDir.value !== inRoot.value ? parentOf(destDir.value) : ''))
const newFolder = ref(null)
function addFolder() {
  const n = (newFolder.value || '').trim().replace(/[\\/]/g, '')
  if (!n) return
  j.value.dest = destDir.value + '/' + n
  newFolder.value = null
}
const destCheck = ref(null)
let destTimer = null
watch(
  () => j.value.dest,
  (d) => {
    clearTimeout(destTimer)
    destCheck.value = null
    if (!d) return
    destTimer = setTimeout(async () => {
      try {
        const r = await post(`${base}/syncs/validate`, { dest: d })
        if (j.value.dest === d) destCheck.value = r.dest
      } catch (e) {
        destCheck.value = { ok: false, error: e.message }
      }
    }, 300)
  },
  { immediate: true },
)

// ── 4. how and when
const presets = [
  ['', 'Only by hand'],
  ['0 1 * * *', 'Every day at 01:00'],
  ['0 */4 * * *', 'Every 4 hours'],
  ['0 2 * * 0', 'Sundays at 02:00'],
]
const custom = ref(!!j.value.cron && !presets.some(([c]) => c === j.value.cron))
function pickWhen(v) {
  custom.value = v === 'custom'
  if (!custom.value) j.value.cron = v
}
const cronCheck = ref(null)
let cronTimer = null
watch(
  () => j.value.cron,
  (c) => {
    clearTimeout(cronTimer)
    cronCheck.value = null
    if (!c) return
    cronTimer = setTimeout(async () => {
      try {
        const r = await post(`${base}/syncs/validate`, { cron: c })
        if (j.value.cron === c) cronCheck.value = r.cron
      } catch (e) {
        cronCheck.value = { ok: false, error: e.message }
      }
    }, 300)
  },
  { immediate: true },
)
const when = (d) =>
  new Date(d).toLocaleString(undefined, { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
const quarantine = computed(() => `${props.roots.find((r) => j.value.dest.startsWith(r + '/')) || '/srv/shares'}/_sync/`)
const mirrorWord = computed(() => t('MIRROR'))
const confirmText = ref('')
const idTouched = ref(false)
const slug = (s) =>
  s
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
function toHow() {
  if (!j.value.name) j.value.name = (j.value.source.path || j.value.source.share).split('/').pop() + ' · ' + j.value.source.host
  if (!editing && !idTouched.value) j.value.id = slug(j.value.name) || 'sync-' + Date.now().toString(36)
  go(3)
}
watch(
  () => j.value.name,
  (n) => {
    if (!editing && !idTouched.value && step.value === 3) j.value.id = slug(n) || j.value.id
  },
)
const canSave = computed(
  () =>
    j.value.name &&
    j.value.id &&
    destCheck.value?.ok !== false &&
    (!j.value.cron || cronCheck.value?.ok !== false) &&
    (j.value.mode !== 'mirror' || confirmText.value.trim().toUpperCase() === mirrorWord.value.toUpperCase()),
)

// ── save, then the preview followed here
const run = ref(null) // the preview run, as the agent reports it
let pollTimer = null
let closed = false // the dialog went away (✕, Esc) while a request ran
onUnmounted(() => {
  closed = true
  clearTimeout(destTimer)
  clearTimeout(cronTimer)
  clearTimeout(pollTimer)
})
async function save(preview) {
  busy.value = true
  error.value = ''
  try {
    const { excludeText, password_set, approved_rev, ...job } = j.value
    job.exclude = excludeText
      .split('\n')
      .map((x) => x.trim())
      .filter(Boolean)
    job.transfers = Number(job.transfers) || 0
    job.max_delete = Number(job.max_delete) || 0
    job.max_delete_pct = Number(job.max_delete_pct) || 0
    job.keep_days = Number(job.keep_days) || 0
    job.keep_gib = Number(job.keep_gib) || 0
    job.enabled = !!job.cron
    job.confirm = job.mode === 'mirror' ? job.id : '' // the word was typed: the agent wants the job's id
    if (job.source.auth !== 'ntlm') Object.assign(job, { password: '', password_file: '' })
    await put(`${base}/syncs/${enc(job.id)}`, job)
    if (!preview) {
      toast.success(t('Sync job saved.'))
      busy.value = false
      return props.onSaved?.()
    }
    run.value = await post(`${base}/syncs/${enc(job.id)}/preview`)
    // saved and previewing, but this dialog is gone: the page only lists it again (another wizard may be open)
    if (closed) return props.onSaved?.({ closed: true })
    step.value = 4
    poll()
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
async function poll() {
  try {
    const d = await get(`${base}/syncs`)
    const r = d.runs?.find((x) => x.id === run.value.id)
    if (r) run.value = r
  } catch {
    /* tried again below */
  }
  if (['queued', 'running'].includes(run.value.status)) pollTimer = setTimeout(poll, 2000)
}
async function runNow() {
  busy.value = true
  try {
    await post(`${base}/syncs/${enc(j.value.id)}/run`)
    toast.success(t('Sync queued.'))
    props.onSaved?.()
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
const plan = computed(() => run.value?.result?.plan)
const running = computed(() => ['queued', 'running'].includes(run.value?.status))

// Enter in a field moves on (the Modal's form submits)
function onEnter() {
  if (busy.value) return
  if (step.value === 0) toSource()
  else if (step.value === 2 && destCheck.value?.ok) toHow()
  else if (step.value === 3 && canSave.value) save(true)
}
</script>

<template>
  <Modal
    :title="editing ? t('Edit {name}', { name: j.name || j.id }) : t('New sync job')"
    wide
    :ok="null"
    :foot="false"
    @close="emit('close')"
    @submit="onEnter"
  >
    <!-- the steps, as the setup wizard: done ones can be opened again (not from the preview) -->
    <nav :aria-label="t('Wizard steps')" class="mb-4">
      <ol class="flex flex-wrap gap-x-4 gap-y-2 text-[12px]">
        <li v-for="(s, i) in steps" :key="s" :aria-current="i === step ? 'step' : undefined">
          <button
            type="button"
            class="flex items-center gap-2 disabled:cursor-default"
            :disabled="i > reached || i === step || busy || step === 4"
            @click="go(i)"
          >
            <span
              class="grid size-6 place-items-center border text-[12px] font-semibold"
              :class="
                i < step
                  ? 'border-ok-fill bg-ok-fill text-on-ok-fill'
                  : i === step
                    ? 'border-action bg-action text-on-action'
                    : 'border-line text-fg-muted'
              "
            >
              <Check v-if="i < step" :size="14" :aria-label="t('done')" />
              <template v-else>{{ i + 1 }}</template>
            </span>
            <span
              :class="
                i === step
                  ? 'font-semibold text-fg-strong'
                  : i <= reached && step < 4
                    ? 'text-fg underline-offset-2 hover:underline'
                    : 'text-fg-muted'
              "
              >{{ t(s) }}</span
            >
          </button>
        </li>
      </ol>
      <div class="mt-3 h-1 bg-surface-2" aria-hidden="true">
        <div class="h-full bg-action transition-[width] duration-300" :style="{ width: `${(step / (steps.length - 1)) * 100}%` }" />
      </div>
      <h3 class="mt-3 text-[13px] font-semibold text-fg-strong">
        {{ t('Step {n} of {total}: {name}', { n: step + 1, total: steps.length, name: t(steps[step]) }) }}
      </h3>
    </nav>

    <div ref="body" class="min-h-80 text-[12px]">
      <!-- 1. source and access -->
      <div v-if="step === 0" class="grid gap-3 md:grid-cols-2">
        <div class="md:col-span-2">
          <label class="lbl" for="w-host">{{ t('Source server') }}</label>
          <input
            id="w-host"
            v-model.trim="j.source.host"
            class="inp font-mono"
            required
            placeholder="old-fs.example.local"
            autocomplete="off"
          />
          <p v-if="isIP && j.source.auth === 'kerberos'" class="mt-1 flex items-center gap-1 text-warn">
            <AlertTriangle :size="13" />
            {{ t('Kerberos needs the full name (old-fs.example.local); with an IP use user and password.') }}
          </p>
        </div>
        <div class="grid gap-2 md:col-span-2 md:grid-cols-2" role="radiogroup" :aria-label="t('How to sign in')">
          <button
            type="button"
            role="radio"
            :aria-checked="j.source.auth === 'kerberos'"
            :disabled="!kerberos"
            class="border p-3 text-left disabled:opacity-60"
            :class="j.source.auth === 'kerberos' ? 'border-focus bg-surface-2' : 'border-line hover:bg-row-hover'"
            @click="j.source.auth = 'kerberos'"
          >
            <span class="flex items-center gap-2 font-semibold"
              ><Server :size="14" /> {{ t("This server's machine account (Kerberos)") }}</span
            >
            <span class="mt-1 block text-[11px] text-fg-muted">
              <template v-if="kerberos">{{
                t('Same domain, no password kept. Give {machine} (or Domain Computers) read permission on the share.', {
                  machine: machine || 'FS01$',
                })
              }}</template>
              <template v-else>{{ t('Not available: this server is not joined to a domain.') }}</template>
            </span>
          </button>
          <button
            type="button"
            role="radio"
            :aria-checked="j.source.auth === 'ntlm'"
            class="border p-3 text-left"
            :class="j.source.auth === 'ntlm' ? 'border-focus bg-surface-2' : 'border-line hover:bg-row-hover'"
            @click="j.source.auth = 'ntlm'"
          >
            <span class="flex items-center gap-2 font-semibold"><KeyRound :size="14" /> {{ t('User and password') }}</span>
            <span class="mt-1 block text-[11px] text-fg-muted">{{
              t('Another domain or a standalone server; the password is kept encrypted.')
            }}</span>
          </button>
        </div>
        <template v-if="j.source.auth === 'ntlm'">
          <div>
            <label class="lbl" for="w-user">{{ t('User') }}</label>
            <input
              id="w-user"
              v-model.trim="j.source.user"
              class="inp"
              required
              autocomplete="off"
              placeholder="DOMAIN\user"
              @blur="splitUser"
            />
          </div>
          <div>
            <label class="lbl" for="w-dom">{{ t('Domain (or the server name)') }}</label>
            <input id="w-dom" v-model.trim="j.source.domain" class="inp" autocomplete="off" />
          </div>
          <div class="md:col-span-2">
            <label class="lbl" for="w-pw">{{ t('Password') }}</label>
            <input
              id="w-pw"
              v-model="j.password"
              type="password"
              class="inp"
              autocomplete="new-password"
              :placeholder="editing && (j.password_set || j.password_file) ? t('kept (type to change)') : ''"
            />
          </div>
        </template>

        <div v-if="test" class="border border-line p-2 md:col-span-2" role="status" aria-live="polite">
          <div class="lbl mb-1">{{ t('Connection test') }}</div>
          <ul class="space-y-1">
            <li v-for="s in test.steps" :key="s.name" class="flex items-start gap-2">
              <CheckCircle2 v-if="s.ok" :size="14" class="mt-0.5 shrink-0 text-ok" />
              <XCircle v-else :size="14" class="mt-0.5 shrink-0 text-err" />
              <div>
                <span class="font-semibold">{{ t(testNames[s.name] || s.name) }}</span>
                <span v-if="s.ok && stepDetail(s)" class="text-fg-muted"> · {{ stepDetail(s) }}</span>
                <div v-if="!s.ok" class="text-err">{{ serverText(s.hint || s.detail) }}</div>
                <div v-if="!s.ok && s.hint" class="font-mono text-[10px] break-all text-fg-muted">{{ s.detail }}</div>
              </div>
            </li>
            <li v-for="n in missingSteps" :key="n" class="flex items-center gap-2 text-fg-muted">
              <Circle :size="14" /> {{ t(testNames[n]) }} · {{ t('not reached') }}
            </li>
          </ul>
        </div>
        <p v-if="error" class="text-err md:col-span-2" role="alert">{{ serverText(error) }}</p>
        <div class="flex flex-wrap justify-between gap-2 border-t border-line pt-3 md:col-span-2">
          <button type="button" class="btn-ghost" :disabled="busy || !j.source.host" @click="runTest">
            <component :is="busy ? Loader2 : PlugZap" :size="14" :class="busy && 'motion-safe:animate-spin'" /> {{ t('Test connection') }}
          </button>
          <button type="button" class="btn" :disabled="busy || !j.source.host" @click="toSource">
            {{ t('Next') }} <ChevronRight :size="14" />
          </button>
        </div>
      </div>

      <!-- 2. what to copy -->
      <div v-else-if="step === 1" class="space-y-3">
        <p class="text-fg-muted">
          {{ t('Open the share and the folder to copy; "size" counts what is inside (it can take a while on large folders).') }}
        </p>
        <div class="flex flex-wrap items-center gap-1 font-mono">
          <button type="button" class="act px-0" @click="toShares">\\{{ j.source.host }}</button>
          <template v-if="j.source.share">
            <span class="text-fg-muted">\</span>
            <button type="button" class="act px-0" @click="open('')">{{ j.source.share }}</button>
            <template v-for="(c, i) in crumbs" :key="i">
              <span class="text-fg-muted">\</span>
              <button type="button" class="act px-0" @click="open(crumbs.slice(0, i + 1).join('/'))">{{ c }}</button>
            </template>
          </template>
          <Loader2 v-if="busy" :size="13" class="motion-safe:animate-spin text-fg-muted" />
        </div>
        <ul class="max-h-64 divide-y divide-line overflow-auto border border-line">
          <template v-if="!j.source.share">
            <li v-for="s in shares || []" :key="s">
              <button type="button" class="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-row-hover" @click="pickShare(s)">
                <HardDrive :size="14" class="text-fg-muted" /> {{ s }}
              </button>
            </li>
            <li v-if="shares && !shares.length" class="px-3 py-2 text-fg-muted">{{ t('No shares this account can see.') }}</li>
          </template>
          <template v-else>
            <li v-for="f in folders || []" :key="f" class="flex items-center">
              <button type="button" class="flex flex-1 items-center gap-2 px-3 py-1.5 text-left hover:bg-row-hover" @click="open(sub(f))">
                <Folder :size="14" class="text-fg-muted" /> {{ f }}
              </button>
              <span class="px-2 text-[11px] text-fg-muted tabular-nums">{{ sizeText(sub(f)) }}</span>
              <button
                v-if="!sizes[sizeKey(sub(f))]"
                type="button"
                class="act mr-2"
                :title="t('Count the files and bytes (walks the whole folder)')"
                @click="sizeOf(sub(f))"
              >
                {{ t('size') }}
              </button>
            </li>
            <li v-if="folders && !folders.length" class="px-3 py-2 text-fg-muted">
              {{ t('No folders inside: this one can be the source.') }}
            </li>
          </template>
        </ul>
        <p v-if="error" class="text-err" role="alert">{{ serverText(error) }}</p>
        <div class="flex flex-wrap items-center gap-2 border-t border-line pt-3">
          <button type="button" class="btn-ghost mr-auto" @click="go(0)"><ChevronLeft :size="14" /> {{ t('Back') }}</button>
          <span v-if="j.source.share" class="text-[11px] text-fg-muted tabular-nums">{{ sizeText(j.source.path) }}</span>
          <button type="button" class="btn" :disabled="!j.source.share || busy" @click="toDestination">
            <FolderOpen :size="14" /> {{ t('Copy {path}', { path: unc }) }} <ChevronRight :size="14" />
          </button>
        </div>
      </div>

      <!-- 3. where to -->
      <div v-else-if="step === 2" class="space-y-3">
        <p class="text-fg-muted">{{ t('Open a folder of this server and use it, or make a new one.') }}</p>
        <div class="flex flex-wrap items-center gap-2 font-mono">
          <button v-for="r in roots" :key="r" type="button" class="act px-0" @click="browseDest(r)">{{ r }}</button>
          <template v-if="destDir && destDir !== inRoot"
            ><span class="text-fg-muted">→</span> {{ destDir.slice((inRoot || '').length) }}</template
          >
          <Loader2 v-if="busy" :size="13" class="motion-safe:animate-spin text-fg-muted" />
        </div>
        <ul class="max-h-52 divide-y divide-line overflow-auto border border-line">
          <li v-if="destUp">
            <button
              type="button"
              class="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-row-hover"
              @click="browseDest(destUp)"
            >
              <ChevronLeft :size="14" class="text-fg-muted" /> ..
            </button>
          </li>
          <li v-for="d in destDirs" :key="d" class="flex items-center" :class="j.dest === d && 'bg-surface-2'">
            <button type="button" class="flex flex-1 items-center gap-2 px-3 py-1.5 text-left hover:bg-row-hover" @click="browseDest(d)">
              <Folder :size="14" class="text-fg-muted" /> {{ d.split('/').pop() }}
            </button>
            <Check v-if="j.dest === d" :size="14" class="mr-2 text-ok" />
            <button v-else type="button" class="act mr-2" @click="j.dest = d">{{ t('use') }}</button>
          </li>
          <li v-if="!destDirs.length && !busy" class="px-3 py-2 text-fg-muted">{{ t('No folders inside.') }}</li>
        </ul>
        <div class="flex flex-wrap items-center gap-2">
          <button v-if="destDir && destDir !== inRoot && j.dest !== destDir" type="button" class="btn-ghost" @click="j.dest = destDir">
            <Check :size="14" /> {{ t('Use this folder') }}
          </button>
          <button v-if="newFolder === null" type="button" class="btn-ghost" @click="newFolder = ''">
            <FolderPlus :size="14" /> {{ t('New folder…') }}
          </button>
          <template v-else>
            <input
              v-model="newFolder"
              class="inp w-56"
              :placeholder="t('name of the new folder')"
              :aria-label="t('New folder…')"
              @keydown.enter.prevent="addFolder"
            />
            <button type="button" class="btn-ghost" @click="addFolder">{{ t('use') }}</button>
          </template>
        </div>
        <div>
          <label class="lbl" for="w-dest">{{ t('Destination folder') }}</label>
          <input id="w-dest" v-model.trim="j.dest" class="inp font-mono" required />
        </div>
        <div v-if="destCheck" class="space-y-1 text-[11px]" role="status" aria-live="polite">
          <p v-if="!destCheck.ok" class="text-err">{{ serverText(destCheck.error) }}</p>
          <template v-else>
            <p class="text-ok">
              <CheckCircle2 :size="12" class="inline" />
              {{ destCheck.exists ? t('The folder exists') : t('The folder is made when the job runs') }}
              <template v-if="destCheck.share"> · {{ t('shared as {share}', { share: destCheck.share }) }}</template>
              <template v-if="destCheck.free_bytes != null">
                · {{ t('{free} free', { free: bytes(destCheck.free_bytes) }) }}
                <template v-if="sourceSize"> ({{ t('the source has {size}', { size: bytes(sourceSize.bytes) }) }})</template>
              </template>
            </p>
            <p v-if="sourceSize && destCheck.free_bytes != null && sourceSize.bytes > destCheck.free_bytes" class="text-err">
              {{ t('Not enough free space for the source.') }}
            </p>
            <p v-if="destCheck.items" class="text-warn">
              <AlertTriangle :size="12" class="inline" />
              {{
                t('The folder already has {n} items: files of the same name are replaced (the old ones go to the quarantine).', {
                  n: destCheck.items,
                })
              }}
            </p>
          </template>
        </div>
        <p v-if="error" class="text-err" role="alert">{{ serverText(error) }}</p>
        <div class="flex justify-between gap-2 border-t border-line pt-3">
          <button type="button" class="btn-ghost" @click="go(1)"><ChevronLeft :size="14" /> {{ t('Back') }}</button>
          <button type="button" class="btn" :disabled="!j.dest || destCheck?.ok !== true" @click="toHow">
            {{ t('Next') }} <ChevronRight :size="14" />
          </button>
        </div>
      </div>

      <!-- 4. how and when, with the summary -->
      <div v-else-if="step === 3" class="space-y-3">
        <div class="grid gap-2 md:grid-cols-2" role="radiogroup" :aria-label="t('Mode')">
          <button
            v-for="[m, title, help] in [
              ['copy', 'Copy (recommended)', 'Adds and updates; never deletes in the destination.'],
              ['mirror', 'Mirror', 'Also deletes in the destination what the source no longer has.'],
            ]"
            :key="m"
            type="button"
            role="radio"
            :aria-checked="j.mode === m"
            class="border p-3 text-left"
            :class="j.mode === m ? 'border-focus bg-surface-2' : 'border-line hover:bg-row-hover'"
            @click="j.mode = m"
          >
            <span class="block font-semibold">{{ t(title) }}</span>
            <span class="block text-[11px] text-fg-muted">{{ t(help) }}</span>
          </button>
        </div>
        <div class="space-y-2 border border-line p-2 text-[11px]">
          <p>{{ t('Deleted and replaced files go to {dir} (root only), where they can be given back.', { dir: quarantine }) }}</p>
          <div class="flex flex-wrap items-center gap-2">
            {{ t('Keep them for') }}
            <input v-model="j.keep_days" type="number" min="0" max="3650" class="inp w-20" :aria-label="t('Days kept in the quarantine')" />
            {{ t('days and at most') }}
            <input v-model="j.keep_gib" type="number" min="0" class="inp w-20" :aria-label="t('GiB kept in the quarantine')" />
            {{ t('GiB (0: no limit); older ones are removed after each run.') }}
          </div>
        </div>
        <div v-if="j.mode === 'mirror'" class="space-y-2 border border-line p-2 text-[11px]">
          <div class="flex flex-wrap items-center gap-2">
            {{ t('Stop before deleting more than') }}
            <input v-model="j.max_delete" type="number" min="0" class="inp w-20" :aria-label="t('Most files deleted per run')" />
            {{ t('files or') }}
            <input
              v-model="j.max_delete_pct"
              type="number"
              min="0"
              max="100"
              class="inp w-16"
              :aria-label="t('Most deleted, % of the destination')"
            />
            {{ t('% of the destination.') }}
          </div>
          <p class="text-fg-muted">{{ t('A mirror runs by hand only after a preview of this version (saving makes one).') }}</p>
        </div>
        <div class="grid gap-3 md:grid-cols-2">
          <div>
            <label class="lbl" for="w-when">{{ t('When') }}</label>
            <select id="w-when" class="inp" :value="custom ? 'custom' : j.cron" @change="pickWhen($event.target.value)">
              <option v-for="[c, l] in presets" :key="c" :value="c">{{ t(l) }}</option>
              <option value="custom">{{ t('Custom (cron)…') }}</option>
            </select>
            <input v-if="custom" v-model.trim="j.cron" class="inp mt-1 font-mono" placeholder="30 22 * * 1-5" :aria-label="t('Cron')" />
            <p v-if="cronCheck && !cronCheck.ok" class="mt-1 text-[11px] text-err">
              {{ t('Not a valid schedule (min hour day month weekday).') }}
            </p>
            <p v-else-if="cronCheck?.next" class="mt-1 text-[11px] text-fg-muted">
              {{ t('Next: {when}', { when: cronCheck.next.map(when).join(' · ') }) }}
            </p>
          </div>
          <div>
            <label class="lbl" for="w-name">{{ t('Name') }}</label>
            <input id="w-name" v-model.trim="j.name" class="inp" maxlength="80" required />
          </div>
        </div>
        <table class="grid-t">
          <tbody>
            <tr>
              <th class="w-32">{{ t('Source') }}</th>
              <td>
                <span class="font-mono">{{ unc }}</span>
                <span class="text-fg-muted">
                  · {{ j.source.auth === 'ntlm' ? j.source.user + (j.source.domain ? '@' + j.source.domain : '') : t('machine account') }}
                  <template v-if="sourceSize">
                    · {{ t('{n} files · {size}', { n: sourceSize.files.toLocaleString(), size: bytes(sourceSize.bytes) }) }}</template
                  >
                </span>
              </td>
            </tr>
            <tr>
              <th>{{ t('Destination') }}</th>
              <td>
                <span class="font-mono">{{ j.dest }}</span>
                <span v-if="destCheck?.free_bytes != null" class="text-fg-muted">
                  · {{ t('{free} free', { free: bytes(destCheck.free_bytes) }) }}</span
                >
              </td>
            </tr>
            <tr>
              <th>{{ t('Mode') }}</th>
              <td>
                {{ j.mode === 'mirror' ? t('mirror') : t('copy') }} ·
                {{ j.cron ? (cronCheck?.next ? t('next {when}', { when: when(cronCheck.next[0]) }) : j.cron) : t('only by hand') }}
              </td>
            </tr>
          </tbody>
        </table>
        <details class="border border-line p-2">
          <summary class="cursor-pointer font-semibold">{{ t('Advanced') }}</summary>
          <div class="mt-2 grid gap-3 md:grid-cols-2">
            <div>
              <label class="lbl" for="w-ex">{{ t('Leave out (one pattern per line)') }}</label>
              <textarea id="w-ex" v-model="j.excludeText" class="inp h-20 font-mono"></textarea>
            </div>
            <div class="space-y-2">
              <div>
                <label class="lbl" for="w-bw">{{ t('Bandwidth limit (e.g. 10M, or 08:00,2M 19:00,off)') }}</label>
                <input id="w-bw" v-model.trim="j.bwlimit" class="inp font-mono" />
              </div>
              <div>
                <label class="lbl" for="w-tr">{{ t('Simultaneous transfers') }}</label>
                <input id="w-tr" v-model="j.transfers" type="number" min="1" max="16" class="inp" />
              </div>
            </div>
            <label class="flex items-center gap-2"
              ><input v-model="j.verify" type="checkbox" /> {{ t('Compare the content after the run (reads the source again)') }}</label
            >
            <label class="flex items-center gap-2"
              ><input v-model="j.reinherit" type="checkbox" />
              {{ t("Apply the destination folder's Windows permissions to what came") }}</label
            >
            <div v-if="!editing">
              <label class="lbl" for="w-id">{{ t('Id') }}</label>
              <input id="w-id" v-model.trim="j.id" class="inp font-mono" pattern="[a-z0-9][a-z0-9\-]{0,31}" @input="idTouched = true" />
            </div>
          </div>
        </details>
        <div v-if="j.mode === 'mirror'">
          <label class="lbl" for="w-confirm">{{
            t('A mirror deletes in the destination. Type {word} to confirm', { word: mirrorWord })
          }}</label>
          <input id="w-confirm" v-model="confirmText" class="inp w-48 font-mono" autocomplete="off" />
        </div>
        <p v-if="error" class="text-err" role="alert">{{ serverText(error) }}</p>
        <div class="flex flex-wrap items-center gap-2 border-t border-line pt-3">
          <button type="button" class="btn-ghost mr-auto" @click="go(2)"><ChevronLeft :size="14" /> {{ t('Back') }}</button>
          <button type="button" class="btn-ghost order-1" :disabled="busy || !canSave" @click="save(false)">
            <Save :size="14" /> {{ t('Only save') }}
          </button>
          <button type="button" class="btn order-2" :disabled="busy || !canSave" @click="save(true)">
            <component :is="busy ? Loader2 : Play" :size="14" :class="busy && 'motion-safe:animate-spin'" /> {{ t('Save and preview') }}
          </button>
        </div>
      </div>

      <!-- 5. the preview, followed here -->
      <div v-else class="space-y-3" role="status" aria-live="polite">
        <p class="flex items-center gap-2 font-semibold">
          <Loader2 v-if="running" :size="14" class="motion-safe:animate-spin" />
          <CheckCircle2 v-else-if="run?.status === 'ok'" :size="14" class="text-ok" />
          <XCircle v-else :size="14" class="text-err" />
          {{
            running
              ? t('Preview running: listing the source and the destination…')
              : run?.status === 'ok'
                ? t('Preview done: nothing was written yet.')
                : t('The preview failed.')
          }}
        </p>
        <p v-if="run?.error" class="text-err" role="alert">{{ serverText(run.error) }}</p>
        <table v-if="plan" class="grid-t">
          <tbody>
            <tr>
              <th class="w-40">{{ t('New') }}</th>
              <td>{{ plan.New }} ({{ bytes(plan.NewBytes) }})</td>
            </tr>
            <tr>
              <th>{{ t('Changed') }}</th>
              <td>{{ plan.Changed }} ({{ bytes(plan.ChangedBytes) }})</td>
            </tr>
            <tr v-if="j.mode === 'mirror'">
              <th>{{ t('To delete') }}</th>
              <td :class="plan.Delete && 'text-warn'">{{ plan.Delete }} ({{ bytes(plan.DeleteBytes) }})</td>
            </tr>
            <tr>
              <th>{{ t('Unchanged') }}</th>
              <td>{{ plan.Same }}</td>
            </tr>
          </tbody>
        </table>
        <template
          v-for="[g, label] in [
            ['new', 'New'],
            ['changed', 'Changed'],
            ['delete', 'To delete'],
          ]"
          :key="g"
        >
          <details v-if="plan?.sample?.[g]?.length">
            <summary class="cursor-pointer">{{ t(label) }} ({{ t('first {n}', { n: plan.sample[g].length }) }})</summary>
            <pre class="max-h-32 overflow-auto bg-surface-2 p-2 text-[11px]">{{ plan.sample[g].join('\n') }}</pre>
          </details>
        </template>
        <div class="flex flex-wrap justify-end gap-2 border-t border-line pt-3">
          <button type="button" class="btn-ghost" @click="props.onSaved?.()">{{ t('Close (the job is saved)') }}</button>
          <button type="button" class="btn" :disabled="busy || run?.status !== 'ok'" @click="runNow">
            <Play :size="14" /> {{ t('Run now') }}
          </button>
        </div>
      </div>
    </div>
  </Modal>
</template>
