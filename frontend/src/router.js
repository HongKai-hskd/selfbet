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
  }
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
