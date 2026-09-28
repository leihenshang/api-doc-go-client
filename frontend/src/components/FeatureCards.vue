<script setup lang="ts">
// 本地优先·六大特性（design-spec §4）：3×2 白卡栅格，图标统一主色。
import { NIcon } from 'naive-ui'
import {
  CloudOfflineOutline,
  DocumentTextOutline,
  FlashOutline,
  GitBranchOutline,
  KeypadOutline,
  ShieldCheckmarkOutline,
} from '@vicons/ionicons5'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const items = computed(() => [
  { key: 'plain', icon: DocumentTextOutline, title: t('feature.plainTitle'), desc: t('feature.plainDesc') },
  { key: 'offline', icon: CloudOfflineOutline, title: t('feature.offlineTitle'), desc: t('feature.offlineDesc') },
  { key: 'git', icon: GitBranchOutline, title: t('feature.gitTitle'), desc: t('feature.gitDesc') },
  { key: 'search', icon: FlashOutline, title: t('feature.searchTitle'), desc: t('feature.searchDesc') },
  { key: 'keys', icon: KeypadOutline, title: t('feature.keysTitle'), desc: t('feature.keysDesc') },
  { key: 'privacy', icon: ShieldCheckmarkOutline, title: t('feature.privacyTitle'), desc: t('feature.privacyDesc') },
])
</script>

<template>
  <section class="features">
    <h2 class="ttl">{{ t('feature.title') }}</h2>
    <p class="sub">{{ t('feature.subtitle') }}</p>
    <div class="grid">
      <article v-for="it in items" :key="it.key" class="card">
        <div class="hd">
          <n-icon :component="it.icon" :size="17" class="ic" />
          <span class="ct">{{ it.title }}</span>
        </div>
        <p class="cd">{{ it.desc }}</p>
      </article>
    </div>
  </section>
</template>

<style scoped>
.features {
  padding: 22px 24px 26px;
}

.ttl {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: var(--app-text);
}

.sub {
  margin: 6px 0 16px;
  font-size: 12.5px;
  color: var(--app-muted);
}

.grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

@media (max-width: 900px) {
  .grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.card {
  background: var(--app-panel);
  border: 1px solid var(--app-border);
  border-radius: 10px;
  padding: 14px 16px 16px;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ic {
  color: var(--app-accent);
}

.ct {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--app-text);
}

.cd {
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--app-muted);
}
</style>
