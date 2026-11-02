<script setup>
// Terminal: an SSH shell to a registered server's host (port 22), carried by the panel over a WebSocket. The SSH
// password, key and passphrase go only in the first message and are cleared once the shell opens; the host key is
// pinned on first use. In the desktop app the panel's WebSocket is opened in Go (platform.terminal).
import { FileKey, KeyRound, Plug, Unplug, RefreshCw, SquareTerminal } from 'lucide-vue-next'
import { nextTick, onUnmounted, ref } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import xtermColors from '../themes/xterm'
import { del, get, onSessionChange, platform } from '../api'
import { askConfirm } from '../dialogs'
import { serverText, t } from '../i18n'
import { toast } from '../toasts'
import { agentHost, spinner } from '../util'
import BackupTabs from '../components/BackupTabs.vue'

const agents = ref([])
const currentId = ref(0)
const loading = ref(false)
const spin = spinner(loading)
const form = ref({ user: 'root', password: '', key: '', keyName: '', passphrase: '' })
const state = ref('idle') // idle, connecting, open
const error = ref('')
const termEl = ref(null)
let ws = null
let term = null
let fit = null
let resizeObs = null
let attempts = 0 // the latest connect: an older socket's answers are ignored
const keyInput = ref(null)

async function load() {
  loading.value = true
  try {
    agents.value = await get('/agents')
    if (!agents.value.some((a) => a.id === currentId.value)) currentId.value = agents.value[0]?.id || 0
  } catch (e) {
    toast.error(e.message)
  }
  loading.value = false
}
const current = () => agents.value.find((a) => a.id === currentId.value)
function pick(id) {
  if (state.value !== 'idle') return // one terminal per page: disconnect first
  currentId.value = id
  error.value = ''
}

async function pickKey(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  if (f.size > 32 << 10) return toast.error(t('The key file is too large.'))
  form.value.key = await f.text()
  form.value.keyName = f.name
}
function clearKey() {
  form.value.key = form.value.keyName = form.value.passphrase = ''
}
function forget() {
  form.value.password = ''
  clearKey()
}

const codes = {
  auth_failed: 'SSH authentication failed',
  hostkey_changed: 'The host key of {server} changed',
  unreachable: 'The server does not answer on port 22',
  timeout: 'The server did not answer',
  closed: 'Connection closed',
  bad_request: 'Enter the user and a password or a key',
  internal: 'The panel could not open the terminal',
}

function ensureTerm() {
  if (term) return
  term = new Terminal({
    cursorBlink: true,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
    fontSize: 13,
    theme: xtermColors,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(termEl.value)
  fit.fit()
  // Shift+Insert pastes the clipboard, as in a desktop terminal (the browser does not send it to xterm.js)
  term.attachCustomKeyEventHandler((e) => {
    if (e.key !== 'Insert' || !e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return true
    e.preventDefault()
    // the desktop reads it in Go: its window denies clipboard reads to the page
    const read = platform.readClipboard || (() => navigator.clipboard.readText())
    if (e.type === 'keydown')
      Promise.resolve()
        .then(read)
        .then((text) => text && term.paste(text))
        .catch(() => toast.error(t('The browser did not allow reading the clipboard.')))
    return false
  })
  term.onData((d) => ws?.readyState === WebSocket.OPEN && state.value === 'open' && ws.send(new TextEncoder().encode(d)))
  term.onResize(({ cols, rows }) => state.value === 'open' && ws.send(JSON.stringify({ type: 'resize', cols, rows })))
  resizeObs = new ResizeObserver(() => fit.fit())
  resizeObs.observe(termEl.value)
}

async function connect(trust = '') {
  const a = current()
  if (!a) return
  error.value = ''
  state.value = 'connecting'
  await nextTick()
  ensureTerm()
  term.reset()
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  // the desktop's has the WebSocket's shape (onopen, onmessage, onclose, send, close, readyState)
  const sock = platform.terminal ? platform.terminal(a.id) : new WebSocket(`${proto}://${location.host}/api/agents/${a.id}/ssh`)
  const n = ++attempts
  ws = sock
  sock.binaryType = 'arraybuffer'
  let answered = false
  sock.onopen = () => {
    const { user, password, key, passphrase } = form.value
    sock.send(JSON.stringify({ user, password, key, passphrase, cols: term.cols, rows: term.rows, trust }))
  }
  sock.onmessage = async (ev) => {
    if (typeof ev.data !== 'string') return term.write(new Uint8Array(ev.data))
    answered = true
    const m = JSON.parse(ev.data)
    if (m.type === 'ready') {
      state.value = 'open'
      forget() // the credential is not kept once the shell is open
      term.focus()
    } else if (m.type === 'hostkey') {
      state.value = 'idle'
      const ok = await askConfirm(
        t('First connection to {server}. Check that its SSH host key fingerprint is:', { server: a.name }) +
          '\n\n' +
          `${m.key_type} ${m.fingerprint}`,
        { title: t('Trust the host key?'), ok: t('Trust and connect') },
      )
      if (ok && n === attempts) connect(m.fingerprint)
    } else if (m.type === 'error') {
      error.value = t(codes[m.code] || codes.internal, { server: a.name })
      if (state.value === 'open') term.write('\r\n\x1b[2m[' + error.value + ']\x1b[0m\r\n')
    }
  }
  sock.onclose = () => {
    if (ws !== sock) return
    // closed before any answer: the upgrade was refused (switched off, 4 terminals already, signed out) or dropped
    if (!answered && state.value === 'connecting')
      error.value = sock.error ? serverText(sock.error) : t('The terminal could not be opened.')
    if (state.value === 'open' && !error.value) term.write('\r\n\x1b[2m[' + t('Connection closed') + ']\x1b[0m\r\n')
    state.value = 'idle'
    ws = null
  }
}

function disconnect() {
  ws?.close()
}

async function resetKey() {
  const a = current()
  if (!a) return
  if (
    !(await askConfirm(t('Clear the SSH host key saved for {server}? The next connection asks to trust it again.', { server: a.name }), {
      ok: t('Clear'),
    }))
  )
    return
  try {
    await del(`/agents/${a.id}/ssh/hostkey`)
    error.value = ''
    toast.success(t('SSH host key cleared.'))
  } catch (e) {
    toast.error(e.message)
  }
}

const offSession = onSessionChange(() => ws?.close())
onUnmounted(() => {
  attempts++ // a trust confirmed after leaving the page connects nothing
  offSession()
  ws?.close()
  resizeObs?.disconnect()
  term?.dispose()
})
load()
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
              v-for="a in agents"
              :key="a.id"
              class="px-3 py-2 font-semibold"
              :class="[
                currentId === a.id ? 'nav-on bg-nav-on text-on-nav-on shadow-(--shadow-nav-on)' : 'hover:bg-row-hover',
                state === 'idle' ? 'cursor-pointer' : 'cursor-not-allowed',
              ]"
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
              "SSH to the host of the server's agent URL, port 22. The password or key is used only to sign in and is not kept. Commands run here skip the panel's protections and audit: the server's own login records are the record.",
            )
          }}
        </p>
      </aside>

      <section v-if="current()" class="card flex min-h-0 flex-col">
        <div class="card-h flex items-center gap-3">
          <SquareTerminal :size="14" /> {{ current().name }}
          <span class="font-normal normal-case text-fg-muted">
            {{ agentHost(current().url) }}:22<template v-if="state === 'open'">
              · {{ t('Connected as {user}', { user: form.user }) }}</template
            >
          </span>
          <!-- connected: the header is the whole bar, so the terminal gets the height -->
          <button v-if="state === 'open'" class="act ml-auto normal-case" @click="disconnect">
            <Unplug :size="12" /> {{ t('Disconnect') }}
          </button>
          <button v-else class="act ml-auto normal-case" :disabled="state !== 'idle'" @click="resetKey">
            <KeyRound :size="12" /> {{ t('Clear SSH host key') }}
          </button>
        </div>
        <!-- one slim row: the labels are for screen readers, the placeholders say what goes where -->
        <form v-if="state !== 'open'" class="flex flex-wrap items-center gap-2 px-3 py-2" @submit.prevent="connect()">
          <label class="sr-only" for="t-user">{{ t('User') }}</label>
          <input id="t-user" v-model.trim="form.user" class="inp w-36" :placeholder="t('User')" autocomplete="off" required />
          <label class="sr-only" for="t-pass">{{ t('Password') }}</label>
          <input id="t-pass" v-model="form.password" type="password" class="inp w-44" :placeholder="t('Password')" autocomplete="off" />
          <!-- the panel's own button, not the browser's file field (aligned with the inputs, translated) -->
          <input ref="keyInput" type="file" class="hidden" @change="pickKey" />
          <template v-if="form.keyName">
            <span class="tag bg-surface-2" :title="t('Private key')">{{ form.keyName }}</span>
            <button type="button" class="act" @click="clearKey">{{ t('remove') }}</button>
            <label class="sr-only" for="t-phrase">{{ t('Passphrase') }}</label>
            <input
              id="t-phrase"
              v-model="form.passphrase"
              type="password"
              class="inp w-36"
              :placeholder="t('Passphrase')"
              autocomplete="off"
            />
          </template>
          <button v-else type="button" class="act normal-case" @click="keyInput.click()">
            <FileKey :size="12" /> {{ t('Choose a key file (optional)') }}
          </button>
          <!-- the last outcome (refused, closed) on the same row, beside Connect -->
          <span class="ml-auto flex min-w-0 items-center gap-3">
            <span v-if="error" class="truncate text-err" role="alert" :title="error">{{ error }}</span>
            <button class="btn shrink-0" :disabled="state !== 'idle' || !form.user || (!form.password && !form.key)">
              <Plug :size="14" /> {{ state === 'connecting' ? t('Connecting…') : t('Connect') }}
            </button>
          </span>
        </form>
        <div class="term-box min-h-[320px] flex-1">
          <div ref="termEl" class="h-full w-full" data-testid="terminal"></div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* the terminal's own black fills the whole box (xterm.js paints its viewport, which covers the padding box) */
.term-box :deep(.xterm) {
  height: 100%;
  padding: 6px;
}
/* a thin scrollbar (xterm.js sets 14 px inline); its colors are the palette's (themes/xterm.js) */
.term-box :deep(.xterm-scrollable-element > .scrollbar.vertical),
.term-box :deep(.xterm-scrollable-element > .scrollbar.vertical > .slider) {
  width: 6px !important;
}
</style>
