<script setup>
// GPOs and where they are linked (from LDAP), loaded on open. A GPO can be backed up by the DC's agent (downloaded as
// .tar.gz) and a backup restored as a new, unlinked GPO.
import { Download, Upload } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { auth, get, post, enc } from '../../api'
import { toast } from '../../toasts'
import { confirmTyped, showAlert } from '../../dialogs'
import { serverText, t } from '../../i18n'
import { savePanelBlob } from '../../util'
import Modal from '../Modal.vue'
import LoadingNote from '../LoadingNote.vue'

const emit = defineEmits(['close'])

const gpo = ref(null)
async function load() {
  gpo.value = { loading: true }
  try {
    gpo.value = await get('/gpos')
  } catch (e) {
    gpo.value = { error: e.message }
  }
}
load()
// the first RDN of a DN, for a short label ("OU=Financeiro,DC=…" → "Financeiro"); the domain root by its name
const shortDN = (dn) => (/^DC=/i.test(dn) ? auth.domain : dn.split(',')[0].replace(/^[A-Za-z]+=/, ''))
const blocked = computed(() => (gpo.value?.containers || []).filter((c) => c.block_inheritance))
// backup of a GPO by the agent on a domain controller, downloaded as .tar.gz (an error becomes a toast)
const backingUp = ref('')
async function backup(g) {
  backingUp.value = g.guid
  try {
    await savePanelBlob(`/api/gpos/${enc(g.guid)}/backup`, `gpo-${g.guid.replace(/[{}]/g, '')}.tar.gz`)
  } catch (e) {
    toast.error(e.message)
  }
  backingUp.value = ''
}

// restore a backup (.tar.gz from "backup") as a new, unlinked GPO: typed confirmation of the new name; Samba does
// not replicate SYSVOL, so the answer lists the other DCs to copy the GPO's folder to
const restoring = ref(false)
const restoreName = ref('')
const restoreFile = ref(null)
async function restore() {
  const name = restoreName.value.trim()
  const file = restoreFile.value
  if (!name || !file) return
  if (
    !(await confirmTyped(
      t('Create the new GPO {name} from {file}? It is created unlinked, with a new GUID.', { name, file: file.name }),
      name,
    ))
  )
    return
  restoring.value = true
  try {
    const url = await new Promise((resolve, reject) => {
      const r = new FileReader()
      r.onload = () => resolve(r.result)
      r.onerror = () => reject(r.error)
      r.readAsDataURL(file)
    })
    const res = await post('/gpos/restore', { name, archive: String(url).split(',')[1] || '' })
    toast.success(t('GPO {name} created ({guid}).', { name, guid: res.guid }))
    restoreName.value = ''
    restoreFile.value = null
    if (res.sync.length)
      await showAlert(
        t('Samba does not replicate SYSVOL: copy the folder Policies/{guid} from {dc} to {dcs} before linking the GPO.', {
          guid: res.guid,
          dc: res.dc,
          dcs: res.sync.join(', '),
        }),
      )
    load()
  } catch (e) {
    toast.error(e.message)
  }
  restoring.value = false
}
</script>

<template>
  <Modal wide :title="t('Group Policy Objects (GPOs)')" :ok="null" @close="emit('close')">
    <LoadingNote v-if="gpo.loading" />
    <p v-else-if="gpo.error" class="text-err">{{ gpo.error }}</p>
    <template v-else>
      <table class="grid-t table-fixed">
        <thead>
          <tr>
            <th>{{ t('Name') }}</th>
            <th class="w-36">{{ t('Status') }}</th>
            <th class="w-44 text-right">{{ t('Version (computer / user)') }}</th>
            <th>{{ t('Linked to') }}</th>
            <th>{{ t('Extensions') }}</th>
            <th class="w-24"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in gpo.gpos" :key="g.guid">
            <td class="truncate font-semibold" :title="g.guid">{{ g.name || g.guid }}</td>
            <td class="truncate">
              <span v-if="g.machine_enabled && g.user_enabled">{{ t('in use') }}</span>
              <span v-else-if="!g.machine_enabled && !g.user_enabled" class="text-err">{{ t('turned off') }}</span>
              <span v-else class="text-warn">{{ g.machine_enabled ? t('user part disabled') : t('computer part disabled') }}</span>
            </td>
            <td class="text-right tabular-nums">{{ g.machine_version }} / {{ g.user_version }}</td>
            <td class="truncate" :title="g.links.map((l) => l.target).join('\n')">
              <span v-if="!g.links.length" class="text-fg-muted">{{ t('not linked') }}</span>
              <span v-for="(l, i) in g.links" :key="l.target" :class="l.disabled ? 'text-fg-subtle line-through' : ''"
                >{{ i ? ', ' : '' }}{{ shortDN(l.target) }}{{ l.enforced ? ' (' + t('enforced') + ')' : '' }}</span
              >
            </td>
            <td class="truncate text-fg-muted" :title="g.extensions.join(', ')">{{ g.extensions.join(', ') || '—' }}</td>
            <td class="text-right">
              <button
                type="button"
                class="act"
                :disabled="!!backingUp"
                :title="t('Back up this GPO (needs the agent installed on a domain controller)')"
                @click="backup(g)"
              >
                <Download :size="12" /> {{ backingUp === g.guid ? '…' : t('backup') }}
              </button>
            </td>
          </tr>
          <tr v-if="!gpo.gpos.length">
            <td colspan="6" class="py-3 text-center text-fg-muted">{{ t('No GPOs.') }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="blocked.length" class="mt-2 text-[12px]">
        {{ t('Inheritance blocked in') }}: <b>{{ blocked.map((c) => shortDN(c.dn)).join(', ') }}</b>
      </p>
      <p class="mt-2 text-[11px] text-fg-muted">
        {{ t('From the directory (LDAP). A struck-through link is disabled; in a container, link order 1 wins.') }}
      </p>
      <form class="mt-3 flex flex-wrap items-center gap-2 border-t border-line pt-3" @submit.prevent="restore">
        <span class="text-[12px] font-semibold">{{ t('Restore a backup as a new GPO') }}</span>
        <input
          type="file"
          accept=".tar.gz,.tgz,application/gzip"
          class="text-[12px]"
          required
          :aria-label="t('Backup file')"
          @change="restoreFile = $event.target.files[0] || null"
        />
        <input
          v-model="restoreName"
          class="inp w-64"
          required
          maxlength="128"
          :placeholder="t('Name of the new GPO')"
          :aria-label="t('Name of the new GPO')"
        />
        <button class="btn" :disabled="restoring || !restoreFile || !restoreName.trim()">
          <Upload :size="13" /> {{ restoring ? '…' : t('Restore') }}
        </button>
      </form>
      <p class="mt-1 text-[11px] text-fg-muted">
        {{ t('Needs the agent on a domain controller in its GPO restore mode (gpo_restore_user). The new GPO is not linked.') }}
      </p>
    </template>
  </Modal>
</template>
