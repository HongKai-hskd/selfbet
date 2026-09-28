<template>
  <div class="login-wrap">
    <van-form @submit="onLogin">
      <van-cell-group inset>
        <van-field
          v-model="password"
          type="password"
          placeholder="输入密码"
          :rules="[{ required: true, message: '请输入密码' }]"
        />
      </van-cell-group>
      <div class="btn-wrap">
        <van-button round block type="primary" native-type="submit" :loading="loading">
          进入
        </van-button>
      </div>
    </van-form>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'
import api from '../api'

const router = useRouter()
const password = ref('')
const loading = ref(false)

async function onLogin() {
  loading.value = true
  try {
    const res = await api.post('/auth/login', { password: password.value })
    localStorage.setItem('selfbet_token', res.token)
    router.push('/tasks')
  } catch (e) {
    showToast(e)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 0 8px;
  width: 100%;
}
.btn-wrap {
  margin: 20px 16px 0;
}
</style>
