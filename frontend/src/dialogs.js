// Panel dialogs (as in the earlier panel this one grew from): replace the
// native confirm()/alert() with a themed <dialog>. Every helper returns a Promise:
//   askConfirm(msg, {title, ok, cancel})   -> Promise<boolean>
//   showAlert(msg, {title, ok})            -> Promise<void>
//   confirmTyped(msg, expected, opts)      -> Promise<boolean>
import { reactive } from 'vue'
import { t } from './i18n'

// dialogState drives PanelDialogs.vue (one per app).
export const dialogState = reactive({
  confirm: null, // { title, msg, ok, cancel, resolve }
  typed: null, // { title, msg, expected, value, resolve }
  alert: null, // { title, msg, ok, resolve }
})

// cancelDialogs answers every open dialog as cancelled (a new session: nothing confirmed for the old one runs)
export function cancelDialogs() {
  for (const k of ['confirm', 'typed', 'alert']) {
    const d = dialogState[k]
    dialogState[k] = null
    d?.resolve(k === 'alert' ? undefined : false)
  }
}

export function askConfirm(msg, opts = {}) {
  return new Promise((resolve) => {
    dialogState.confirm = { title: opts.title || t('Confirmation'), msg, ok: opts.ok || 'OK', cancel: opts.cancel || t('Cancel'), resolve }
  })
}

export function showAlert(msg, opts = {}) {
  return new Promise((resolve) => {
    dialogState.alert = { title: opts.title || t('Notice'), msg, ok: opts.ok || 'OK', resolve }
  })
}

// confirmTyped guards destructive actions: the button only enables when the
// typed text matches (trimmed, case-insensitive).
export function confirmTyped(msg, expected, opts = {}) {
  return new Promise((resolve) => {
    dialogState.typed = {
      title: opts.title || t('Safety confirmation'),
      msg: msg || t('This action is destructive and cannot be undone.'),
      expected,
      value: '',
      resolve,
    }
  })
}
