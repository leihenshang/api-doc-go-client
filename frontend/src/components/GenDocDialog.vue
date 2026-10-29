<script setup lang="ts">
// 「生成接口文档」结果弹窗：先展示生成的 Markdown（预览/编辑两态，可编辑、可复制），
// 完成后再选择落点——完全覆盖 / 放到已有文档下方 / 下载。真正的写盘由父级（RequestEditor）执行，
// 本组件只回传「当前文本 + 所选动作」，避免编辑态与请求模型双写。
import { uploadMdImages } from '@/lib/md-image'
import { message } from '@/lib/notice'
import { isDark } from '@/lib/theme'
import { MdEditor, MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { NButton, NModal } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean; text: string }>()
const emit = defineEmits<{
    'update:show': [v: boolean]
    apply: [payload: { mode: 'overwrite' | 'append' | 'download'; text: string }]
}>()
const { t, locale } = useI18n()

/** 可编辑副本：弹窗打开时以生成文本初始化，编辑/复制/落点都基于它。 */
const draft = ref(props.text)
/** preview = 只读渲染；edit = md-editor 源码编辑（可改可复制）。 */
const mode = ref<'preview' | 'edit'>('preview')

// 每次打开重置为最新生成结果，并回到预览态（不残留上次的编辑与模式）
watch(
    () => [props.show, props.text] as const,
    () => {
        if (!props.show) return
        draft.value = props.text
        mode.value = 'preview'
    },
)

const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))
const mdTheme = computed(() => (isDark.value ? 'dark' : 'light'))
const mdPreviewTheme = computed(() => (isDark.value ? 'github' : 'default'))

async function copyText(): Promise<void> {
    try {
        await navigator.clipboard.writeText(draft.value)
        message.success(t('docs.copied'))
    } catch {
        message.error(t('docs.copyFailed'))
    }
}

/** 选择一个落点：把当前（可能已编辑的）文本连同动作回传父级，并关闭弹窗。 */
function pick(modeKind: 'overwrite' | 'append' | 'download'): void {
    emit('apply', { mode: modeKind, text: draft.value })
    emit('update:show', false)
}
</script>

<template>
    <n-modal :show="show" preset="card" :title="t('docs.gen')" style="width: 820px; max-width: 94vw"
        @update:show="emit('update:show', $event)">
        <div class="gen-body" data-testid="docs.genDialog">
            <div class="gen-mode">
                <button class="sub-tab" :class="{ on: mode === 'preview' }" type="button" data-testid="docs.mode"
                    data-mode="preview" @click="mode = 'preview'">{{ t('docs.view') }}</button>
                <button class="sub-tab" :class="{ on: mode === 'edit' }" type="button" data-testid="docs.mode"
                    data-mode="edit" @click="mode = 'edit'">{{ t('docs.edit') }}</button>
                <span class="sp" />
                <n-button size="tiny" tertiary data-testid="docs.copy" @click="copyText">{{ t('docs.copy') }}</n-button>
            </div>
            <div v-if="mode === 'edit'" class="gen-pane" data-testid="docs.genEditor">
                <md-editor v-model="draft" :language="mdLanguage" :theme="mdTheme" :preview-theme="mdPreviewTheme"
                    :on-upload-img="uploadMdImages" class="md-edit" />
            </div>
            <div v-else class="gen-pane" data-testid="docs.genPreview">
                <md-preview :model-value="draft" :language="mdLanguage" :theme="mdTheme" :preview-theme="mdPreviewTheme"
                    class="md-preview" />
            </div>
        </div>
        <template #footer>
            <div class="gen-ft">
                <p class="gen-hint">{{ t('docs.genHint') }}</p>
                <span class="sp" />
                <n-button size="small" data-testid="docs.dlgDownload" @click="pick('download')">{{ t('docs.download')
                }}</n-button>
                <n-button size="small" data-testid="docs.dlgAppend" @click="pick('append')">{{ t('docs.append')
                }}</n-button>
                <n-button size="small" type="primary" data-testid="docs.dlgOverwrite" @click="pick('overwrite')">{{
                    t('docs.overwrite') }}</n-button>
            </div>
        </template>
    </n-modal>
</template>

<style scoped>
.gen-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 0;
}

/* 预览/编辑的容器：保证外层 testid 稳定落在 DOM（不依赖组件 attrs 透传） */
.gen-pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
}

.gen-mode {
    display: flex;
    align-items: center;
    gap: 4px;
}

.sub-tab {
    border: none;
    background: none;
    border-radius: 5px;
    padding: 4px 10px;
    font-family: inherit;
    font-size: 12px;
    color: var(--app-muted);
    cursor: pointer;
}

.sub-tab:hover {
    background: var(--app-row-hover);
    color: var(--app-text);
}

.sub-tab.on {
    background: var(--app-accent-tint);
    color: var(--app-accent-dark);
    font-weight: 600;
}

.sp {
    flex: 1 1 auto;
}

.md-edit {
    min-height: 360px;
}

.md-preview {
    max-height: 60vh;
    overflow: auto;
    border: 1px solid var(--app-border);
    border-radius: 6px;
    padding: 12px 16px;
}

.gen-ft {
    display: flex;
    align-items: center;
    gap: 8px;
}

.gen-hint {
    margin: 0;
    font-size: 11.5px;
    color: var(--app-muted);
}
</style>
