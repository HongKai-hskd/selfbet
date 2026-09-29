<template>
  <div class="page">
    <div class="balance-card">
      <div class="balance-label">当前积分</div>
      <div class="balance-num">{{ balance }}</div>
      <div class="balance-sub">
        <span>累计获得 {{ totalEarned }}</span>
        <span>累计消费 {{ totalSpent }}</span>
      </div>
    </div>

    <!-- 四格统计（净额：获得-罚分，负数红色） -->
    <div class="stat-grid">
      <div class="stat-cell">
        <b :class="{ neg: netOf(stats.today) < 0 }">{{ signedNet(stats.today) }}</b>
        <span>今天</span>
      </div>
      <div class="stat-cell">
        <b :class="{ neg: netOf(stats.yesterday) < 0 }">{{ signedNet(stats.yesterday) }}</b>
        <span>昨天</span>
      </div>
      <div class="stat-cell">
        <b :class="{ neg: netOf(stats.this_week) < 0 }">{{ signedNet(stats.this_week) }}</b>
        <span>本周</span>
      </div>
      <div class="stat-cell">
        <b :class="{ neg: netOf(stats.this_month) < 0 }">{{ signedNet(stats.this_month) }}</b>
        <span>本月</span>
      </div>
    </div>

    <!-- 热力图 -->
    <div class="card">
      <div class="card-head">
        <span class="card-title">坚持热力图</span>
        <div class="mini-chips">
          <span class="mchip" :class="{ on: heatWeeks === 26 }" @click="setHeatWeeks(26)">半年</span>
          <span class="mchip" :class="{ on: heatWeeks === 53 }" @click="setHeatWeeks(53)">一年</span>
        </div>
      </div>
      <div class="hm-scroll">
        <div class="hm-inner">
          <div class="hm-months">
            <span
              v-for="(m, i) in monthLabels" :key="i"
              class="hm-month" :style="{ left: i * 16 + 'px' }"
            >{{ m }}</span>
          </div>
          <div class="heatmap">
            <div v-for="(week, wi) in heatColumns" :key="wi" class="hm-col">
              <div
                v-for="(cell, ci) in week" :key="ci"
                class="hm-cell" :class="cellClass(cell)"
                @mouseenter="cell && showTip(cell, $event)"
                @mouseleave="hideTip"
                @click="cell && tapCell(cell)"
              />
            </div>
          </div>
        </div>
      </div>
      <div class="hm-legend">
        <span>少</span>
        <span class="hm-cell lv0" />
        <span class="hm-cell lv1" />
        <span class="hm-cell lv2" />
        <span class="hm-cell lv3" />
        <span class="hm-cell lv4" />
        <span>多</span>
        <span class="hm-cell lv-n3" style="margin-left: 6px" />
        <span>负</span>
      </div>
    </div>

    <!-- 明细（可筛选） -->
    <div class="card">
      <div class="card-head">
        <span class="card-title">明细</span>
        <div class="mini-chips">
          <span class="mchip" :class="{ on: detailMode === 'today' }" @click="setDetail('today')">今天</span>
          <span class="mchip" :class="{ on: detailMode === 'yesterday' }" @click="setDetail('yesterday')">昨天</span>
          <span class="mchip" :class="{ on: detailMode === '7d' }" @click="setDetail('7d')">近7天</span>
          <span class="mchip" :class="{ on: detailMode === '30d' }" @click="setDetail('30d')">近30天</span>
          <span class="mchip" :class="{ on: detailMode === 'custom' }" @click="detailMode = 'custom'">自定义</span>
        </div>
      </div>

      <div class="range-row" v-if="detailMode === 'custom'">
        <div class="range-field" @click="pickStart = true">
          <span class="range-label">开始</span>
          <span>{{ customStart ? fmtDate(customStart) : '选择' }}</span>
        </div>
        <span class="range-sep">~</span>
        <div class="range-field" @click="pickEnd = true">
          <span class="range-label">结束</span>
          <span>{{ customEnd ? fmtDate(customEnd) : '选择' }}</span>
        </div>
      </div>

      <div class="range-summary" v-if="detailLoaded">
        <span>{{ rangeText }}</span>
        <span class="rs-earned" :class="{ neg: detailSum < 0 }">共 {{ signedNum(detailSum) }} 分</span>
      </div>

      <div class="detail-list">
        <div v-for="item in detailItems" :key="item.id" class="detail-item">
          <div class="ledger-main">
            <div class="detail-note">{{ item.note }}</div>
            <div class="ledger-time">{{ fmtTime(item.created_at) }}</div>
          </div>
          <div class="ledger-amount" :class="item.amount > 0 ? 'plus' : 'minus'">
            {{ item.amount > 0 ? '+' : '' }}{{ item.amount }}
          </div>
          <van-icon
            v-if="canUndo(item)" name="replay" class="undo-icon"
            @click="undoItem(item)"
          />
        </div>
        <van-empty v-if="detailLoaded && !detailItems.length" description="这个范围没有记录" image-size="64" />
      </div>
    </div>

    <!-- 日期选择 -->
    <van-popup v-model:show="pickStart" round position="bottom">
      <van-date-picker
        v-model="startPicker" :min-date="minDate" :max-date="maxDate"
        title="开始日期" @confirm="onPickStart" @cancel="pickStart = false"
      />
    </van-popup>
    <van-popup v-model:show="pickEnd" round position="bottom">
      <van-date-picker
        v-model="endPicker" :min-date="minDate" :max-date="maxDate"
        title="结束日期" @confirm="onPickEnd" @cancel="pickEnd = false"
      />
    </van-popup>

    <!-- 悬停气泡 -->
    <div v-if="tip.show" class="hm-tip" :style="{ left: tip.x + 'px', top: tip.y + 'px' }">
      {{ tip.text }}
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import api from '../api'

const balance = ref(0)
const totalEarned = ref(0)
const totalSpent = ref(0)
const stats = ref({
  today: { earned: 0, spent: 0 },
  yesterday: { earned: 0, spent: 0 },
  this_week: { earned: 0, spent: 0 },
  this_month: { earned: 0, spent: 0 },
  yesterday_items: [],
  heatmap: []
})
const heatWeeks = ref(26)

const detailMode = ref('today')
const customStart = ref('')
const customEnd = ref('')
const detailItems = ref([])
const detailSum = ref(0)
const detailLoaded = ref(false)
const pickStart = ref(false)
const pickEnd = ref(false)
const startPicker = ref([])
const endPicker = ref([])
const minDate = new Date(2026, 0, 1)
const maxDate = new Date()

const tip = ref({ show: false, x: 0, y: 0, text: '' })

function fmtDate(s) {
  if (!s) return ''
  const d = new Date(s)
  return `${d.getMonth() + 1}/${d.getDate()}`
}

function fmtTime(s) {
  const d = new Date(s)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function dateStr(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// ---- stats & heatmap ----

async function loadStats() {
  const s = await api.get('/stats', { params: { weeks: heatWeeks.value } })
  stats.value = s
}

function setHeatWeeks(w) {
  if (heatWeeks.value === w) return
  heatWeeks.value = w
  loadStats()
}

const heatColumns = computed(() => {
  const list = stats.value.heatmap || []
  if (!list.length) return []
  const cols = []
  let col = []
  for (const d of list) {
    if (d.weekday === 0 && col.length) {
      cols.push(col)
      col = []
    }
    col.push(d)
  }
  if (col.length) cols.push(col)
  return cols
})

// 月份轴：每列首格的月份变化处标 label
const monthLabels = computed(() => {
  const cols = heatColumns.value
  const labels = []
  let last = -1
  cols.forEach((col, i) => {
    if (!col.length) return
    const m = new Date(col[0].date).getMonth() + 1
    if (m !== last) {
      labels.push(m + '月')
      last = m
    } else {
      labels.push('')
    }
  })
  return labels
})

// ---- 热力图色阶：分位数自适应（GitHub 同款） ----
// 基准来自当前视图内的数据分布，不拍脑袋定阈值：
// 正数日按正数分位数分 4 档绿，负数日按亏损绝对值分 3 档红，
// 最高日必为深绿、最大亏损日必为深红；切换视图范围时重算。

function quantile(sorted, p) {
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))]
}

const posSorted = computed(() =>
  stats.value.heatmap.filter((c) => c.earned > 0).map((c) => c.earned).sort((a, b) => a - b)
)
const negSorted = computed(() =>
  stats.value.heatmap.filter((c) => c.earned < 0).map((c) => Math.abs(c.earned)).sort((a, b) => a - b)
)
const posQ = computed(() =>
  posSorted.value.length ? [0.25, 0.5, 0.75].map((p) => quantile(posSorted.value, p)) : null
)
const negQ = computed(() =>
  negSorted.value.length ? [0.25, 0.5, 0.75].map((p) => quantile(negSorted.value, p)) : null
)

function cellClass(cell) {
  if (!cell) return 'lv-empty'
  const e = cell.earned
  if (e > 0) {
    const q = posQ.value
    if (!q) return 'lv1'
    if (e < q[0]) return 'lv1'
    if (e < q[1]) return 'lv2'
    if (e < q[2]) return 'lv3'
    return 'lv4'
  }
  if (e < 0) {
    const a = Math.abs(e)
    const q = negQ.value
    if (!q) return 'lv-n1'
    if (a < q[0]) return 'lv-n1'
    if (a < q[1]) return 'lv-n2'
    return 'lv-n3'
  }
  return 'lv0'
}

function showTip(cell, e) {
  const d = new Date(cell.date)
  const v = cell.earned
  const text = v !== 0
    ? `${d.getMonth() + 1}月${d.getDate()}日 净 ${v > 0 ? '+' : ''}${v} 分`
    : `${d.getMonth() + 1}月${d.getDate()}日 没有积分变动`
  tip.value = { show: true, x: e.clientX, y: e.clientY - 40, text }
}

function hideTip() {
  tip.value.show = false
}

function tapCell(cell) {
  const d = new Date(cell.date)
  const v = cell.earned
  if (v !== 0) showToast(`${d.getMonth() + 1}月${d.getDate()}日 净 ${v > 0 ? '+' : ''}${v} 分`)
  else showToast(`${d.getMonth() + 1}月${d.getDate()}日 没有积分变动`)
}

// ---- 明细（按范围） ----

function setDetail(mode) {
  if (detailMode.value === mode && mode !== 'custom') return
  detailMode.value = mode
  if (mode === 'custom') {
    if (!customStart.value) {
      const y = new Date(Date.now() - 86400000)
      customStart.value = dateStr(y)
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
  if (detailMode.value === 'today') {
    start = now
  } else if (detailMode.value === 'yesterday') {
    const y = new Date(now.getTime() - 86400000)
    start = y
    end = y
  } else if (detailMode.value === '7d') {
    start = new Date(now.getTime() - 6 * 86400000)
  } else if (detailMode.value === '30d') {
    start = new Date(now.getTime() - 29 * 86400000)
  } else {
    if (!customStart.value || !customEnd.value) return
    start = new Date(customStart.value)
    end = new Date(customEnd.value)
    if (start > end) return showToast('开始日期不能晚于结束日期')
  }
  const params = { start_date: dateStr(start), end_date: dateStr(end), limit: 500 }
  const res = await api.get('/ledger', { params })
  detailItems.value = res.items || []
  detailSum.value = res.range_sum || 0
  detailLoaded.value = true
}

// 四格卡/汇总净额：获得 - 罚分等负数行
function netOf(p) {
  return (p.earned || 0) - (p.spent || 0)
}
function signedNet(p) {
  const v = netOf(p)
  return v >= 0 ? `+${v}` : `${v}`
}
function signedNum(v) {
  return v >= 0 ? `+${v}` : `${v}`
}

function onPickStart({ selectedValues }) {
  const [y, m, d] = selectedValues
  customStart.value = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
  pickStart.value = false
  if (customEnd.value) loadDetail()
}

// ---- 撤回（仅今天 + 任务完成/宝箱开出类流水；罚分/商城不开放） ----

function canUndo(item) {
  return (item.type === 'task' || item.type === 'box') && dateStr(new Date(item.created_at)) === dateStr(new Date())
}

async function undoItem(item) {
  const msg = item.type === 'task'
    ? `「${item.note}」\n积分将回退 -${item.amount} 分，任务恢复为进行中`
    : `「${item.note}」\n积分将回退 -${item.amount} 分`
  try {
    await showConfirmDialog({ title: '撤回记录', message: msg })
  } catch {
    return
  }
  try {
    await api.post(`/ledger/${item.id}/undo`)
    showToast('已撤回')
    loadDetail()
    loadStats()
    api.get('/ledger', { params: { limit: 1 } }).then((r) => {
      balance.value = r.balance
    })
  } catch (e) {
    showToast(e)
  }
}

function onPickEnd({ selectedValues }) {
  const [y, m, d] = selectedValues
  customEnd.value = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
  pickEnd.value = false
  if (customStart.value) loadDetail()
}

const rangeText = computed(() => {
  const now = new Date()
  if (detailMode.value === 'today') return '今天'
  if (detailMode.value === 'yesterday') return '昨天'
  if (detailMode.value === '7d') return `${fmtDate(dateStr(new Date(now.getTime() - 6 * 86400000)))} ~ 今天`
  if (detailMode.value === '30d') return `${fmtDate(dateStr(new Date(now.getTime() - 29 * 86400000)))} ~ 今天`
  return `${fmtDate(customStart.value)} ~ ${fmtDate(customEnd.value)}`
})

onMounted(() => {
  loadStats()
  loadDetail()
  api.get('/ledger', { params: { limit: 1 } }).then((r) => {
    balance.value = r.balance
  })
  api.get('/me').then((r) => {
    totalEarned.value = r.total_earned
    totalSpent.value = r.total_spent
  })
})
</script>

<style scoped>
.balance-card {
  background: linear-gradient(135deg, #4d9dff, #1989fa);
  border-radius: 16px;
  color: #fff;
  padding: 22px 20px;
  text-align: center;
}
.balance-label { font-size: 13px; opacity: 0.85; }
.balance-num { font-size: 48px; font-weight: 700; line-height: 1.2; }
.balance-sub { display: flex; justify-content: center; gap: 20px; font-size: 12px; opacity: 0.9; margin-top: 4px; }
.stat-grid {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}
.stat-cell {
  flex: 1;
  background: #fff;
  border-radius: 12px;
  text-align: center;
  padding: 12px 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.stat-cell b { font-size: 17px; color: #1989fa; }
.stat-cell span { font-size: 11px; color: #969799; }
.card { background: #fff; border-radius: 12px; padding: 14px; margin-top: 12px; }
.card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.card-title { font-size: 14px; font-weight: 600; color: #323233; }
.mini-chips { display: flex; gap: 6px; }
.mchip {
  padding: 3px 10px;
  border-radius: 12px;
  font-size: 11px;
  background: #f7f8fa;
  color: #646566;
  border: 1px solid #ebedf0;
  white-space: nowrap;
}
.mchip.on { background: #1989fa; color: #fff; border-color: #1989fa; }
.hm-scroll { overflow-x: auto; -webkit-overflow-scrolling: touch; }
.hm-inner { width: max-content; margin: 0 auto; position: relative; }
.hm-months { position: relative; height: 16px; margin-bottom: 2px; }
.hm-month {
  position: absolute;
  top: 0;
  font-size: 10px;
  color: #969799;
  white-space: nowrap;
}
.heatmap { display: flex; gap: 3px; }
.hm-col { display: flex; flex-direction: column; gap: 3px; }
.hm-cell {
  width: 13px;
  height: 13px;
  border-radius: 3px;
  background: #ebedf0;
}
.hm-cell.lv0 { background: #f0f1f3; }
.hm-cell.lv1 { background: #9be9a8; }
.hm-cell.lv2 { background: #40c463; }
.hm-cell.lv3 { background: #30a14e; }
.hm-cell.lv4 { background: #216e39; }
.hm-cell.lv-n1 { background: #ffc9c9; }
.hm-cell.lv-n2 { background: #ff8080; }
.hm-cell.lv-n3 { background: #e02020; }
.hm-cell.lv-empty { background: transparent; }
.hm-legend {
  display: flex;
  align-items: center;
  gap: 4px;
  justify-content: flex-end;
  margin-top: 10px;
  font-size: 11px;
  color: #c8c9cc;
}
.hm-legend .hm-cell { width: 11px; height: 11px; }
.range-row { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; }
.range-field {
  flex: 1;
  background: #f7f8fa;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
  color: #323233;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.range-label { color: #969799; font-size: 12px; margin-right: 6px; }
.range-sep { color: #c8c9cc; }
.range-summary {
  display: flex;
  justify-content: space-between;
  background: #f0f9ff;
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 12px;
  color: #646566;
  margin-bottom: 8px;
}
.rs-earned { color: #1989fa; font-weight: 600; }
.rs-earned.neg { color: #ee0a24; }
.stat-cell b.neg { color: #ee0a24; }
.detail-list { max-height: 420px; overflow-y: auto; }
.detail-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 9px 2px;
  border-bottom: 1px solid #f7f8fa;
}
.detail-item:last-child { border-bottom: none; }
.ledger-main { flex: 1; min-width: 0; }
.detail-note { font-size: 13px; color: #323233; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ledger-time { font-size: 12px; color: #c8c9cc; margin-top: 2px; }
.ledger-amount { font-weight: 700; font-size: 15px; flex-shrink: 0; }
.undo-icon { color: #c8c9cc; font-size: 16px; padding: 6px 2px 6px 10px; flex-shrink: 0; }
.undo-icon:active { color: #1989fa; }
.plus { color: #07c160; }
.minus { color: #ee0a24; }
.hm-tip {
  position: fixed;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.82);
  color: #fff;
  font-size: 12px;
  padding: 7px 12px;
  border-radius: 8px;
  z-index: 4000;
  pointer-events: none;
  white-space: nowrap;
}
</style>
