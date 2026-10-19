<script setup>
// Login with the same measures as a Roundcube webmail skin (
// measured on the production webmail): 580 px form, 480 × 267 px navy box with an
// orange stripe, 195 × 65 logo, 41 px rows, 326 × 25 inputs, 65 × 26 button and
// Larry's elliptic shadow. Errors and hints stay outside the box.
// With a token it is the public page of a password link (/password/<token>).
import { ref } from 'vue'
import { brand, get, loadMe, logoUrl, post } from '../api'
import { formatDate } from '../util'
import { languages, lang, setLang, t } from '../i18n'
import defaultLogo from '../assets/ad-manager-logo-light.png'
import darkInkLogo from '../assets/ad-manager-logo.png'

const props = defineProps({ token: { type: String, default: '' } })
const user = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

// password link: the first access claims it for this browser
const link = ref(null)
const confirm = ref('')
const changed = ref(false)
if (props.token)
  get(`/password-link/${props.token}`)
    .then((l) => (link.value = l))
    .catch((e) => (error.value = e.message))

async function changeByLink() {
  error.value = ''
  if (password.value !== confirm.value) return (error.value = t('The confirmation does not match.'))
  busy.value = true
  try {
    await post(`/password-link/${props.token}`, { password: password.value })
    changed.value = true
    link.value = null
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function signIn() {
  busy.value = true
  error.value = ''
  try {
    await post('/login', { user: user.value, password: password.value })
    password.value = ''
    // came from /asynqmon or /swagger without a session: go back there (only these targets, never a free URL)
    const next = new URLSearchParams(location.search).get('next')
    if (next === '/asynqmon' || next === '/swagger') return location.assign(next)
    await loadMe()
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}
// domain and NetBIOS come from the public /api/health so they show before login
const domain = () => brand.domain || 'example.net'
const netbios = () => brand.netbios || domain().split('.')[0].toUpperCase()
</script>

<template>
  <div class="screen">
    <form id="login-form" @submit.prevent="token ? changeByLink() : signIn()">
      <div class="box-inner">
        <div class="top">
          <img v-if="brand.logo" :src="logoUrl()" :alt="brand.company || t('Company logo')" class="logo logo-plate" />
          <template v-else>
            <img :src="defaultLogo" alt="AD Manager" class="logo" style="display: var(--login-logo-light-ink)" />
            <img :src="darkInkLogo" alt="AD Manager" class="logo" style="display: var(--login-logo-dark-ink)" />
          </template>
          <div class="ad">
            <div class="ad-title">Active Directory</div>
            <div class="ad-domain">
              {{ netbios() }} <span>({{ domain() }})</span>
            </div>
          </div>
        </div>
        <template v-if="token">
          <p v-if="changed" class="done">{{ t('Password changed. Use the new password from now on.') }}</p>
          <table v-else-if="link">
            <tbody>
              <tr>
                <td class="title">{{ t('User') }}</td>
                <td class="input who">
                  {{ link.name || link.user }} <span>({{ link.user }})</span>
                </td>
              </tr>
              <tr>
                <td class="title">
                  <label for="lnk-new">{{ t('New password') }}</label>
                </td>
                <td class="input">
                  <input id="lnk-new" v-model="password" type="password" autocomplete="new-password" required autofocus />
                </td>
              </tr>
              <tr>
                <td class="title">
                  <label for="lnk-confirm">{{ t('Confirm') }}</label>
                </td>
                <td class="input"><input id="lnk-confirm" v-model="confirm" type="password" autocomplete="new-password" required /></td>
              </tr>
            </tbody>
          </table>
          <p v-if="link" class="formbuttons">
            <button class="button" :disabled="busy">{{ busy ? '…' : t('Change password') }}</button>
          </p>
        </template>
        <table v-else>
          <tbody>
            <tr>
              <td class="title">
                <label for="rcmloginuser">{{ t('User') }}</label>
              </td>
              <td class="input"><input id="rcmloginuser" v-model="user" autocomplete="username" required autofocus /></td>
            </tr>
            <tr>
              <td class="title">
                <label for="rcmloginpwd">{{ t('Password') }}</label>
              </td>
              <td class="input"><input id="rcmloginpwd" v-model="password" type="password" autocomplete="current-password" required /></td>
            </tr>
          </tbody>
        </table>
        <p v-if="!token" class="formbuttons">
          <button class="button" :disabled="busy">{{ busy ? '…' : t('Sign in') }}</button>
        </p>
      </div>
      <div class="box-bottom">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="token && link" class="hint">
          {{ t('Single-use link, valid until {date}. Your current password no longer works.', { date: formatDate(link.expires) }) }}
        </p>
        <p v-if="!token" class="hint">
          {{ t('Use') }} <code>{{ t('user') }}</code
          >, <code>{{ t('user') }}@{{ domain() }}</code> {{ t('or') }} <code>{{ netbios() }}\{{ t('user') }}</code
          >.<br />
          {{ t('Domain administrators only; every action is audited.') }}
        </p>
        <p class="langs">
          <button v-for="[code, name] in languages" :key="code" type="button" :class="{ on: lang === code }" @click="setLang(code)">
            {{ name }}
          </button>
        </p>
      </div>
    </form>
  </div>
</template>

<style scoped>
.screen {
  position: fixed;
  inset: 0;
  overflow: auto;
  /* same dotted pattern as the panel (body) */
  background: var(--color-login-page) var(--bg-page);
  background-size: 22px 22px;
  font-size: 12px;
}
#login-form {
  position: relative;
  width: 580px;
  max-width: calc(100% - 32px);
  margin: calc(var(--view-h) * 0.1) auto 1.28%; /* 10% of the window height from the top */
}
.box-inner {
  box-sizing: border-box;
  width: 480px;
  max-width: 100%;
  min-height: 267px;
  margin: 0 auto;
  padding: 10px 24px 24px;
  background: var(--color-login-box);
  border: 1px solid var(--color-login-box-line);
  border-top: 4px solid var(--color-login-box-bar);
  box-shadow: var(--shadow-login-box);
}
.top {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  margin: 0 0 23px;
}
.logo {
  display: block;
  width: 195px;
  height: 65px;
  object-fit: contain;
}
.ad {
  padding-bottom: 6px;
  text-align: right;
}
.ad-title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: var(--color-login-title);
  text-transform: uppercase;
}
.ad-domain {
  margin-top: 2px;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  color: var(--color-on-login-box-strong);
}
.ad-domain span {
  color: var(--color-on-login-box-faint);
}
.hint {
  width: 480px;
  max-width: 100%;
  margin: 4px auto 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--color-fg-muted);
  text-align: center;
}
.done {
  margin: 30px 0;
  font-size: 13px;
  line-height: 1.6;
  color: var(--color-on-login-box);
  text-align: center;
}
td.who {
  font-weight: 600;
  color: var(--color-on-login-box);
}
td.who span {
  font-weight: 400;
  color: var(--color-on-login-box-muted);
}
.hint code {
  font-size: 10.5px;
  color: var(--color-fg-strong);
}
/* same table as Roundcube: cells 2 px apart */
table {
  border-collapse: separate;
  border-spacing: 2px;
}
tr {
  height: 41px;
}
td.title {
  width: 83px;
  height: 41px;
  box-sizing: border-box;
  padding: 1px 12px 1px 1px;
  color: var(--color-on-login-box);
  text-align: right;
  white-space: nowrap;
}
td.input {
  box-sizing: border-box;
  width: 332px;
  padding: 8px;
}
td.input input {
  box-sizing: border-box;
  width: 326px;
  max-width: 100%;
  height: 25px;
  padding: 4px;
  font-size: 12px;
  color: var(--color-autofill);
  background: var(--color-field);
  border: 1px solid var(--color-login-field-line);
  outline: none;
}
td.input input:focus {
  border-color: var(--color-field-focus);
  box-shadow: 0 0 0 var(--focus-ring-w) var(--color-login-focus);
}
/* a field filled by the browser keeps the field color under the focus ring (the ring would replace the inset shadow
   that hides the browser's autofill background) */
td.input input:-webkit-autofill:focus {
  box-shadow:
    0 0 0 var(--focus-ring-w) var(--color-login-focus),
    0 0 0 1000px var(--color-field) inset;
}
.formbuttons {
  margin: 24px 0 12px;
  text-align: center;
}
.button {
  height: 26px;
  min-width: 65px;
  padding: 4px 12px;
  font-size: 13.2px;
  font-weight: 700;
  color: var(--color-on-brand-action);
  cursor: pointer;
  /* volume like the panel buttons: the theme's gradient and shadow (the flat look turns both off) */
  background: var(--color-brand-action) var(--bg-brand-action);
  border: 1px solid var(--color-brand-action-hover);
  box-shadow: var(--shadow-brand-action);
}
.button:hover,
.button:focus {
  background: var(--color-brand-action-hover);
}
.button:active:not(:disabled) {
  transform: translateY(1px);
  box-shadow: var(--shadow-btn-active);
}
.button:disabled {
  opacity: 0.7;
}
.box-bottom {
  min-height: 60px;
  margin-top: -3px;
  padding-top: 10px;
  /* the elliptic shadow under the box: --login-shadow-img (a light glow on dark themes) */
  background-image: var(--login-shadow-img);
  background-repeat: no-repeat;
  background-position: top center;
}
/* flat look: no shadow image under the box either */
:root[data-flat='true'] .box-bottom {
  background-image: none !important;
}
.error {
  width: 480px;
  max-width: 100%;
  margin: 6px auto 0;
  padding: 6px 10px;
  color: var(--color-on-err-soft);
  background: var(--color-err-soft);
  border-left: 4px solid var(--color-toast-err);
}
.langs {
  margin: 10px 0 0;
  text-align: center;
}
.langs button {
  margin: 0 6px;
  font-size: 11px;
  color: var(--color-fg-muted);
  cursor: pointer;
  background: none;
  border: 0;
}
.langs button.on {
  font-weight: 600;
  color: var(--color-fg-strong);
  text-decoration: underline;
}
</style>
