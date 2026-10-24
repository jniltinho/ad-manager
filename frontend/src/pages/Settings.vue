<script setup>
// Runtime settings (stored in the local SQLite, applied without a restart) and
// the read-only values that come from config.toml.
import { computed, ref } from 'vue'
import { Check, Copy, ImageUp, KeyRound, RotateCcw, Trash2 } from 'lucide-vue-next'
import { auth, brand, bumpLogo, del, get, loadMe, logoUrl, post, put } from '../api'
import { toast } from '../toasts'
import { askConfirm } from '../dialogs'
import { languages, setLang, t } from '../i18n'
import defaultLogo from '../assets/ad-manager-logo.png'
import lightLogo from '../assets/ad-manager-logo-light.png'
import LoadingNote from '../components/LoadingNote.vue'

const form = ref(null)
const readonly = ref({})
const stored = ref(true)
const hasLogo = ref(false)
const ous = ref([])
const busy = ref(false)

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
  <LoadingNote v-if="!form" class="justify-center p-6" />
  <div v-else class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_520px]">
    <form class="space-y-5" @submit.prevent="save">
      <p v-if="!stored" class="border-l-4 border-warn bg-warn-soft px-3 py-2 text-[12px]">
        {{ t('No local database (db.path): settings cannot be saved.') }}
      </p>
      <section class="card">
        <div class="card-h">{{ t('General') }}</div>
        <div class="grid gap-4 p-3 md:grid-cols-2">
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
            <label class="mt-2 flex items-center gap-2"
              ><input v-model="form.flat" type="checkbox" /> {{ t('Flat look (no gradients or shadows)') }}</label
            >
          </div>
          <div>
            <label class="lbl" for="s-session">{{ t('Session timeout (minutes idle)') }}</label>
            <input id="s-session" v-model.number="form.session_minutes" type="number" min="5" max="720" class="inp" required />
          </div>
          <div class="md:col-span-2">
            <label class="flex items-center gap-2 font-semibold text-fg-strong"
              ><input v-model="form.swagger" type="checkbox" /> {{ t('API documentation (Swagger) at /swagger') }}</label
            >
            <p class="mt-1 text-[11px] text-fg-muted">
              {{ t('Only for signed-in administrators, like the queue monitor. Keep it off in production.') }}
            </p>
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
                >{{ t('PNG, JPEG or SVG up to 512 KiB.') }}<br />{{ t('Maximum size: 660 × 220 px (width × height), ratio 3:1.') }}<br />{{
                  t('Shown at up to 195 × 65 px on the login page and 40 px high in the header.')
                }}</span
              >
            </div>
          </div>
        </div>
      </section>

      <div class="flex gap-2">
        <button class="btn" :disabled="busy || !stored"><Check :size="14" /> {{ busy ? t('Saving…') : t('Save settings') }}</button>
        <button type="button" class="btn-ghost" @click="load"><RotateCcw :size="13" /> {{ t('Discard changes') }}</button>
      </div>
    </form>

    <div class="space-y-5 self-start">
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
        </p>
        <p class="px-3 pb-2 text-[11px] text-fg-muted">{{ t('Signed in as {user}.', { user: auth.user }) }}</p>
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
          <div class="grid grid-cols-2 gap-2">
            <div>
              <label class="lbl" for="icu">{{ t('User') }}</label
              ><input id="icu" v-model.trim="importForm.user" class="inp" autocomplete="off" required />
            </div>
            <div>
              <label class="lbl" for="icp">{{ t('Password') }}</label
              ><input id="icp" v-model="importForm.password" type="password" class="inp" autocomplete="new-password" required />
            </div>
          </div>
          <div class="flex gap-2">
            <button class="btn-ghost" :disabled="importBusy"><KeyRound :size="13" /> {{ t('Save') }}</button>
            <button v-if="importCred.set" type="button" class="btn-ghost" @click="removeImportCred">
              <Trash2 :size="13" /> {{ t('Remove') }}
            </button>
          </div>
        </form>
      </section>

      <section class="card">
        <div class="card-h">{{ t('File server agent token') }}</div>
        <div class="space-y-3 px-3 py-3 text-[12px]">
          <p class="text-fg-muted">
            {{ t('Register the token in Shares → + new, and run the command on the file server: it keeps only the hash.') }}
          </p>
          <button type="button" class="btn-ghost" @click="newAgentToken"><KeyRound :size="13" /> {{ t('Generate token') }}</button>
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
    </div>
  </div>
</template>
