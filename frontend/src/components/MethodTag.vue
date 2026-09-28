<script setup lang="ts">
// 方法徽标：按方法着色（GET 绿 / POST 蓝 / PUT 橙 / DELETE 红 …）。
const props = defineProps<{ method: string }>()

const colors: Record<string, string> = {
  GET: '#18a058',
  POST: '#4098fc',
  PUT: '#f0a020',
  DELETE: '#d03050',
  PATCH: '#8a2be2',
  HEAD: '#8a9199',
  OPTIONS: '#8a9199',
}
const color = colors[props.method.toUpperCase()] ?? '#909399'
</script>

<template>
  <span class="mt" :style="{ background: color }">{{ method.toUpperCase() }}</span>
</template>

<style scoped>
.mt {
  display: inline-block;
  min-width: 38px;
  padding: 0 5px;
  border-radius: 3px;
  color: #fff;
  font-size: 10px;
  line-height: 17px;
  text-align: center;
  font-weight: 600;
  letter-spacing: 0.5px;
}
</style>
