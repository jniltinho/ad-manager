<script setup>
// Runtime settings (stored in the local SQLite, applied without a restart), the panel's [ad] keys of config.toml
// (applied by the agent on the panel's host, which restarts the panel) and the read-only values, in sub-tabs.
import { computed, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  Building2,
  Check,
  Copy,
  ImageUp,
  KeyRound,
  LoaderCircle,
  Network,
  RefreshCw,
  RotateCcw,
  Server,
  SlidersHorizontal,
  Trash2,
} from 'lucide-vue-next'
import { auth, brand, bumpLogo, del, get, loadMe, logoUrl, post, put } from '../api'
import { toast } from '../toasts'
import { askConfirm } from '../dialogs'
import { languages, setLang, t } from '../i18n'
import defaultLogo from '../assets/ad-manager-logo.png'
import lightLogo from '../assets/ad-manager-logo-light.png'
import LoadingNote from '../components/LoadingNote.vue'
import { formatDate, spinner } from '../util'

const route = useRoute()
const tabs = [
  ['general', 'General', SlidersHorizontal],
  ['company', 'Company', Building2],
  ['directory', 'Directory', Network],
  ['server', 'Panel server', Server],
  ['agents', 'Agents', KeyRound],
]
const tab = computed(() => (tabs.some(([id]) => id === route.query.tab) ? route.query.tab : 'general'))

const form = ref(null)
const readonly = ref({})
const stored = ref(true)
const hasLogo = ref(false)
const ous = ref([])
const busy = ref(false)
const saving = spinner(busy) // the save button's loader turns visibly even when the panel answers at once

async function load() {
  try {
    const r = await get('/settings')
    form.value = { ...r.settings }
    readonly.value = r.readonly
    stored.value = r.stored
    hasLogo.value = r.logo
  } catch (e) {
    toast.error(e.message)
  }
}
load()
get('/ous')
  .then((r) => (ous.value = r))
  .catch(() => {})

async function save() {
  busy.value = true
  try {
    form.value = await put('/settings', form.value)
    await loadMe()
    setLang(form.value.lang) // the panel switches right away for whoever saved
    toast.success(t('Settings saved.'))
  } catch (e) {
    toast.error(e.message)
  }
  busy.value = false
}

const logoInput = ref(null)
async function uploadLogo(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  const fd = new FormData()
  fd.append('logo', f)
  try {
    await put('/settings/logo', fd)
    bumpLogo()
    hasLogo.value = brand.logo = true
    toast.success(t('Logo updated.'))
  } catch (e) {
    toast.error(e.message)
  }
}
async function removeLogo() {
  if (!(await askConfirm(t('Remove the company logo and go back to the default one?'), { ok: t('Remove') }))) return
  try {
    await del('/settings/logo')
    bumpLogo()
    hasLogo.value = brand.logo = false
    toast.removed(t('Logo removed.'))
  } catch (e) {
    toast.error(e.message)
  }
}

// agent token: shown once; the file server keeps only its hash
// the import credential (the target administrator the DC agent's GPO import uses), kept sealed by the panel
const importCred = ref({ user: '', set: false })
const importForm = ref({ user: 'Administrator', password: '' })
const importBusy = ref(false)
get('/settings/import-credential')
  .then((r) => (importCred.value = r))
  .catch(() => {})
async function saveImportCred() {
  importBusy.value = true
  try {
    importCred.value = await put('/settings/import-credential', importForm.value)
    importForm.value.password = ''
    toast.success(t('Import credential saved (encrypted).'))
  } catch (e) {
    toast.error(e.message)
  }
  importBusy.value = false
}
async function removeImportCred() {
  if (!(await askConfirm(t('Remove the import credential?'), { ok: t('Remove') }))) return
  try {
    importCred.value = await del('/settings/import-credential')
  } catch (e) {
    toast.error(e.message)
  }
}

// the panel's [ad] keys of config.toml: sent to the agent on the panel's host, which writes the file, restarts the
// panel and puts the previous file back when the panel does not come back
const conf = ref(null)
const confForm = ref({})
const confBusy = ref(false)
const applying = ref(false)
let confTimer = null
async function loadConf() {
  clearTimeout(confTimer)
  try {
    conf.value = await get('/settings/config')
    confForm.value = { ...conf.value.values }
    // a change still running on the agent: look again until it ends
    if (['pending', 'active', 'retry', 'scheduled'].includes(conf.value.last?.state)) confTimer = setTimeout(loadConf, 3000)
  } catch (e) {
    toast.error(e.message)
  }
}
loadConf()
const confLocked = (k) => !conf.value?.agent || !!conf.value?.env?.[k] || applying.value
const adChanged = computed(() =>
  Object.keys(confForm.value).some((k) => k.startsWith('ad.') && confForm.value[k] !== conf.value?.values?.[k]),
)
const confChanged = computed(() => Object.keys(confForm.value).some((k) => confForm.value[k] !== conf.value?.values?.[k]))
let poll = null
onUnmounted(() => {
  clearInterval(poll)
  clearTimeout(confTimer)
})
async function applyConf() {
  const ok = await askConfirm(
    adChanged.value
      ? t(
          "The agent on the panel's host writes config.toml and restarts the panel: every session ends and you sign in again. If the panel does not come back within 60 s, the agent puts the previous file back.",
        )
      : t("The agent on the panel's host writes config.toml and restarts itself with the new switches."),
    { ok: t('Apply and restart') },
  )
  if (!ok) return
  confBusy.value = true
  try {
    await put('/settings/config', { keys: confForm.value })
    if (!adChanged.value) {
      toast.success(t('Sent: the agent restarts with the new switches.'))
      confBusy.value = false
      setTimeout(loadConf, 1500)
      return
    }
    applying.value = true
    // the restart ends this session: the next call answers 401 and the panel goes back to the login
    const until = Date.now() + 120000
    poll = setInterval(async () => {
      if (Date.now() > until) {
        clearInterval(poll)
        applying.value = false
        loadConf() // the panel was not restarted: the agent's answer is in the last result
        return
      }
      try {
        await get('/me')
      } catch {
        // restarting: try again
      }
    }, 3000)
  } catch (e) {
    toast.error(e.message)
  }
  confBusy.value = false
}
const lastState = computed(() => {
  const l = conf.value?.last
  if (!l) return null
  if (l.state === 'completed') return ['ok', t('applied')]
  if (l.error) return ['bad', t('failed: {error}', { error: l.error })]
  return ['muted', l.state || t('unknown')]
})
// an empty Base DN is derived from the domain, as the server does (ad.Config.Base): show the value in effect
const derivedBase = computed(() =>
  (confForm.value['ad.domain'] || '')
    .split('.')
    .filter(Boolean)
    .map((p) => 'DC=' + p)
    .join(','),
)
const placeholder = (k, ph) =>
  k === 'ad.base_dn' && derivedBase.value ? t('{dn} (derived from the domain)', { dn: derivedBase.value }) : t(ph)
const confFields = [
  ['ad.url', 'Domain controller', 'ldaps://dc1.domain.local'],
  ['ad.domain', 'Domain', 'domain.local'],
  ['ad.base_dn', 'Base DN', 'derived from the domain'],
  ['ad.admin_group', 'Administrators group', 'Domain Admins (RID 512)'],
  ['ad.ca_file', 'LDAPS CA', 'system trust store (e.g. /opt/ad-manager/tls/samba-ca.pem)'],
]

const agentToken = ref(null)
async function newAgentToken() {
  try {
    agentToken.value = await post('/agents/token')
  } catch (e) {
    toast.error(e.message)
  }
}
async function copy(text, msg) {
  await navigator.clipboard?.writeText(text)
  toast.success(t(msg))
}

const readonlyRows = computed(() => {
  const r = readonly.value
  const yes = (v) => (v ? t('yes') : t('no'))
  return [
    [t('Domain controller'), r.ad_url],
    [t('Domain'), r.domain],
    ['Base DN', r.base_dn],
    [t('LDAPS CA'), r.ca_file || t('system trust store')],
    [t('Administrators group'), r.admin_group || 'Domain Admins (RID 512)'],
    [t('Panel address'), r.listen],
    ['HTTPS', r.tls ? (r.self_signed ? t('self-signed certificate') : t('certificate from config.toml')) : t('behind a TLS proxy')],
    [t('Trust proxy'), yes(r.trust_proxy)],
    [t('Audit log'), r.audit_log],
    [t('Local database'), r.db],
    [t('Queue (Redis)'), r.redis || t('disabled')],
    [t('Version'), r.version],
  ]
})
</script>

<template>
  <div class="subtabs">
    <!-- custom: only the query changes, so a plain router-link would mark every tab as the current page -->
    <router-link v-for="[id, label, icon] in tabs" :key="id" v-slot="{ href, navigate }" :to="{ query: { tab: id } }" custom>
      <a :href="href" class="subtab" :class="{ on: tab === id }" :aria-current="tab === id ? 'page' : undefined" @click="navigate">
        <component :is="icon" :size="13" /> {{ t(label) }}
      </a>
    </router-link>
  </div>
  <LoadingNote v-if="!form" class="justify-center p-6" />
  <div v-else class="max-w-[1400px] space-y-4">
    <form v-if="tab === 'general' || tab === 'company'" class="space-y-4" @submit.prevent="save">
      <p v-if="!stored" class="border-l-4 border-warn bg-warn-soft px-3 py-2 text-[12px]">
        {{ t('No local database (db.path): settings cannot be saved.') }}
      </p>
      <template v-if="tab === 'general'">
        <section class="card">
          <div class="card-h">{{ t('General') }}</div>
          <div class="grid gap-x-4 gap-y-3 p-3 md:grid-cols-3">
            <div>
              <label class="lbl" for="s-lang">{{ t('Default language') }}</label>
              <select id="s-lang" v-model="form.lang" class="inp">
                <option v-for="[code, name] in languages" :key="code" :value="code">{{ name }}</option>
              </select>
              <p class="mt-1 text-[11px] text-fg-muted">{{ t('Also selectable on the login page.') }}</p>
            </div>
            <div>
              <label class="lbl" for="s-theme">{{ t('Theme') }}</label>
              <select id="s-theme" v-model="form.theme" class="inp">
                <option v-for="th in brand.themes" :key="th.id" :value="th.id">{{ t(th.label) }}</option>
              </select>
              <p class="mt-1 text-[11px] text-fg-muted">{{ t('Look of the panel and the login page for every operator.') }}</p>
            </div>
            <div>
              <label class="lbl" for="s-session">{{ t('Session timeout (minutes idle)') }}</label>
              <input id="s-session" v-model.number="form.session_minutes" type="number" min="5" max="720" class="inp" required />
            </div>
            <div class="flex flex-wrap items-start gap-x-8 gap-y-2 md:col-span-3">
              <label class="flex items-center gap-2"
                ><input v-model="form.flat" type="checkbox" /> {{ t('Flat look (no gradients or shadows)') }}</label
              >
              <div class="min-w-0 flex-1">
                <label class="flex items-center gap-2 font-semibold text-fg-strong"
                  ><input v-model="form.swagger" type="checkbox" /> {{ t('API documentation (Swagger) at /swagger') }}</label
                >
                <p class="mt-0.5 text-[11px] text-fg-muted">
                  {{ t('Only for signed-in administrators, like the queue monitor. Keep it off in production.') }}
                </p>
              </div>
              <div class="min-w-0 flex-1">
                <label class="flex items-center gap-2 font-semibold text-fg-strong"
                  ><input v-model="form.ssh_terminal" type="checkbox" /> {{ t('SSH terminal (Backup › Terminal)') }}</label
                >
                <p class="mt-0.5 text-[11px] text-fg-muted">
                  {{
                    t(
                      "SSH to the registered servers from the panel. Commands run there skip the panel's protections and audit (only the connection is audited). Turning it off closes open terminals.",
                    )
                  }}
                </p>
              </div>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-h">{{ t('Users') }}</div>
          <div class="grid gap-4 p-3 md:grid-cols-3">
            <div>
              <label class="lbl" for="s-inactive">{{ t('Inactive after (days without logon)') }}</label>
              <input id="s-inactive" v-model.number="form.inactive_days" type="number" min="7" max="3650" class="inp" required />
            </div>
            <div>
              <label class="lbl" for="s-pwlen">{{ t('Generated password length') }}</label>
              <input id="s-pwlen" v-model.number="form.password_length" type="number" min="10" max="64" class="inp" required />
            </div>
            <div>
              <label class="lbl" for="s-ou">{{ t('Default OU for new users') }}</label>
              <select id="s-ou" v-model="form.default_ou" class="inp">
                <option value="">CN=Users</option>
                <option v-for="o in ous" :key="o.dn" :value="o.dn">{{ o.dn.replace(/,DC=.*$/i, '') }}</option>
              </select>
            </div>
          </div>
        </section>
      </template>
      <template v-else>
        <section class="card">
          <div class="card-h">{{ t('Company') }}</div>
          <div class="grid gap-4 p-3 md:grid-cols-2">
            <div>
              <label class="lbl" for="s-company">{{ t('Company name (footer)') }}</label>
              <input id="s-company" v-model="form.company" class="inp" maxlength="80" placeholder="AD Manager" />
            </div>
            <div>
              <label class="lbl" for="s-support">{{ t('Support link') }}</label>
              <input
                id="s-support"
                v-model="form.support_url"
                type="url"
                class="inp"
                placeholder="https://github.com/jniltinho/ad-manager/wiki"
              />
            </div>
            <div class="md:col-span-2">
              <span class="lbl">{{ t('Logo (login page and header)') }}</span>
              <div class="flex flex-wrap items-center gap-3">
                <div class="flex h-16 w-56 items-center justify-center border border-line bg-surface-3 p-2">
                  <img v-if="hasLogo" :src="logoUrl()" :alt="t('Logo')" class="logo-plate max-h-full max-w-full object-contain" />
                  <template v-else>
                    <img
                      :src="defaultLogo"
                      :alt="t('Logo')"
                      class="max-h-full max-w-full object-contain"
                      style="display: var(--preview-logo-dark-ink)"
                    />
                    <img
                      :src="lightLogo"
                      :alt="t('Logo')"
                      class="max-h-full max-w-full object-contain"
                      style="display: var(--preview-logo-light-ink)"
                    />
                  </template>
                </div>
                <input ref="logoInput" type="file" accept="image/png,image/jpeg,image/svg+xml" class="hidden" @change="uploadLogo" />
                <button type="button" class="btn-ghost" :disabled="!stored" @click="logoInput.click()">
                  <ImageUp :size="13" /> {{ t('Upload logo') }}
                </button>
                <button v-if="hasLogo" type="button" class="btn-ghost" @click="removeLogo">
                  <Trash2 :size="13" /> {{ t('Use the default logo') }}
                </button>
                <span class="text-[11px] leading-snug text-fg-muted"
                  >{{ t('PNG, JPEG or SVG up to 512 KiB.') }}<br />{{ t('Maximum size: 660 × 220 px (width × height), ratio 3:1.')
                  }}<br />{{ t('Shown at up to 195 × 65 px on the login page and 40 px high in the header.') }}</span
                >
              </div>
            </div>
          </div>
        </section>
      </template>
      <div class="flex gap-2">
        <button class="btn" :disabled="saving || !stored">
          <LoaderCircle v-if="saving" :size="14" class="motion-safe:animate-spin" /><Check v-else :size="14" />
          {{ saving ? t('Saving…') : t('Save settings') }}
        </button>
        <button type="button" class="btn-ghost" @click="load"><RotateCcw :size="13" /> {{ t('Discard changes') }}</button>
      </div>
    </form>

    <section v-if="tab === 'directory'" class="card">
      <div class="card-h">{{ t('Directory (config.toml [ad])') }}</div>
      <LoadingNote v-if="!conf" class="p-3" />
      <form v-else class="space-y-3 p-3 text-[12px]" @submit.prevent="applyConf">
        <p v-if="!conf.agent" class="border-l-4 border-warn bg-warn-soft px-3 py-2">
          {{ t(conf.agent_error) }}.
          {{
            t("Register the agent of the panel's host in Shares to change these here, or edit {file} and restart ad-manager.", {
              file: conf.file || 'config.toml',
            })
          }}
        </p>
        <p v-else class="text-fg-muted">
          {{
            t(
              "Applied by the agent of {name} (the panel's host): it checks the values, writes {file}, restarts the panel and puts the previous file back if the panel does not answer.",
              { name: conf.agent.name, file: conf.file },
            )
          }}
        </p>
        <div class="grid gap-x-4 gap-y-3 md:grid-cols-2">
          <div v-for="[k, label, ph] in confFields" :key="k">
            <label class="lbl" :for="'c-' + k"
              >{{ t(label) }} <span class="font-mono font-normal normal-case">{{ k }}</span></label
            >
            <input
              :id="'c-' + k"
              v-model.trim="confForm[k]"
              class="inp font-mono text-[11px]"
              :placeholder="placeholder(k, ph)"
              :disabled="confLocked(k)"
            />
            <p v-if="conf.env?.[k]" class="mt-1 text-[11px] text-fg-muted">
              {{ t('Set by the environment ({env}).', { env: conf.env[k] }) }}
            </p>
          </div>
          <div class="flex flex-col justify-end gap-2 md:col-span-2">
            <label class="flex items-center gap-2"
              ><input v-model="confForm['ad.start_tls']" type="checkbox" :disabled="confLocked('ad.start_tls')" />
              {{ t('StartTLS on ldap://') }} <span class="font-mono text-fg-muted">ad.start_tls</span></label
            >
            <label class="flex items-center gap-2"
              ><input v-model="confForm['ad.insecure_skip_verify']" type="checkbox" :disabled="confLocked('ad.insecure_skip_verify')" />
              {{ t("Don't verify the DC's certificate (lab only)") }}
              <span class="font-mono text-fg-muted">ad.insecure_skip_verify</span></label
            >
          </div>
        </div>
        <p v-if="applying" class="flex items-center gap-2 border-l-4 border-warn bg-warn-soft px-3 py-2">
          <LoaderCircle :size="13" class="motion-safe:animate-spin" />
          {{ t('Applying: the panel restarts and the sign-in page opens when it is back.') }}
        </p>
        <p v-if="conf.last && lastState" class="text-fg-muted">
          {{ t('Last change: {keys} by {user}, {at}:', { keys: conf.last.keys, user: conf.last.user, at: formatDate(conf.last.at) }) }}
          <span :class="{ 'font-semibold text-ok': lastState[0] === 'ok', 'font-semibold text-err': lastState[0] === 'bad' }">{{
            lastState[1]
          }}</span>
        </p>
        <div class="flex gap-2">
          <button class="btn" :disabled="!conf.agent || !confChanged || confBusy || applying">
            <LoaderCircle v-if="confBusy" :size="14" class="motion-safe:animate-spin" /><RefreshCw v-else :size="14" />
            {{ t('Apply and restart') }}
          </button>
          <button type="button" class="btn-ghost" :disabled="applying" @click="loadConf">
            <RotateCcw :size="13" /> {{ t('Discard changes') }}
          </button>
        </div>
      </form>
    </section>

    <div v-if="tab === 'server'" class="max-w-[900px]">
      <section class="card">
        <div class="card-h">{{ t('From config.toml (read-only)') }}</div>
        <table class="grid-t border-0">
          <tbody>
            <tr v-for="[k, v] in readonlyRows" :key="k">
              <td class="w-px font-semibold whitespace-nowrap text-fg-strong">{{ k }}</td>
              <!-- one line per value; a value that does not fit shows in full on hover -->
              <td class="max-w-0 truncate font-mono text-[11px] whitespace-nowrap" :title="v">{{ v }}</td>
            </tr>
          </tbody>
        </table>
        <p class="border-t border-line px-3 py-2 text-[11px] text-fg-muted">
          {{ t('Edit /opt/ad-manager/config.toml and restart the ad-manager service to change these.') }}
          {{ t('Signed in as {user}.', { user: auth.user }) }}
        </p>
      </section>
    </div>

    <div v-if="tab === 'agents'" class="grid gap-4 xl:grid-cols-2">
      <section v-if="conf" class="card xl:col-span-2">
        <div class="card-h">{{ t("This server's agent (config.toml [agent])") }}</div>
        <form class="space-y-3 p-3 text-[12px]" @submit.prevent="applyConf">
          <p class="border-l-4 border-warn bg-warn-soft px-3 py-2">
            {{
              t(
                "These switch on write modes of the agent, which runs as root on the panel's host: every administrator of the panel can then use them. The agent restarts to apply them.",
              )
            }}
          </p>
          <p v-if="!conf.agent" class="text-fg-muted">{{ t(conf.agent_error) }}.</p>
          <label class="flex items-start gap-2"
            ><input
              v-model="confForm['agent.dc_file_server']"
              type="checkbox"
              class="mt-0.5"
              :disabled="confLocked('agent.dc_file_server')"
            />
            <span
              ><span class="font-semibold text-fg-strong">{{ t('This DC also serves the shares') }}</span>
              <span class="ml-1 font-mono text-fg-muted">agent.dc_file_server</span><br /><span class="text-fg-muted">{{
                t('Folders, shares and ACLs on the domain controller (Windows ACLs and registry shares required).')
              }}</span></span
            ></label
          >
          <label class="flex items-start gap-2"
            ><input v-model="confForm['agent.import_modes']" type="checkbox" class="mt-0.5" :disabled="confLocked('agent.import_modes')" />
            <span
              ><span class="font-semibold text-fg-strong">{{ t('GPO import and password migration') }}</span>
              <span class="ml-1 font-mono text-fg-muted">agent.import_modes</span><br /><span class="text-fg-muted">{{
                t('The administrator credential is the one kept in Import from an existing AD, below.')
              }}</span></span
            ></label
          >
          <button class="btn" :disabled="!conf.agent || !confChanged || confBusy || applying">
            <LoaderCircle v-if="confBusy" :size="14" class="motion-safe:animate-spin" /><RefreshCw v-else :size="14" />
            {{ t('Apply and restart') }}
          </button>
        </form>
      </section>
      <section class="card">
        <div class="card-h">{{ t('File server agent token') }}</div>
        <div class="space-y-3 px-3 py-3 text-[12px]">
          <div class="flex items-center gap-3">
            <p class="min-w-0 flex-1 text-fg-muted">
              {{ t('Register the token in Shares → + new, and run the command on the file server: it keeps only the hash.') }}
            </p>
            <button type="button" class="btn-ghost shrink-0" @click="newAgentToken">
              <KeyRound :size="13" /> {{ t('Generate token') }}
            </button>
          </div>
          <template v-if="agentToken">
            <div>
              <label class="lbl" for="agt">{{ t('Token (shown only now)') }}</label>
              <div class="flex">
                <input id="agt" :value="agentToken.token" class="inp font-mono text-[11px]" readonly @focus="$event.target.select()" />
                <button type="button" class="btn-ghost" @click="copy(agentToken.token, 'Token copied.')"><Copy :size="13" /></button>
              </div>
            </div>
            <div>
              <label class="lbl" for="agc">{{ t('Command on the file server') }}</label>
              <div class="flex">
                <input id="agc" :value="agentToken.command" class="inp font-mono text-[11px]" readonly @focus="$event.target.select()" />
                <button type="button" class="btn-ghost" @click="copy(agentToken.command, 'Command copied.')"><Copy :size="13" /></button>
              </div>
            </div>
          </template>
        </div>
      </section>
      <section class="card">
        <div class="card-h">{{ t('Import from an existing AD') }}</div>
        <form class="space-y-3 px-3 py-3 text-[12px]" @submit.prevent="saveImportCred">
          <p class="text-fg-muted">
            {{
              t(
                "This domain's administrator, for the GPO import of From another AD: kept encrypted in the panel's database and sent to the DC agent with each task only. Remove it after the import.",
              )
            }}
          </p>
          <p v-if="importCred.set" class="font-semibold text-ok">{{ t('Kept for {user}.', { user: importCred.user }) }}</p>
          <div class="flex items-end gap-2">
            <div class="min-w-0 flex-1">
              <label class="lbl" for="icu">{{ t('User') }}</label
              ><input id="icu" v-model.trim="importForm.user" class="inp" autocomplete="off" required />
            </div>
            <div class="min-w-0 flex-1">
              <label class="lbl" for="icp">{{ t('Password') }}</label
              ><input id="icp" v-model="importForm.password" type="password" class="inp" autocomplete="new-password" required />
            </div>
            <button class="btn-ghost" :disabled="importBusy || !importForm.user || !importForm.password">
              <KeyRound :size="13" /> {{ t('Save') }}
            </button>
            <button v-if="importCred.set" type="button" class="btn-ghost" @click="removeImportCred">
              <Trash2 :size="13" /> {{ t('Remove') }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
