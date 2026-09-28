<script setup lang="ts">
// 方法徽标：默认按设计稿用语义色文字（GET 绿 / POST 蓝…）；filled 变体给命令面板等需要色块的场景。
import { computed } from 'vue'
import { methodColor, methodTint } from '@/lib/method'

const props = withDefaults(defineProps<{ method: string; filled?: boolean }>(), { filled: false })

const name = computed(() => props.method.toUpperCase())
const style = computed(() =>
  props.filled
    ? { color: methodColor(name.value), background: methodTint(name.value) }
    : { color: methodColor(name.value) },
)
</script>

<template>
  <span class="mt" :class="{ filled }" :style="style">{{ name }}</span>
</template>

<style scoped>
.mt {
  display: inline-block;
  min-width: 34px;
  font-family: var(--app-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.4px;
  text-align: left;
  line-height: 16px;
  flex: 0 0 auto;
}

.mt.filled {
  min-width: 38px;
  padding: 0 5px;
  border-radius: 4px;
  text-align: center;
  line-height: 17px;
}
</style>
