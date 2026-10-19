<script setup>
// The loading sign: a spinner (still when the system asks for less motion), "Loading…" only for screen readers
// (role="status").
import { LoaderCircle } from 'lucide-vue-next'
import { t } from '../i18n'
</script>

<template>
  <div role="status" class="flex items-center text-fg-muted">
    <LoaderCircle :size="20" class="shrink-0 motion-safe:animate-spin" aria-hidden="true" />
    <span class="sr-only">{{ t('Loading…') }}</span>
  </div>
</template>
