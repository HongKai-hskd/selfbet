import axios from 'axios'
import router from './router'

const api = axios.create({ baseURL: '/api', timeout: 10000 })

api.interceptors.request.use((cfg) => {
  const t = localStorage.getItem('selfbet_token')
  if (t) cfg.headers.Authorization = 'Bearer ' + t
  return cfg
})

api.interceptors.response.use(
  (res) => res.data,
  (err) => {
    // 密码输错(在登录页)时不跳转，只清 token
    if (err.response && err.response.status === 401 && router.currentRoute.value.path !== '/login') {
      localStorage.removeItem('selfbet_token')
      router.push('/login')
    }
    return Promise.reject((err.response && err.response.data && err.response.data.error) || '请求失败')
  }
)

export default api
