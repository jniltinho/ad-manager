<script setup>
// Setup wizard of a fresh installation (config.toml: setup = true), in steps: sign in with the one-time token, check
// this server, choose a role, fill in the details (checked here before the review), review in plain words, install
// with a live progress. The agent on this server does the work.
import {
  ArchiveRestore,
  ArrowLeftRight,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  CirclePlus,
  CircleX,
  Copy,
  FolderTree,
  Info,
  KeyRound,
  Link,
  LoaderCircle,
  Network,
  Play,
  RefreshCw,
  TriangleAlert,
} from 'lucide-vue-next'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import { serverText, t } from '../i18n'
import { DC_ROLES, needsPassword as roleNeedsPassword, suggestNetbios, validate } from '../setupRules'

const STEPS = [
  { id: 'token', label: 'Access' },
  { id: 'server', label: 'This server' },
  { id: 'role', label: 'Role' },
  { id: 'details', label: 'Details' },
  { id: 'review', label: 'Overview' },
  { id: 'run', label: 'Installation' },
]
const step = ref('token')
const idx = computed(() => STEPS.findIndex((s) => s.id === step.value))
const busy = ref(false)
const error = ref('')
// a step already done can be opened again (not the token, nor anything once the installation started)
const canGo = (i) => i > 0 && i < idx.value && step.value !== 'run' && !busy.value
const heading = ref(null)
watch(step, async (now, before) => {
  error.value = ''
  // the password lives only while its fields are on screen (and through the review, which runs with it): back from
  // the review it is asked again
  if (before === 'review' && now !== 'run') password.value = confirm.value = ''
  await nextTick()
  heading.value?.focus() // the screen reader and the keyboard start at the new step's title
})

async function call(method, path, body) {
  error.value = ''
  return api(method, '/setup' + path, body)
}

// ── 1. access
const token = ref('')
const copied = ref(false)
const tokenCmd = 'sudo cat /opt/ad-manager/data/setup-token'
async function copyCmd() {
  try {
    await navigator.clipboard.writeText(tokenCmd)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    /* no clipboard (http): the command stays on screen */
  }
}
async function signIn() {
  busy.value = true
  try {
    await call('POST', '/login', { token: token.value.trim() })
    token.value = ''
    await diagnose()
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}

// ── 2. this server: the diagnosis as checks (ok, warning, problem) with what to do
const diag = ref(null)
async function diagnose() {
  busy.value = true
  try {
    diag.value = await call('GET', '/diagnose')
    if (step.value === 'token') step.value = 'server'
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
const checks = computed(() => {
  const d = diag.value
  if (!d) return []
  const gib = Math.floor(d.disk_free_mib / 1024)
  const used = Object.entries(d.ports_in_use || {})
    .filter(([, u]) => u)
    .map(([p]) => p)
  const missing = Object.entries(d.packages || {})
    .filter(([, ok]) => !ok)
    .map(([p]) => p)
  return [
    { level: 'ok', label: 'Name and address', text: `${d.fqdn} · ${(d.ips || []).join(', ')}` },
    { level: 'ok', label: 'System', text: d.os },
    d.clock_synced
      ? { level: 'ok', label: 'Clock', text: t('synchronized') }
      : {
          level: 'error',
          label: 'Clock',
          text: t('not synchronized'),
          fix: t('Kerberos needs the right time: run {cmd} and check again.', { cmd: 'sudo timedatectl set-ntp true' }),
        },
    gib < 2
      ? { level: 'error', label: 'Free disk', text: `${gib} GiB`, fix: t('Free at least 2 GiB in /var/lib (10 GiB or more for backups).') }
      : gib < 10
        ? { level: 'warn', label: 'Free disk', text: `${gib} GiB`, fix: t('Enough to install; backups need 10 GiB or more.') }
        : { level: 'ok', label: 'Free disk', text: `${gib} GiB` },
    d.samba_files
      ? {
          level: 'warn',
          label: 'Samba',
          text: [d.samba_role, d.realm].filter(Boolean).join(' · ') || t('existing files'),
          fix: t('Samba is already set up here: it is backed up before any change.'),
        }
      : { level: 'ok', label: 'Samba', text: t('not configured: a clean start') },
    used.length && !d.samba_files
      ? {
          level: 'warn',
          label: 'Ports',
          text: t('in use: {ports}', { ports: used.join(', ') }),
          fix: t('Another service uses them; a domain controller needs them free.'),
        }
      : { level: 'ok', label: 'Ports', text: t('free for the domain') },
    missing.length
      ? { level: 'info', label: 'Packages', text: t('installed by the setup: {list}', { list: missing.join(', ') }) }
      : { level: 'ok', label: 'Packages', text: t('all installed') },
  ]
})
const problems = computed(() => checks.value.filter((c) => c.level === 'error').length)
const LEVEL = {
  ok: { icon: CircleCheck, cls: 'text-ok', sr: 'OK' },
  info: { icon: Info, cls: 'text-fg-muted', sr: 'Note' },
  warn: { icon: TriangleAlert, cls: 'text-warn', sr: 'Attention' },
  error: { icon: CircleX, cls: 'text-err', sr: 'Problem' },
}

// ── 3. role
const ROLES = [
  {
    id: 'new-dc',
    icon: CirclePlus,
    name: 'New domain',
    help: 'This server becomes the first domain controller of a new Samba domain, with the panel.',
    when: 'For a company without a domain, or a fresh start.',
    recommended: true,
  },
  {
    id: 'dc-join',
    icon: Network,
    name: 'Additional DC',
    help: 'This server joins an existing domain (Windows or Samba) as one more domain controller, with the panel.',
    when: 'To add a second DC, or to migrate from an old Windows DC.',
  },
  {
    id: 'restore-dc',
    icon: ArchiveRestore,
    name: 'Restore a DC',
    help: 'This new server becomes the domain controller of a DC backup made by the panel (when the old DC is lost).',
    when: 'Only when the old DC is lost and you have its backup.',
  },
  {
    id: 'member',
    icon: FolderTree,
    name: 'File server',
    help: 'This server joins the domain as a member and serves shares through the agent.',
    when: 'For a server that only keeps the shared folders.',
  },
  {
    id: 'connect',
    icon: Link,
    name: 'Connect only',
    help: 'The panel manages an Active Directory that already runs elsewhere; nothing is installed here.',
    when: 'To manage an existing Active Directory from this panel.',
  },
]
const plan = ref({
  role: 'new-dc',
  realm: '',
  domain: '',
  ip: '',
  src_dc: '',
  dns_dc: '',
  url: 'ldaps://',
  insecure: false,
  admin: 'Administrator',
  forwarder: '1.1.1.1',
  share_root: '/srv/shares',
  files: true,
  backup: '',
  import_ad: false,
})
const roleInfo = computed(() => ROLES.find((r) => r.id === plan.value.role))
const isDC = computed(() => DC_ROLES.includes(plan.value.role))
const needsPassword = computed(() => roleNeedsPassword(plan.value.role))
// the import write modes are this DC agent's: only a new domain whose DC also serves files can have them
const importReady = computed(() => plan.value.role === 'new-dc' && plan.value.files && plan.value.import_ad)
const sent = computed(() => ({ ...plan.value, import_ad: importReady.value }))
function chooseRole() {
  if (!plan.value.ip && diag.value?.ips?.length) plan.value.ip = diag.value.ips[0]
  step.value = 'details'
}

// ── 4. details: the fields of the role, checked before the review
const password = ref('')
const confirm = ref('')
const showPassword = ref(false)
const errors = ref({})
const domainTouched = ref(false) // the NetBIOS name follows the DNS name until it is typed
watch(
  () => plan.value.realm,
  (r) => {
    if (!domainTouched.value) plan.value.domain = suggestNetbios(r)
  },
)
const fields = computed(() => {
  const r = plan.value.role
  const f = [
    {
      key: 'realm',
      label: 'Domain name (DNS)',
      hint: 'The name of the domain, e.g. company.local: computers and e-mail-like logins use it.',
      placeholder: 'example.local',
    },
  ]
  if (r !== 'connect' && r !== 'restore-dc')
    f.push({
      key: 'domain',
      label: 'Short name (NetBIOS)',
      hint: 'Up to 15 characters; Windows shows it as NAME\\user. Suggested from the domain name.',
      placeholder: 'EXAMPLE',
      upper: true,
      max: 15,
    })
  if (isDC.value)
    f.push({ key: 'ip', label: "This server's IP address", hint: 'The address the computers use to reach this DC.', mono: true })
  if (r === 'restore-dc')
    f.push({
      key: 'backup',
      label: 'DC backup on this server (.tar.bz2)',
      hint: 'Download it in Backup (kind dc) and copy it here, e.g. scp. The domain, its name and its administrator come from the backup; the old DC must stay off.',
      placeholder: '/root/20261018T235351Z-dc-220b59bb.tar.bz2',
      mono: true,
      wide: true,
    })
  if (r === 'dc-join')
    f.push({
      key: 'src_dc',
      label: 'IP address of an existing DC',
      hint: 'This server copies the domain from it.',
      placeholder: '10.0.0.10',
      mono: true,
    })
  if (r === 'member')
    f.push({
      key: 'dns_dc',
      label: 'IP address of a DC',
      hint: 'Used as DNS to find the domain.',
      placeholder: '10.0.0.10',
      mono: true,
    })
  if (r === 'new-dc' || r === 'dc-join')
    f.push({
      key: 'forwarder',
      label: 'DNS for internet names (forwarder)',
      hint: 'Where the DC asks for names outside the domain: your router or e.g. 1.1.1.1.',
      mono: true,
    })
  if (r === 'connect')
    f.push({
      key: 'url',
      label: 'Address of the DC (LDAP URL)',
      hint: 'e.g. ldaps://dc01.company.local',
      placeholder: 'ldaps://dc01.example.local',
      mono: true,
      wide: true,
    })
  if (r === 'member' || (isDC.value && plan.value.files))
    f.push({ key: 'share_root', label: 'Folder for the shares', hint: 'Where the shared folders live on this server.', mono: true })
  return f
})
function edited(key) {
  if (key === 'domain') domainTouched.value = true
  delete errors.value[key]
}
async function toReview() {
  errors.value = validate(plan.value, password.value, confirm.value)
  const first = Object.keys(errors.value)[0]
  if (first) {
    await nextTick()
    document.getElementById(`f-${first}`)?.focus()
    return
  }
  busy.value = true
  try {
    const r = await call('POST', '/plan', sent.value)
    review.value = r.lines || []
    step.value = 'review'
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}

// ── 5. review: what will be done, in words; the commands of the dry run behind a toggle
const review = ref([])
// a JSON line of samba-setup.py is a step ({"step"}) or an error ({"error"}); the rest is the commands' output
const parse = (l) => {
  if (l.startsWith('{')) {
    try {
      return JSON.parse(l)
    } catch {
      /* plain line */
    }
  }
  return { line: l }
}
const planSteps = computed(() => review.value.map(parse).filter((x) => x.step || x.error))
const commands = computed(() => review.value.filter((l) => !parse(l).step && !parse(l).error).map((l) => l.replace('[dry-run] ', '')))
const summary = computed(() => {
  const p = plan.value
  const s = []
  if (p.role === 'new-dc') s.push(t('Create the domain {realm} ({domain}) with this server as its first domain controller, at {ip}.', p))
  if (p.role === 'dc-join')
    s.push(t('Join this server to {realm} as one more domain controller, at {ip}, copying from {src}.', { ...p, src: p.src_dc }))
  if (p.role === 'restore-dc') s.push(t('Restore the domain controller of {realm} from {file}, at {ip}.', { ...p, file: p.backup }))
  if (p.role === 'member')
    s.push(t('Join this server to {realm} ({domain}) as a file server, using {dns} as DNS.', { ...p, dns: p.dns_dc }))
  if (p.role === 'connect') s.push(t('Connect the panel to the Active Directory at {url}; nothing is installed here.', p))
  if (needsPassword.value) s.push(t('With the domain administrator {admin}.', p))
  if (p.role === 'new-dc' || p.role === 'dc-join') s.push(t('Names outside the domain are asked to {fw}.', { fw: p.forwarder }))
  if (p.role === 'member' || (isDC.value && p.files)) s.push(t('The shared folders stay in {root}.', { root: p.share_root }))
  if (importReady.value)
    s.push(
      t(
        "This DC's agent gets the write modes to import GPOs and passwords from an existing AD; the administrator credential is kept encrypted in the panel (Settings) until you remove it.",
      ),
    )
  if (diag.value?.samba_files) s.push(t('Before any change, the Samba already here is backed up (shown under Backup).'))
  return s
})

// ── 6. installation
const lines = ref([])
const state = ref(null)
const done = ref(false)
const showLog = ref(false)
let timer = null
const runSteps = computed(() => lines.value.map(parse).filter((x) => x.step || x.error))
const total = computed(() => Math.max(planSteps.value.filter((x) => x.step).length, runSteps.value.length, 1))
// a step line is printed when the step starts: the last one is done only when the next starts or the setup ends
const started = computed(() => runSteps.value.filter((x) => x.step).length)
const finished = computed(() => (done.value && state.value?.state !== 'error' ? total.value : Math.max(0, started.value - 1)))
const failed = computed(() => state.value?.state === 'error')
const current = computed(() => {
  if (failed.value) return t('The installation stopped.')
  if (done.value) return t('Done.')
  if (state.value?.state === 'backup') return t('Backing up the current Samba…')
  return runSteps.value.at(-1)?.step || t('Starting…')
})
async function run() {
  busy.value = true
  try {
    await call('POST', '/run', { ...sent.value, password: needsPassword.value ? password.value : '' })
    password.value = confirm.value = ''
    lines.value = []
    done.value = false
    state.value = null
    step.value = 'run'
    poll()
  } catch (e) {
    error.value = e.message
  }
  busy.value = false
}
async function poll() {
  try {
    const s = await call('GET', `/status?from=${lines.value.length}`)
    state.value = s
    lines.value.push(...s.lines)
    if (s.finished) {
      done.value = true
      return
    }
  } catch {
    // the panel restarts at the end of a DC or "connect" setup: it comes back out of setup mode
    try {
      const h = await fetch('/api/health').then((r) => r.json())
      if (!h.setup) {
        done.value = true
        state.value = { state: 'ok' }
        return
      }
    } catch {
      /* still restarting */
    }
  }
  timer = setTimeout(poll, 1500)
}
onUnmounted(() => clearTimeout(timer))
// after a failure: back to the details (the password is asked again; finished steps are skipped on the next run)
function retry() {
  clearTimeout(timer)
  step.value = 'details'
}
</script>

<template>
  <main class="mx-auto my-6 w-[min(960px,calc(100%-32px))] space-y-4">
    <header class="card">
      <div class="p-4">
        <h1 class="text-[18px] font-semibold text-fg-strong">{{ t('AD Manager setup') }}</h1>
        <p class="mt-1 text-[13px] text-fg-muted">
          {{ t('A few steps to prepare this server. Nothing changes on it until you confirm the review.') }}
        </p>
      </div>
      <!-- the steps: done ones can be opened again; the current one is marked for screen readers -->
      <nav :aria-label="t('Setup steps')" class="border-t border-line px-4 py-3">
        <ol class="flex flex-wrap gap-x-5 gap-y-2 text-[12px]">
          <li v-for="(s, i) in STEPS" :key="s.id" :aria-current="i === idx ? 'step' : undefined">
            <button type="button" class="flex items-center gap-2 disabled:cursor-default" :disabled="!canGo(i)" @click="step = s.id">
              <span
                class="grid size-6 place-items-center border text-[12px] font-semibold"
                :class="
                  i < idx
                    ? 'border-ok-fill bg-ok-fill text-on-ok-fill'
                    : i === idx
                      ? 'border-action bg-action text-on-action'
                      : 'border-line text-fg-muted'
                "
              >
                <Check v-if="i < idx" :size="14" :aria-label="t('done')" />
                <template v-else>{{ i + 1 }}</template>
              </span>
              <span
                :class="
                  i === idx ? 'font-semibold text-fg-strong' : i < idx ? 'text-fg underline-offset-2 hover:underline' : 'text-fg-muted'
                "
                >{{ t(s.label) }}</span
              >
            </button>
          </li>
        </ol>
        <div class="mt-3 h-1 bg-surface-2" aria-hidden="true">
          <div class="h-full bg-action transition-[width] duration-300" :style="{ width: `${(idx / (STEPS.length - 1)) * 100}%` }" />
        </div>
      </nav>
    </header>

    <p v-if="error" role="alert" class="flex items-start gap-2 border border-err bg-surface p-3 text-[13px] text-err">
      <CircleX :size="16" class="mt-0.5 shrink-0" /> <span>{{ serverText(error) }}</span>
    </p>

    <!-- 1. access -->
    <form v-if="step === 'token'" class="card" @submit.prevent="signIn">
      <h2 ref="heading" tabindex="-1" class="card-h flex items-center gap-2 outline-none"><KeyRound :size="14" /> {{ t('Access') }}</h2>
      <div class="space-y-3 p-4 text-[13px]">
        <p>{{ t('To start, type the setup token: it proves you have access to this server.') }}</p>
        <div>
          <p class="text-[12px] text-fg-muted">{{ t('On the server, as root, run:') }}</p>
          <div class="mt-1 flex items-stretch">
            <code class="flex-1 border border-line bg-surface-2 px-2 py-1.5 font-mono text-[12px]">{{ tokenCmd }}</code>
            <button type="button" class="btn-ghost border-l-0" @click="copyCmd">
              <Check v-if="copied" :size="13" /><Copy v-else :size="13" /> {{ copied ? t('Copied') : t('Copy') }}
            </button>
          </div>
          <p class="mt-1 text-[11px] text-fg-muted">{{ t('It is valid for 24 hours and stops working when the setup ends.') }}</p>
        </div>
        <div>
          <label class="lbl" for="f-token">{{ t('Setup token') }}</label>
          <input id="f-token" v-model="token" class="inp font-mono" required autocomplete="off" spellcheck="false" />
        </div>
        <div class="flex justify-end">
          <button class="btn" :disabled="busy || !token.trim()">
            <LoaderCircle v-if="busy" :size="14" class="motion-safe:animate-spin" /> {{ t('Continue') }} <ChevronRight :size="14" />
          </button>
        </div>
      </div>
    </form>

    <!-- 2. this server -->
    <section v-if="step === 'server'" class="card" aria-labelledby="h-server">
      <h2 id="h-server" ref="heading" tabindex="-1" class="card-h flex items-center gap-2 outline-none">
        {{ t('This server') }}
        <button type="button" class="act ml-auto normal-case" :disabled="busy" @click="diagnose">
          <RefreshCw :size="12" :class="busy && 'motion-safe:animate-spin'" /> {{ t('Check again') }}
        </button>
      </h2>
      <div class="p-4 text-[13px]">
        <p :class="problems ? 'text-err' : 'text-fg-muted'" aria-live="polite">
          {{
            problems
              ? t(
                  problems === 1
                    ? '1 problem to fix: the setup may fail until it is solved.'
                    : '{n} problems to fix: the setup may fail until they are solved.',
                  { n: problems },
                )
              : t('Everything the setup needs is here. Items marked with attention are only notes.')
          }}
        </p>
        <ul class="mt-3 divide-y divide-line border border-line">
          <li v-for="c in checks" :key="c.label" class="flex items-start gap-3 px-3 py-2">
            <component :is="LEVEL[c.level].icon" :size="16" class="mt-0.5 shrink-0" :class="LEVEL[c.level].cls" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <div>
                <span class="sr-only">{{ t(LEVEL[c.level].sr) }}: </span><span class="font-semibold">{{ t(c.label) }}</span>
                <span class="text-fg-muted"> — {{ c.text }}</span>
              </div>
              <p v-if="c.fix" class="text-[12px]" :class="c.level === 'error' ? 'text-err' : 'text-fg-muted'">{{ c.fix }}</p>
            </div>
          </li>
        </ul>
        <div class="mt-4 flex justify-end">
          <button type="button" class="btn" @click="step = 'role'">{{ t('Continue') }} <ChevronRight :size="14" /></button>
        </div>
      </div>
    </section>

    <!-- 3. role -->
    <form v-if="step === 'role'" class="card" @submit.prevent="chooseRole">
      <h2 ref="heading" tabindex="-1" class="card-h outline-none">{{ t('What should this server be?') }}</h2>
      <fieldset class="grid gap-3 p-4 md:grid-cols-2">
        <legend class="sr-only">{{ t('Role of this server') }}</legend>
        <label
          v-for="r in ROLES"
          :key="r.id"
          class="flex cursor-pointer gap-3 border p-3 has-[:focus-visible]:outline-(length:--focus-ring-w) has-[:focus-visible]:outline-focus"
          :class="plan.role === r.id ? 'border-action bg-surface-2' : 'border-line hover:bg-row-hover'"
        >
          <input v-model="plan.role" type="radio" name="role" :value="r.id" class="sr-only" />
          <component
            :is="r.icon"
            :size="22"
            class="mt-0.5 shrink-0"
            :class="plan.role === r.id ? 'text-action' : 'text-fg-muted'"
            aria-hidden="true"
          />
          <span class="min-w-0">
            <span class="flex flex-wrap items-center gap-2 font-semibold">
              {{ t(r.name) }}
              <span v-if="r.recommended" class="tag bg-ok-fill text-on-ok-fill">{{ t('recommended') }}</span>
              <Check v-if="plan.role === r.id" :size="14" class="ml-auto text-action" :aria-label="t('chosen')" />
            </span>
            <span class="mt-1 block text-[12px] text-fg-muted">{{ t(r.help) }}</span>
            <span class="mt-1 block text-[12px]"
              ><b>{{ t('When:') }}</b> {{ t(r.when) }}</span
            >
          </span>
        </label>
      </fieldset>
      <div class="flex justify-between border-t border-line p-3">
        <button type="button" class="btn-ghost" @click="step = 'server'"><ChevronLeft :size="14" /> {{ t('Back') }}</button>
        <button class="btn">{{ t('Continue') }} <ChevronRight :size="14" /></button>
      </div>
    </form>

    <!-- 4. details -->
    <form v-if="step === 'details'" class="card" novalidate @submit.prevent="toReview">
      <h2 ref="heading" tabindex="-1" class="card-h flex items-center gap-2 outline-none">
        <component :is="roleInfo.icon" :size="14" aria-hidden="true" /> {{ t('Details: {role}', { role: t(roleInfo.name) }) }}
      </h2>
      <div class="space-y-4 p-4 text-[13px]">
        <fieldset class="grid gap-x-4 gap-y-3 md:grid-cols-2" :disabled="busy">
          <legend class="mb-2 text-[12px] font-semibold text-fg-strong uppercase">{{ t('Domain and server') }}</legend>
          <div v-for="f in fields" :key="f.key" :class="f.wide && 'md:col-span-2'">
            <label class="lbl" :for="`f-${f.key}`">{{ t(f.label) }}</label>
            <input
              :id="`f-${f.key}`"
              v-model.trim="plan[f.key]"
              class="inp"
              :class="[f.mono && 'font-mono', f.upper && 'uppercase', errors[f.key] && '!border-err']"
              :placeholder="f.placeholder"
              :maxlength="f.max"
              :aria-invalid="errors[f.key] ? 'true' : undefined"
              :aria-describedby="errors[f.key] ? `e-${f.key} h-${f.key}` : `h-${f.key}`"
              spellcheck="false"
              @input="edited(f.key)"
            />
            <p v-if="errors[f.key]" :id="`e-${f.key}`" class="mt-1 flex items-center gap-1 text-[12px] text-err">
              <CircleX :size="12" aria-hidden="true" /> {{ t(errors[f.key]) }}
            </p>
            <p :id="`h-${f.key}`" class="mt-1 text-[11px] text-fg-muted">{{ t(f.hint) }}</p>
          </div>
          <label v-if="isDC" class="flex items-center gap-2 text-[12px] md:col-span-2">
            <input v-model="plan.files" type="checkbox" /> {{ t('This DC also serves files (shares)') }}
          </label>
          <div v-if="plan.role === 'new-dc' && plan.files" class="text-[12px] md:col-span-2">
            <label class="flex items-center gap-2">
              <input v-model="plan.import_ad" type="checkbox" aria-describedby="h-import_ad" />
              {{ t('Prepare the import of GPOs and passwords from an existing AD') }}
            </label>
            <p id="h-import_ad" class="mt-1 text-[11px] text-fg-muted">
              {{
                t(
                  "Turns on this DC agent's write modes for the import (Import/export → From another AD), with the administrator credential kept encrypted in the panel; remove it after the import.",
                )
              }}
            </p>
          </div>
          <label v-if="plan.role === 'connect'" class="flex items-center gap-2 text-[12px] md:col-span-2">
            <input v-model="plan.insecure" type="checkbox" /> {{ t('Accept self-signed certificate') }}
          </label>
        </fieldset>

        <fieldset v-if="needsPassword" class="grid gap-x-4 gap-y-3 border-t border-line pt-4 md:grid-cols-3" :disabled="busy">
          <legend class="sr-only">{{ t('Domain administrator') }}</legend>
          <p class="text-[12px] font-semibold text-fg-strong uppercase md:col-span-3">{{ t('Domain administrator') }}</p>
          <div>
            <label class="lbl" for="f-admin">{{ t('Administrator account') }}</label>
            <input
              id="f-admin"
              v-model.trim="plan.admin"
              class="inp"
              :class="errors.admin && '!border-err'"
              :aria-invalid="errors.admin ? 'true' : undefined"
              :aria-describedby="errors.admin ? 'e-admin' : undefined"
              @input="edited('admin')"
            />
            <p v-if="errors.admin" id="e-admin" class="mt-1 text-[12px] text-err">{{ t(errors.admin) }}</p>
          </div>
          <div>
            <label class="lbl" for="f-password">{{
              plan.role === 'new-dc' ? t('Password for the new administrator') : t('Administrator password')
            }}</label>
            <input
              id="f-password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              class="inp"
              :class="errors.password && '!border-err'"
              autocomplete="new-password"
              :aria-invalid="errors.password ? 'true' : undefined"
              :aria-describedby="errors.password ? 'e-password h-password' : 'h-password'"
              @input="edited('password')"
            />
            <p v-if="errors.password" id="e-password" class="mt-1 text-[12px] text-err">{{ t(errors.password) }}</p>
          </div>
          <div>
            <label class="lbl" for="f-confirm">{{ t('Type it again') }}</label>
            <input
              id="f-confirm"
              v-model="confirm"
              :type="showPassword ? 'text' : 'password'"
              class="inp"
              :class="errors.confirm && '!border-err'"
              autocomplete="new-password"
              :aria-invalid="errors.confirm ? 'true' : undefined"
              :aria-describedby="errors.confirm ? 'e-confirm' : undefined"
              @input="edited('confirm')"
            />
            <p v-if="errors.confirm" id="e-confirm" class="mt-1 text-[12px] text-err">{{ t(errors.confirm) }}</p>
          </div>
          <p id="h-password" class="text-[11px] text-fg-muted md:col-span-2">
            {{
              plan.role === 'new-dc'
                ? t(
                    'At least 8 characters mixing three of: lowercase, uppercase, digits and symbols. Keep it safe: it is the key to the domain.',
                  )
                : t('The password of a domain administrator.')
            }}
            {{ t('It goes to Samba through standard input: never on a command line, in a log or in the audit.') }}
          </p>
          <label class="flex items-center gap-2 text-[12px]"
            ><input v-model="showPassword" type="checkbox" /> {{ t('Show the password') }}</label
          >
        </fieldset>
      </div>
      <div class="flex justify-between border-t border-line p-3">
        <button type="button" class="btn-ghost" :disabled="busy" @click="step = 'role'"><ChevronLeft :size="14" /> {{ t('Back') }}</button>
        <button class="btn" :disabled="busy">
          <LoaderCircle v-if="busy" :size="14" class="motion-safe:animate-spin" /> {{ t('Review') }} <ChevronRight :size="14" />
        </button>
      </div>
    </form>

    <!-- 5. review -->
    <section v-if="step === 'review'" class="card">
      <h2 ref="heading" tabindex="-1" class="card-h outline-none">{{ t('Review: what will be done') }}</h2>
      <div class="space-y-4 p-4 text-[13px]">
        <ul class="list-disc space-y-1 pl-5">
          <li v-for="(s, i) in summary" :key="i">{{ s }}</li>
        </ul>
        <div v-if="planSteps.length">
          <h3 class="text-[12px] font-semibold text-fg-strong uppercase">{{ t('Steps ({n})', { n: planSteps.length }) }}</h3>
          <ol class="mt-2 list-decimal space-y-0.5 pl-5 text-[12px]">
            <li v-for="(s, i) in planSteps" :key="i" :class="s.error && 'text-err'">{{ s.step || s.error }}</li>
          </ol>
        </div>
        <details v-if="commands.length" class="text-[12px]">
          <summary class="cursor-pointer text-link">{{ t('Show the commands ({n})', { n: commands.length }) }}</summary>
          <pre class="mt-2 max-h-72 overflow-auto bg-surface-2 p-2 text-[11px]">{{ commands.join('\n') }}</pre>
        </details>
        <p class="flex items-start gap-2 border-l-4 border-warn bg-surface-2 p-2 text-[12px]">
          <TriangleAlert :size="14" class="mt-0.5 shrink-0 text-warn" aria-hidden="true" />
          {{ t('Installing changes this server and can take several minutes. Keep this page open until it ends.') }}
        </p>
      </div>
      <div class="flex justify-between border-t border-line p-3">
        <button type="button" class="btn-ghost" :disabled="busy" @click="step = 'details'">
          <ChevronLeft :size="14" /> {{ t('Back') }}
        </button>
        <button type="button" class="btn" :disabled="busy" @click="run">
          <LoaderCircle v-if="busy" :size="14" class="motion-safe:animate-spin" /><Play v-else :size="14" /> {{ t('Install now') }}
        </button>
      </div>
    </section>

    <!-- 6. installation -->
    <section v-if="step === 'run'" class="card">
      <h2 ref="heading" tabindex="-1" class="card-h outline-none">
        {{ failed ? t('The installation stopped') : done ? t('Installation finished') : t('Installing…') }}
      </h2>
      <div class="space-y-3 p-4 text-[13px]">
        <div
          role="progressbar"
          :aria-label="t('Installation progress')"
          aria-valuemin="0"
          :aria-valuemax="total"
          :aria-valuenow="finished"
          :aria-valuetext="t('{done} of {total} steps', { done: finished, total })"
          class="h-2 bg-surface-2"
        >
          <div
            class="h-full transition-[width] duration-300"
            :class="failed ? 'bg-err' : done ? 'bg-ok' : 'bg-action'"
            :style="{ width: `${(finished / total) * 100}%` }"
          />
        </div>
        <p class="flex items-center gap-2" aria-live="polite">
          <LoaderCircle v-if="!done && !failed" :size="14" class="motion-safe:animate-spin text-action" aria-hidden="true" />
          <CircleCheck v-else-if="done && !failed" :size="14" class="text-ok" aria-hidden="true" />
          <CircleX v-else :size="14" class="text-err" aria-hidden="true" />
          <span>{{ current }}</span>
          <span class="ml-auto text-[12px] text-fg-muted">{{ t('{done} of {total} steps', { done: finished, total }) }}</span>
        </p>
        <p v-if="state?.backup" class="text-[12px] text-fg-muted">{{ t('Backup before the setup: {id}', { id: state.backup }) }}</p>
        <ol class="space-y-0.5 text-[12px]">
          <li v-for="(s, i) in runSteps" :key="i" class="flex items-center gap-1.5" :class="s.error && 'text-err'">
            <CircleX v-if="s.error || (failed && i === runSteps.length - 1)" :size="13" class="text-err" aria-hidden="true" />
            <LoaderCircle
              v-else-if="!done && i === runSteps.length - 1"
              :size="13"
              class="text-action motion-safe:animate-spin"
              :aria-label="t('running')"
            />
            <CircleCheck v-else :size="13" class="text-ok" aria-hidden="true" />
            {{ s.step || s.error }}
          </li>
        </ol>
        <p v-if="state?.error" class="text-err">{{ serverText(state.error) }}</p>

        <div v-if="done && !failed" class="border-l-4 border-ok bg-surface-2 p-3">
          <p class="font-semibold">{{ t('Next steps') }}</p>
          <ol class="mt-1 list-decimal space-y-0.5 pl-5 text-[12px]">
            <li>{{ t('Open the panel and sign in with the domain administrator.') }}</li>
            <li v-if="plan.role === 'new-dc'">
              {{ t('To bring users, groups, DNS, GPOs, shares and passwords from an existing AD: Import/export → From another AD.') }}
            </li>
            <li v-if="importReady">
              {{ t('After the import, remove the import credential in Settings → Import from an existing AD.') }}
            </li>
            <li v-if="isDC">{{ t('Point the DNS of the computers to {ip} and join them to the domain.', { ip: plan.ip }) }}</li>
            <li v-if="plan.role === 'member' || (isDC && plan.files)">{{ t('Create the shared folders in Shares.') }}</li>
            <li>{{ t('In Backup, check the first backup and its schedule.') }}</li>
          </ol>
        </div>
        <p v-if="failed" class="border-l-4 border-err bg-surface-2 p-3 text-[12px]">
          {{
            t('Nothing more runs. Fix the cause and run again (finished steps are skipped), or restore the backup taken before the setup.')
          }}
        </p>

        <details :open="showLog" class="text-[12px]" @toggle="showLog = $event.target.open">
          <summary class="cursor-pointer text-link">{{ showLog ? t('Hide the full log') : t('Show the full log') }}</summary>
          <pre class="mt-2 max-h-80 overflow-auto bg-surface-2 p-2 text-[11px]">{{ lines.join('\n') }}</pre>
          <p v-if="state?.log_file" class="mt-1 text-[11px] text-fg-muted">
            {{ t('Saved on the server in {file}.', { file: state.log_file }) }}
          </p>
        </details>
      </div>
      <div v-if="done || failed" class="flex justify-end gap-2 border-t border-line p-3">
        <button v-if="failed" type="button" class="btn-ghost" @click="retry">
          <ChevronLeft :size="14" /> {{ t('Back to the details') }}
        </button>
        <template v-else>
          <a v-if="plan.role === 'new-dc'" href="/import-export?mode=ad" :class="importReady ? 'btn' : 'btn-ghost'">
            <ArrowLeftRight :size="14" /> {{ t('Import from an existing AD') }}
          </a>
          <a href="/" :class="importReady ? 'btn-ghost' : 'btn'">{{ t('Open the panel') }} <ChevronRight :size="14" /></a>
        </template>
      </div>
    </section>
  </main>
</template>
