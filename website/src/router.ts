import { createRouter, createWebHistory } from 'vue-router'

// 路由级代码分割：首页不再下载文档解析器与插件市场代码。
// 官网是静态托管的营销站，首屏体积直接影响转化，三个页面都按需加载。
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: () => import('@/views/HomeView.vue') },
    { path: '/docs/:file?', name: 'docs', component: () => import('@/views/DocsView.vue') },
    { path: '/plugins', name: 'plugins', component: () => import('@/views/PluginsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior(to, _from, saved) {
    if (to.hash) return { el: to.hash, behavior: 'smooth', top: 72 }
    if (saved) return saved
    return { top: 0 }
  },
})
