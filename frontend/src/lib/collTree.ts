// 集合树的纯函数工具：排序 / 搜索过滤 / 按路径或 uid 定位。与 Vue 响应式无关，便于单测。
import type { TreeNode } from '@/types'

/** 排序模式：default 保持集合内既有顺序；asc/desc 按名称（中文 locale）。 */
export type SortMode = 'default' | 'asc' | 'desc'

/** 按名称递归排序：folder 与叶子各自成组、folder 在前；mode=default 原样返回。返回深拷贝副本（不改原树）。 */
export function sortTree(nodes: TreeNode[], mode: SortMode): TreeNode[] {
    if (mode === 'default') return nodes
    const dir = mode === 'asc' ? 1 : -1
    const byName = (a: TreeNode, b: TreeNode): number => a.name.localeCompare(b.name, 'zh-Hans-CN') * dir
    const folders: TreeNode[] = []
    const leaves: TreeNode[] = []
    for (const n of nodes) {
        const copy = n.children?.length ? { ...n, children: sortTree(n.children, mode) } : n
            ; (copy.type === 'folder' ? folders : leaves).push(copy)
    }
    folders.sort(byName)
    leaves.sort(byName)
    return [...folders, ...leaves]
}

/** 搜索：命中请求保留其祖先分组；命中分组名则保留分组。q 需已小写、已 trim。 */
export function filterTree(nodes: TreeNode[], q: string): TreeNode[] {
    const out: TreeNode[] = []
    for (const n of nodes) {
        if (n.type === 'folder') {
            const kids = n.children ? filterTree(n.children, q) : []
            if (kids.length || n.name.toLowerCase().includes(q)) {
                out.push({ ...n, children: kids.length ? kids : n.children })
            }
        } else if (n.name.toLowerCase().includes(q)) {
            out.push(n)
        }
    }
    return out
}

/** 在树里按相对路径找分组节点（跨根提示、落点校验用）。 */
export function findFolderByPath(nodes: TreeNode[], path: string): TreeNode | null {
    for (const n of nodes) {
        if (n.type !== 'folder') continue
        if (n.path === path) return n
        if (n.children) {
            const hit = findFolderByPath(n.children, path)
            if (hit) return hit
        }
    }
    return null
}

/** 在树里按 uid 找节点（folder / request）。 */
export function searchNode(nodes: TreeNode[], uid: string): TreeNode | null {
    for (const n of nodes) {
        if (n.uid === uid && (n.type === 'folder' || n.type === 'request')) return n
        if (n.children) {
            const hit = searchNode(n.children, uid)
            if (hit) return hit
        }
    }
    return null
}

/** 某节点的祖先分组路径（用于把当前选中的请求自动展开到可见）；只在该根的树里找。 */
export function ancestorPaths(nodes: TreeNode[], uid: string, trail: string[] = []): string[] | null {
    for (const n of nodes) {
        if (n.uid === uid) return trail
        if (n.children) {
            const next = n.type === 'folder' ? [...trail, n.path] : trail
            const hit = ancestorPaths(n.children, uid, next)
            if (hit) return hit
        }
    }
    return null
}

/**
 * 能否把 node 放进 destRoot 里的 destPath（空串 = 该工作目录的根）。
 * 规则：目标必须是分组或根；同根内分组不能移入自己或自己的后代（会成环）；
 * 跨工作目录两种都能搬：请求单搬、分组连同其下子分组与请求整棵搬（目标侧重建 + 源进 .trash）。
 */
export function canDrop(node: TreeNode, dragRoot: string, destRoot: string, destPath: string): boolean {
    if (dragRoot !== destRoot) return true
    if (node.type === 'folder' && destPath === node.path) return false
    if (node.type === 'folder' && destPath.startsWith(node.path + '/')) return false
    return true
}

/** 目标分组（destPath 空 = 该根的根）下是否已有同名分组（tree 为该根的完整树）。 */
export function folderNameTaken(tree: TreeNode[], destPath: string, name: string): boolean {
    const nodes = destPath ? (findFolderByPath(tree, destPath)?.children ?? []) : tree
    return nodes.some((n) => n.type === 'folder' && n.name === name)
}
