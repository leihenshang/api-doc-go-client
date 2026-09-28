<script setup lang="ts">
// 未打开集合时的欢迎页：打开目录（桌面 = 系统对话框；devserver = 启动参数）+ 六大特性卡。
import { NButton } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import FeatureCards from '@/components/FeatureCards.vue'

defineProps<{ lastDir: string }>()
const emit = defineEmits<{ open: [dir: string] }>()
const { t } = useI18n()
</script>

<template>
  <div class="welcome">
    <section class="hero">
      <span class="logo" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round">
          <path d="M22 2 11 13" />
          <path d="M22 2 15 22l-4-9-9-4 20-7Z" />
        </svg>
      </span>
      <h1>{{ t('welcome.title') }}</h1>
      <p class="hint">{{ t('welcome.hint') }}</p>
      <n-button type="primary" size="large" @click="emit('open', '')">{{ t('welcome.open') }}</n-button>
      <p v-if="lastDir" class="last">
        <button class="link" type="button" @click="emit('open', lastDir)">
          {{ t('welcome.lastOpened', { dir: lastDir }) }}
        </button>
      </p>
    </section>

    <feature-cards />
  </div>
</template>

<style scoped>
.welcome {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  background: var(--app-bg);
}

.hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 48px 24px 8px;
}

.logo {
  color: var(--app-accent);
}

h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
}

.hint {
  color: var(--app-muted);
  margin: 0 0 6px;
  font-size: 13px;
  text-align: center;
}

.last {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-muted);
}

.link {
  border: none;
  background: none;
  color: var(--app-accent);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  padding: 0;
}
</style>
