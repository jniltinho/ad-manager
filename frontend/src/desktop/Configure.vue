<script setup>
// Configure (desktop only): the panel URL, the account and this computer's preferences. Test reads the panel's
// certificate (a self-signed one is trusted only by the operator), its health, and signs in for real; Save is enabled
// only after a successful test of the values on screen and opens the app with that session. Language, appearance and
// graphics are this computer's: saved as they change, without a test (also offline, also before the first Save).
// The password is sent only for the test and never saved.
import { LoaderCircle, PlugZap, Save, ShieldAlert, ShieldCheck } from 'lucide-vue-next'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { platform } from '../api'
import { languages, serverText, t } from '../i18n'
import { formatDate } from '../util'
import { configure, language, themes } from './index.js'

const st = ref(null) // ConfigState (desktop.go)
const url = ref('')
const user = ref('')
const password = ref('')
const prefs = ref({ lang: '', theme: '', flat: '', safeGPU: false })
const busy = ref(false)
const result = ref(null) // TestResult of the values on screen
const trusted = ref('') // the fingerprint the operator trusted
const error = ref('')
const trustBtn = ref(null)
const saveBtn = ref(null)
let testID = Date.now() // ids only grow, also across a reload
const tested = ref(0) // the id of the successful test on screen

onMounted(async () => {
  st.value = await configure('State')
  url.value = st.value.url
  user.value = st.value.user
  trusted.value = st.value.fingerprint
  prefs.value = { lang: st.value.lang, theme: st.value.theme, flat: st.value.flat, safeGPU: st.value.safeGPU }
})

// an edit drops every test so far (Go signs a tested session out, and a test still running is never adopted) and
// disables Save
function dropTests() {
  if (busy.value || tested.value) configure('Cancel', testID)
  testID++
  tested.value = 0
  result.value = null
  busy.value = false
}
watch([url, user, password], dropTests)

const canSave = computed(() => !!tested.value && !!result.value?.ok)
const themeList = computed(() => themes())

async function test() {
  error.value = ''
  busy.value = true
  const id = ++testID
  try {
    const r = await fetch('/desktop/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, url: url.value.trim(), user: user.value.trim(), password: password.value, trust: trusted.value }),
    })
    const res = await r.json()
    if (res.stale || id !== testID) return // edited or cancelled meanwhile
    result.value = res
    busy.value = false // before focusing a button it enables
    if (res.ok) {
      tested.value = id
      await nextTick()
      saveBtn.value?.focus()
    } else if (res.cert) {
      await nextTick()
      trustBtn.value?.focus()
    }
  } catch (e) {
    result.value = { error: e.message }
  } finally {
    if (id === testID) busy.value = false
  }
}

function trust() {
  trusted.value = result.value.cert.pin
  test()
}

async function save() {
  error.value = ''
  try {
    await configure('Save', tested.value, prefs.value.lang, prefs.value.theme, prefs.value.flat, prefs.value.safeGPU)
    password.value = ''
    location.assign('/') // the app again, with the new panel's session
  } catch (e) {
    error.value = e.message
  }
}

// preferences apply at once on screen and are saved as they change
const prefsSaved = ref(false)
watch(
  () => ({ ...prefs.value }),
  async (p, old) => {
    if (!st.value || !old) return
    if (p.lang !== old.lang) language(p.lang)
    Object.assign(platform.appearance, { theme: p.theme, flat: p.flat })
    try {
      await configure('SavePreferences', p.lang, p.theme, p.flat, p.safeGPU)
      prefsSaved.value = true
      error.value = ''
    } catch (e) {
      error.value = e.message
    }
  },
)

function close() {
  dropTests()
  platform.screen = null
}

// the file's own problem (loose permissions) is a path and a mode: translated by pattern
const problem = computed(() => {
  const p = st.value?.problem || ''
  const m = /^(.*) must be private and has mode (\d+): run chmod 600$/.exec(p)
  return m ? t('{path} must be private and has mode {mode}: run chmod 600', { path: m[1], mode: m[2] }) : serverText(p)
})
</script>

<template>
  <main class="mx-auto my-6 w-[min(720px,calc(100%-32px))] space-y-3 text-[12px] text-fg">
    <section class="card">
      <div class="card-h">{{ t('Configure') }}</div>
      <form v-if="st" class="grid gap-x-4 gap-y-3 p-3 md:grid-cols-2" @submit.prevent="test">
        <p v-if="problem" class="text-err md:col-span-2" role="alert">{{ problem }}</p>
        <div class="md:col-span-2">
          <label class="lbl" for="c-url">{{ t('Panel URL') }}</label>
          <input
            id="c-url"
            v-model="url"
            class="inp"
            placeholder="https://panel.example.local:8443"
            autocomplete="url"
            required
            autofocus
          />
        </div>
        <div>
          <label class="lbl" for="c-user">{{ t('User') }}</label>
          <input id="c-user" v-model="user" class="inp" autocomplete="username" required />
        </div>
        <div>
          <label class="lbl" for="c-pass">{{ t('Password') }}</label>
          <input id="c-pass" v-model="password" type="password" class="inp" autocomplete="current-password" required />
        </div>
        <div class="flex items-center gap-2 md:col-span-2">
          <button class="btn" :disabled="busy">
            <LoaderCircle v-if="busy" :size="14" class="motion-safe:animate-spin" /><PlugZap v-else :size="14" />
            {{ busy ? t('Testing…') : t('Test connection') }}
          </button>
          <span class="text-fg-muted">{{ t('Signs in with this account; the password is never saved.') }}</span>
        </div>
        <div v-if="result?.error" class="text-err md:col-span-2" role="alert">{{ serverText(result.error) }}</div>
        <div v-if="result?.ok" class="flex items-center gap-2 text-ok md:col-span-2" role="status">
          <ShieldCheck :size="14" />
          {{
            t('Connected to {domain} as {user} (panel {version}).', { domain: result.domain, user: result.user, version: result.version })
          }}
        </div>
        <div v-if="result?.cert" class="space-y-1 border border-line bg-warn-soft text-on-warn-soft p-2 md:col-span-2" role="alert">
          <div class="flex items-center gap-2 font-semibold"><ShieldAlert :size="14" /> {{ t('Certificate not trusted') }}</div>
          <p>
            {{ t('Compare the fingerprint with the one the panel logs at start (journalctl -u ad-manager | grep sha256).') }}
          </p>
          <dl class="grid grid-cols-[auto_1fr] gap-x-3">
            <dt class="text-fg-muted">{{ t('Fingerprint') }}</dt>
            <dd class="font-mono break-all">{{ result.cert.pin }}</dd>
            <dt class="text-fg-muted">{{ t('Subject') }}</dt>
            <dd>{{ result.cert.subject }}</dd>
            <dt class="text-fg-muted">{{ t('Issuer') }}</dt>
            <dd>{{ result.cert.issuer }}</dd>
            <dt class="text-fg-muted">{{ t('Valid') }}</dt>
            <dd>{{ formatDate(result.cert.notBefore) }} – {{ formatDate(result.cert.notAfter) }}</dd>
          </dl>
          <button ref="trustBtn" type="button" class="btn" @click="trust">{{ t('Trust this certificate') }}</button>
        </div>
      </form>
    </section>

    <section v-if="st" class="card">
      <div class="card-h">{{ t('This computer') }}</div>
      <div class="grid gap-x-4 gap-y-3 p-3 md:grid-cols-3">
        <div>
          <label class="lbl" for="c-lang">{{ t('Language') }}</label>
          <select id="c-lang" v-model="prefs.lang" class="inp">
            <option value="">{{ t('Panel default') }}</option>
            <option v-for="[code, name] in languages" :key="code" :value="code">{{ name }}</option>
          </select>
        </div>
        <div v-if="themeList.length">
          <label class="lbl" for="c-theme">{{ t('Theme') }}</label>
          <select id="c-theme" v-model="prefs.theme" class="inp">
            <option value="">{{ t('Panel default') }}</option>
            <option v-for="th in themeList" :key="th.id" :value="th.id">{{ t(th.label) }}</option>
          </select>
        </div>
        <div v-if="themeList.length">
          <label class="lbl" for="c-flat">{{ t('Look') }}</label>
          <select id="c-flat" v-model="prefs.flat" class="inp">
            <option value="">{{ t('Panel default') }}</option>
            <option value="flat">{{ t('Flat') }}</option>
            <option value="depth">{{ t('With depth') }}</option>
          </select>
        </div>
        <label v-if="st.os !== 'darwin'" class="flex items-center gap-2 md:col-span-3">
          <input v-model="prefs.safeGPU" type="checkbox" />
          {{ t('Safe rendering (GPU off): for a graphics driver that cannot draw the window. Applies at the next start.') }}
        </label>
      </div>
    </section>

    <div v-if="st" class="flex items-center gap-2">
      <button ref="saveBtn" type="button" class="btn" :disabled="busy || !canSave" @click="save">
        <Save :size="14" /> {{ t('Save') }}
      </button>
      <button v-if="st.configured" type="button" class="btn-ghost" @click="close">{{ t('Close') }}</button>
      <span class="text-fg-muted">{{
        prefsSaved ? t('This computer’s preferences are saved.') : t('This computer’s preferences are saved as they change.')
      }}</span>
      <span class="ml-auto text-fg-muted">{{ st.path }}</span>
    </div>
    <p v-if="error" class="text-err" role="alert">{{ error }}</p>
  </main>
</template>
