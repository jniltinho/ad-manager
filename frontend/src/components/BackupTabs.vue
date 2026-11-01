<script setup>
// Backup, Sync and Terminal share the Backup tab of the top bar: one place to look after the servers.
import { Archive, FolderSync, SquareTerminal } from 'lucide-vue-next'
import { useRoute } from 'vue-router'
import { auth, platform } from '../api'
import { t } from '../i18n'

const route = useRoute()
</script>

<template>
  <div class="subtabs">
    <router-link
      v-for="[to, label, icon] in [
        ['/backup', 'Backup', Archive],
        ['/sync', 'Sync', FolderSync],
      ]"
      :key="to"
      :to="to"
      class="subtab"
      :class="{ on: route.path === to }"
    >
      <component :is="icon" :size="13" /> {{ t(label) }}
    </router-link>
    <template v-if="auth.settings?.ssh_terminal">
      <!-- the desktop's proxy carries no WebSocket -->
      <span
        v-if="platform.configure"
        class="subtab cursor-not-allowed opacity-50"
        aria-disabled="true"
        :title="t('The SSH terminal is not available in the desktop app')"
      >
        <SquareTerminal :size="13" /> {{ t('Terminal') }}
      </span>
      <router-link v-else to="/terminal" class="subtab" :class="{ on: route.path === '/terminal' }">
        <SquareTerminal :size="13" /> {{ t('Terminal') }}
      </router-link>
    </template>
  </div>
</template>
