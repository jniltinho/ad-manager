<script setup>
import { FileCog, FolderPlus, Trash2 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { auth, get, del } from '../api'
import { toast } from '../toasts'
import { askConfirm } from '../dialogs'
import { t } from '../i18n'
import NewOrgUnitDialog from '../components/dialogs/NewOrgUnitDialog.vue'
import GpoListDialog from '../components/dialogs/GpoListDialog.vue'
import LoadingRow from '../components/LoadingRow.vue'

const list = ref([])
const loaded = ref(false) // the first reply arrived: until then the table shows it is loading
async function load() {
  try {
    list.value = await get('/ous')
  } catch (e) {
    toast.error(e.message)
  }
  loaded.value = true
}
load()

// ── GPOs and where they are linked: listed, backed up and restored in GpoListDialog
const gpos = ref(false)

const base = computed(() =>
  auth.domain
    .split('.')
    .map((p) => 'DC=' + p)
    .join(','),
)
// tree: sort by the path from root to leaf and indent by depth
const tree = computed(() =>
  list.value
    .map((o) => {
      const parts = o.dn
        .slice(0, -(base.value.length + 1))
        .split(/,(?=OU=|CN=)/)
        .reverse()
      return { ...o, level: parts.length - 1, key: parts.join('/').toLowerCase(), ou: o.dn.startsWith('OU=') }
    })
    .sort((a, b) => a.key.localeCompare(b.key)),
)

// ── new OU (the fields live in NewOrgUnitDialog)
const draft = ref(null)
function created() {
  draft.value = null
  load()
}
async function remove(o) {
  if (!(await askConfirm(t('Delete the OU {name}? Only possible when it is empty.', { name: o.name }), { ok: t('Delete') }))) return
  try {
    await del('/ous?dn=' + encodeURIComponent(o.dn))
    toast.removed(t('OU {name} deleted.', { name: o.name }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
</script>

<template>
  <div class="mb-3 flex items-center gap-2">
    <span class="text-[12px] text-fg-muted"
      >{{ t('Organizational units and default containers of') }} <b>{{ auth.domain }}</b></span
    >
    <button class="btn-ghost ml-auto" @click="gpos = true"><FileCog :size="13" /> GPOs</button>
    <button class="btn" @click="draft = { name: '', description: '', parent: '' }"><FolderPlus :size="13" /> {{ t('New OU') }}</button>
  </div>
  <table class="grid-t">
    <thead>
      <tr>
        <th>{{ t('Name') }}</th>
        <th>{{ t('Description') }}</th>
        <th>DN</th>
        <th class="text-right">{{ t('Actions') }}</th>
      </tr>
    </thead>
    <tbody>
      <LoadingRow v-if="!loaded" :cols="4" />
      <tr v-for="o in tree" :key="o.dn">
        <td :style="{ paddingLeft: 8 + o.level * 22 + 'px' }">
          <span class="mr-1 text-accent">{{ o.level ? '└' : '■' }}</span>
          <b>{{ o.name }}</b> <span v-if="!o.ou" class="tag bg-surface-2 text-fg-muted">{{ t('container') }}</span>
        </td>
        <td>{{ o.description }}</td>
        <td class="font-mono text-[11px] text-fg-muted">{{ o.dn }}</td>
        <td class="text-right">
          <button v-if="o.ou && o.name !== 'Domain Controllers'" class="act hover:!text-err" @click="remove(o)">
            <Trash2 :size="12" /> {{ t('delete') }}
          </button>
        </td>
      </tr>
    </tbody>
  </table>

  <NewOrgUnitDialog v-if="draft" :draft="draft" :parents="tree.filter((x) => x.ou)" @close="draft = null" @done="created" />

  <GpoListDialog v-if="gpos" @close="gpos = false" />
</template>
