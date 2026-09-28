<template>
  <div class="page">
    <van-nav-bar title="现金明细" left-arrow @click-left="$router.back()" />

    <div class="sum-card">
      <div class="sum-row">
        <span class="sum-label">获取</span>
        <span class="sum-val in">+¥{{ (earned / 100).toFixed(2) }}</span>
      </div>
      <div class="sum-row">
        <span class="sum-label big">消耗</span>
        <span class="sum-val out big">-¥{{ (spent / 100).toFixed(2) }}</span>
      </div>
    </div>

    <div class="chips">
      <span
        v-for="m in modes" :key="m.value"
        class="chip" :class="{ active: mode === m.value }"
        @click="setMode(m.value)"
      >{{ m.text }}</span>
    </div>
    <div class="chips type-chips">
      <span
        v-for="t in types" :key="t.value"
        class="chip" :class="{ active: typeFilter === t.value }"
        @click="typeFilter = t.value; loadDetail()"
      >{{ t.text }}</span>
    </div>

    <div class="custom" v-if="mode === 'custom'">
      <van-field readonly label="开始" is-link :model-value="customStart || '选择'" @click="pickStart = true" />
      <van-field readonly label="结束" is-link :model-value="customEnd || '选择'" @click="pickEnd = true" />
    </div>

    <div class="list" v-if="flows.length">
      <div v-for="f in flows" :key="f.id" class="row">
        <div class="main">
          <div class="note">{{ f.note }}</div>
          <div class="time">{{ fmtTime(f.created_at) }}</div>
        </div>
        <div class="amt" :class="f.amount_cents > 0 ? 'in' : 'out'">
          {{ f.amount_cents > 0 ? '+' : '-' }}¥{{ (Math.abs(f.amount_cents) / 100).toFixed(2) }}
        </div>
      </div>
    </div>
    <van-empty v-else description="这个范围没有记录" image-size="64" />

    <van-popup v-model:show="pickStart" round position="bottom">
      <van-date-picker v-model="startCols" title="开始日期" @confirm="onPickStart" @cancel="pickStart = false" />
    </van-popup>
    <van-popup v-model:show="pickEnd" round position="bottom">
      <van-date-picker v-model="endCols" title="结束日期" @confirm="onPickEnd" @cancel="pickEnd = false" />
    </van-popup>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'

const flows = ref([])
const earned = ref(0)
const spent = ref(0)
const mode = ref('today')
const typeFilter = ref('')
const customStart = ref('')
const customEnd = ref('')
const pickStart = ref(false)
const pickEnd = ref(false)
const startCols = ref([])
const endCols = ref([])

const modes = [
  { text: '今天', value: 'today' },
  { text: '昨天', value: 'yesterday' },
  { text: '近7天', value: '7d' },
  { text: '近30天', value: '30d' },
  { text: '自定义', value: 'custom' }
]
const types = [
  { text: '全部', value: '' },
  { text: '兑换', value: 'in' },
  { text: '消费', value: 'out' }
]

function dateStr(d) {
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

function setMode(v) {
  if (mode.value === v && v !== 'custom') return
  mode.value = v
  if (v === 'custom') {
    if (!customStart.value) {
      customStart.value = dateStr(new Date(Date.now() - 6 * 86400000))
      customEnd.value = dateStr(new Date())
    }
    loadDetail()
    return
  }
  loadDetail()
}

async function loadDetail() {
  const now = new Date()
  let start, end = now
  if (mode.value === 'today') {
    start = now
  } else if (mode.value === 'yesterday') {
    const y = new Date(now.getTime() - 86400000)
    start = y; end = y
  } else if (mode.value === '7d') {
    start = new Date(now.getTime() - 6 * 86400000)
  } else if (mode.value === '30d') {
    start = new Date(now.getTime() - 29 * 86400000)
  } else {
    if (!customStart.value || !customEnd.value) return
    start = new Date(customStart.value)
    end = new Date(customEnd.value)
  }
  const params = { start_date: dateStr(start), end_date: dateStr(end) }
  if (typeFilter.value) params.type = typeFilter.value
  const res = await api.get('/cash', { params })
  flows.value = res.items || []
  earned.value = res.earned_cents || 0
  spent.value = res.spent_cents || 0
}

function onPickStart({ selectedValues }) {
  const [y, m, d] = selectedValues
  customStart.value = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
  pickStart.value = false
  if (customEnd.value) loadDetail()
}
function onPickEnd({ selectedValues }) {
  const [y, m, d] = selectedValues
  customEnd.value = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
  pickEnd.value = false
  if (customStart.value) loadDetail()
}

function fmtTime(s) {
  const d = new Date(s)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onMounted(loadDetail)
</script>

<style scoped>
.page { padding-bottom: 30px; }
.sum-card {
  margin: 14px 16px 0;
  padding: 14px 18px;
  background: #fff;
  border-radius: 12px;
}
.sum-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 5px 0;
}
.sum-label { font-size: 14px; color: #646566; }
.sum-label.big { font-size: 15px; font-weight: 600; color: #323233; }
.sum-val { font-size: 15px; font-weight: 600; }
.sum-val.big { font-size: 20px; font-weight: 700; }
.in { color: #07c160; }
.out { color: #ee0a24; }
.chips { display: flex; gap: 8px; padding: 12px 16px 2px; }
.type-chips { padding-top: 8px; }
.chip {
  flex-shrink: 0;
  padding: 4px 12px;
  border-radius: 14px;
  background: #fff;
  color: #646566;
  font-size: 12px;
  border: 1px solid #ebedf0;
}
.chip.active { background: #1989fa; color: #fff; border-color: #1989fa; }
.custom { margin: 8px 16px 0; background: #fff; border-radius: 8px; overflow: hidden; }
.list { margin: 10px 16px 0; background: #fff; border-radius: 12px; padding: 0 14px; }
.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 9px 0;
  border-bottom: 1px solid #f7f8fa;
}
.row:last-child { border-bottom: none; }
.note { font-size: 13px; color: #323233; }
.time { font-size: 12px; color: #c8c9cc; margin-top: 2px; }
.amt { font-weight: 700; font-size: 14px; }
</style>
