<script setup lang="ts">
// 未打开集合时的欢迎页：打开目录（桌面 = 系统对话框；devserver = 启动参数）。
import { NButton } from 'naive-ui'
import { useI18n } from 'vue-i18n'

defineProps<{ lastDir: string }>()
const emit = defineEmits<{ open: [dir: string] }>()
const { t } = useI18n()
</script>

<template>
  <div class="welcome">
    <h1>{{ t('welcome.title') }}</h1>
    <p class="hint">{{ t('welcome.hint') }}</p>
    <n-button type="primary" size="large" @click="emit('open', '')">{{ t('welcome.open') }}</n-button>
    <p v-if="lastDir" class="last">
      <button class="link" @click="emit('open', lastDir)">
        {{ t('welcome.lastOpened', { dir: lastDir }) }}
      </button>
    </p>
  </div>
</template>

<style scoped>
.welcome {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
}

h1 {
  margin: 0;
  font-size: 22px;
  letter-spacing: 1px;
}

.hint {
  color: var(--app-muted);
  margin: 0 0 8px;
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
  padding: 0;
}
</style>
