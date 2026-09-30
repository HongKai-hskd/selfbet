import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/login', component: () => import('./views/Login.vue') },
  {
    path: '/',
    component: () => import('./views/Layout.vue'),
    children: [
      { path: '', redirect: '/tasks' },
      { path: 'tasks', component: () => import('./views/TasksView.vue') },
      { path: 'stats', component: () => import('./views/StatsView.vue') },
      { path: 'shop', component: () => import('./views/ShopView.vue') },
      { path: 'mine', component: () => import('./views/MineView.vue') }
    ]
  },
  // 子页面（无底部 Tab，自带返回导航）
  { path: '/backpack', component: () => import('./views/BackpackView.vue') },
  { path: '/cash', component: () => import('./views/CashView.vue') },
  { path: '/schedule', component: () => import('./views/ScheduleView.vue') },
  { path: '/farm', component: () => import('./views/FarmView.vue') },
  { path: '/items-guide', component: () => import('./views/ItemsGuideView.vue') }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to) => {
  const token = localStorage.getItem('selfbet_token')
  if (to.path !== '/login' && !token) return '/login'
  if (to.path === '/login' && token) return '/tasks'
})

export default router
