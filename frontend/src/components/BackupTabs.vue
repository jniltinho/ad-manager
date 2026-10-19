<script setup>
// Backup and Sync share the Backup tab of the top bar: two pages, one place to look after the servers' data.
import { Archive, FolderSync } from 'lucide-vue-next'
import { useRoute } from 'vue-router'
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
  </div>
</template>
