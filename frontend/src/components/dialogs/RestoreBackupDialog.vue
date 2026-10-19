<script setup>
// Restore a backup: what changes is shown first (folders and where for data, parts for the configuration); anything
// that replaces what is in use asks for the end of the id. Calls "done" with the server's answer once queued.
import { computed, ref } from 'vue'
import { post, enc } from '../../api'
import { t } from '../../i18n'
import { toast } from '../../toasts'
import { formatDate } from '../../util'
import Modal from '../Modal.vue'

const props = defineProps({
  backup: { type: Object, required: true },
  base: { type: String, required: true }, // the server's backups route
  kindName: { type: Function, required: true },
  // a prop rather than an emitted event: the request may end after the dialog closed, and the page still reloads
  onDone: { type: Function, default: null },
})
const emit = defineEmits(['close'])

const b = props.backup
const target = ref('new')
const folders = ref([...(b.folders || [])])
const sub = ref('')
const parts = ref(['shares', 'acls'])
const confirm = ref('')
const busy = ref(false)
const code = b.id.slice(-8)
const overwrites = computed(() => b.kind !== 'fileserver-data' || target.value === 'original')

async function restore() {
  busy.value = true
  try {
    const body = { confirm: confirm.value.trim() }
    if (b.kind === 'fileserver-data')
      Object.assign(body, { target: target.value, folders: sub.value.trim() ? [sub.value.trim()] : folders.value })
    if (b.kind === 'fileserver-config') body.parts = parts.value
    props.onDone?.(await post(`${props.base}/${enc(b.id)}/restore`, body))
  } catch (e) {
    toast.error(e.message)
    busy.value = false
  }
}
</script>

<template>
  <Modal :title="t('Restore {id}', { id: backup.id })" :busy="busy" :ok="t('Restore')" @close="emit('close')" @submit="restore">
    <div class="space-y-3 text-[12px]">
      <p>
        <b>{{ kindName(backup.kind) }}</b> · {{ formatDate(backup.started) }}
        <span v-if="backup.kind === 'fileserver-data'" class="text-fg-muted">
          · {{ backup.full ? t('full') : t('incremental: the full backup and the incrementals up to this one are applied in order') }}
        </span>
      </p>
      <template v-if="backup.kind === 'fileserver-data'">
        <fieldset>
          <legend class="lbl">{{ t('Folders') }}</legend>
          <label v-for="f in backup.folders" :key="f" class="flex items-center gap-2 font-mono">
            <input v-model="folders" type="checkbox" :value="f" :disabled="!!sub.trim()" /> {{ f }}
          </label>
          <label class="lbl mt-2" for="rsub">{{ t('Or only one folder inside them') }}</label>
          <input
            id="rsub"
            v-model="sub"
            class="inp font-mono"
            autocomplete="off"
            :placeholder="(backup.folders?.[0] || '/srv/shares') + '/Financeiro'"
          />
        </fieldset>
        <fieldset class="space-y-1">
          <legend class="lbl">{{ t('Where') }}</legend>
          <label class="flex items-start gap-2">
            <input v-model="target" type="radio" value="new" class="mt-0.5" />
            <span>{{ t('In a new folder: [share root]/_restored/[date]/ (nothing current is touched)') }}</span>
          </label>
          <label class="flex items-start gap-2">
            <input v-model="target" type="radio" value="original" class="mt-0.5" />
            <span>{{
              t('Over the original folders: they become a backup first, then go back to how this backup has them (newer files are removed)')
            }}</span>
          </label>
        </fieldset>
      </template>
      <fieldset v-else-if="backup.kind === 'fileserver-config'">
        <legend class="lbl">{{ t('What to restore (the current configuration becomes a backup first)') }}</legend>
        <label class="flex items-center gap-2"><input v-model="parts" type="checkbox" value="shares" /> {{ t('Shares (registry)') }}</label>
        <label class="flex items-center gap-2"
          ><input v-model="parts" type="checkbox" value="acls" /> {{ t('Permissions of the share roots') }}</label
        >
        <label class="flex items-center gap-2"
          ><input v-model="parts" type="checkbox" value="smbconf" />
          {{ t('smb.conf (checked with testparm; the current one is kept)') }}</label
        >
      </fieldset>
      <p v-else-if="backup.kind === 'samba-files'" class="text-warn">
        {{
          t(
            'Samba stops; /etc/samba, /var/lib/samba and krb5.conf are moved aside (…before-restore-[time]) and come back as this backup has them; Samba starts again. On a DC this undoes everything done to the domain since.',
          )
        }}
      </p>
      <p v-else-if="backup.kind === 'panel'" class="text-warn">
        {{
          t(
            "The panel's database (settings, registered servers, sources, links) goes back to this backup; the current one becomes a backup first. The panel restarts. The configuration file and the audit log are not restored.",
          )
        }}
      </p>
      <div v-if="overwrites">
        <label class="lbl" for="rc">{{ t('This replaces what is in use. Type {code} to confirm', { code }) }}</label>
        <input id="rc" v-model="confirm" class="inp font-mono" required autocomplete="off" />
      </div>
    </div>
  </Modal>
</template>
