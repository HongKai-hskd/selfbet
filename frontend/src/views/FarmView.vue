<template>
  <div class="page">
    <van-nav-bar title="农场" left-arrow @click-left="$router.back()" />

    <!-- 农场积分卡 -->
    <div class="farm-card">
      <div class="fc-top">
        <div>
          <div class="fc-coins">{{ fmtNum(farm.coins) }}</div>
          <div class="fc-label">农场积分（100 = 1 积分）</div>
        </div>
        <van-button size="small" round type="primary" :disabled="!farm.withdrawable" :loading="busy" @click="doWithdraw">
          {{ farm.withdrawable ? `提现 ${farm.withdrawable} 分` : '提现' }}
        </van-button>
      </div>
      <div class="fc-stats">
        <div class="fc-stat"><b>{{ fmtNum(farm.yield_per_round) }}</b><span>单格收获</span></div>
        <div class="fc-stat"><b>{{ fmtHours(farm.period_hours) }}</b><span>生长周期</span></div>
        <div class="fc-stat"><b>{{ fmtNum(roundIncome) }}</b><span>每轮收获</span></div>
      </div>
    </div>

    <!-- 快捷操作 -->
    <div class="quick-row">
      <van-button size="small" round plain type="primary" :disabled="!emptyCount" @click="plantAll">
        一键全种（{{ emptyCount }}）
      </van-button>
      <van-button size="small" round type="primary" :disabled="!harvestableCount" @click="harvestAll">
        一键全收{{ harvestableCount ? ` +${fmtNum(farm.harvest_now)}` : '' }}
      </van-button>
    </div>

    <!-- 4×6 田格 -->
    <div class="plots-grid">
      <div
        v-for="p in farm.plots"
        :key="p.plot_index"
        class="plot"
        :class="plotClass(p)"
        @click="onPlot(p)"
      >
        <!-- 未解锁 -->
        <template v-if="!p.unlocked">
          <div class="pl-lock">🔒</div>
          <div class="pl-sub" :class="{ buyable: p.plot_index === farm.next_plot_index }">
            {{ p.plot_index === farm.next_plot_index ? `开垦 ${farm.next_plot_price}分` : '未开垦' }}
          </div>
        </template>
        <!-- 空田 -->
        <template v-else-if="!p.planted_at">
          <div class="pl-plus">＋</div>
          <div class="pl-sub">空田</div>
        </template>
        <!-- 生长中 -->
        <template v-else-if="!matureAt(p)">
          <div class="pl-crop">🌱</div>
          <van-progress :percentage="progressOf(p)" :show-pivot="false" stroke-width="4" class="pl-bar" />
          <div class="pl-sub">{{ remainText(p) }}</div>
        </template>
        <!-- 成熟可收 -->
        <template v-else-if="p.daily_left > 0">
          <div class="pl-crop mature">🌾</div>
          <div class="pl-sub gold">可收 +{{ fmtNum(farm.yield_per_round) }}</div>
        </template>
        <!-- 成熟但今日已满 -->
        <template v-else>
          <div class="pl-crop dim">🌾</div>
          <div class="pl-sub">今日已满</div>
        </template>
      </div>
    </div>
    <div class="tip">每天每块田最多收 2 轮（每轮收获 = 田数 × 单格收获 × 2）；成熟忘收不枯死</div>

    <!-- 升级区 -->
    <div class="section-title">农场升级</div>
    <div class="up-card" v-for="line in upgradeLines" :key="line.key">
      <div class="up-main">
        <div class="up-name">{{ line.name }} <span class="up-lv">Lv{{ line.level }}<template v-if="!line.maxed"> → {{ line.level + 1 }}</template></span></div>
        <div class="up-desc">{{ line.desc }}</div>
      </div>
      <van-button size="small" round type="primary" :color="line.color" :disabled="line.maxed" :loading="busy" @click="doUpgrade(line)">
        {{ line.maxed ? '已满级' : line.priceLabel }}
      </van-button>
    </div>
    <div class="tip">产量A/产量B/周期C 扣主积分；丰收祝福/灾祸减免扣农场积分（对冲关系见道具图鉴）；种植免费，收获产农场积分，攒满 100 可提现 1 积分</div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import api from '../api'
import { primeAudio, playDing } from '../sound'

const farm = ref({
  coins: 0, total_harvest: 0, level_a: 0, level_b: 0, level_c: 0, level_d: 0, level_e: 0,
  yield_per_round: 200, period_hours: 8, daily_income_max: 0,
  plots: [], next_plot_index: -1, next_plot_price: 0,
  upgrade_a: {}, upgrade_b: {}, upgrade_c: {}, upgrade_d: {}, upgrade_e: {},
  withdrawable: 0, harvest_now: 0, balance: 0
})
const busy = ref(false)
let timer = null
const tick = ref(Date.now())

const emptyCount = computed(() => farm.value.plots.filter((p) => p.unlocked && !p.planted_at).length)
const harvestableCount = computed(() => farm.value.plots.filter((p) => p.unlocked && p.planted_at && matureAt(p) && p.daily_left > 0).length)
const unlockedCount = computed(() => farm.value.plots.filter((p) => p.unlocked).length)
// 每轮收获 = 全部已开垦田一轮合计（= 一键全收一次的满额）
const roundIncome = computed(() => farm.value.yield_per_round * unlockedCount.value)

const upgradeLines = computed(() => {
  const f = farm.value
  return [
    {
      key: 'A', name: '产量 A', level: f.level_a, price: f.upgrade_a.price, currency: '积分', priceLabel: `${f.upgrade_a.price} 分`, maxed: f.upgrade_a.maxed,
      desc: `单田每轮收获 +2%，无副作用。当前单田每轮 ${fmtNum(f.yield_per_round)}`
    },
    {
      key: 'B', name: '产量 B', level: f.level_b, price: f.upgrade_b.price, currency: '积分', priceLabel: `${f.upgrade_b.price} 分`, maxed: f.upgrade_b.maxed,
      desc: `单田每轮收获 +6%，但生长周期 +8%。当前单田每轮 ${fmtNum(f.yield_per_round)} / ${fmtHours(f.period_hours)}`
    },
    {
      key: 'C', name: '周期 C', level: f.level_c, price: f.upgrade_c.price, currency: '积分', priceLabel: `${f.upgrade_c.price} 分`, maxed: f.upgrade_c.maxed,
      desc: `生长周期 -5%，收获来得更快。当前 ${fmtHours(f.period_hours)}`
    },
    {
      key: 'D', name: '丰收祝福', level: f.level_d, price: f.upgrade_d.price, currency: '农场积分', priceLabel: fmtK(f.upgrade_d.price) + ' 分', maxed: f.upgrade_d.maxed, color: '#10b981',
      desc: `每日结算：昨日净收益 ×${10 * f.level_d}%——净赚发奖励，净亏放大亏损（灾祸减免可对冲）。扣农场积分`
    },
    {
      key: 'E', name: '灾祸减免', level: f.level_e, price: f.upgrade_e.price, currency: '农场积分', priceLabel: fmtK(f.upgrade_e.price) + ' 分', maxed: f.upgrade_e.maxed, color: '#10b981',
      desc: `每级抵消 10% 的净亏损放大（只对冲，不能独立减罚）。扣农场积分`
    }
  ]
})

function fmtK(n) {
  const v = (n || 0) / 1000
  const r = Math.round(v * 10) / 10
  return (Number.isInteger(r) ? String(r) : r.toFixed(1)) + 'k'
}
function fmtNum(n) {
  const v = Math.round((n || 0) * 10) / 10
  return Number.isInteger(v) ? String(v) : v.toFixed(1)
}
function fmtHours(h) {
  return (Math.round((h || 0) * 10) / 10) + 'h'
}

// ---- 田格状态（本地实时算，后端 harvest 时做权威校验）----
function matureAt(p) {
  if (!p.planted_at) return false
  return tick.value - new Date(p.planted_at).getTime() >= farm.value.period_hours * 3600 * 1000
}
function progressOf(p) {
  const total = farm.value.period_hours * 3600 * 1000
  const done = tick.value - new Date(p.planted_at).getTime()
  return Math.min(100, Math.max(0, Math.round((done / total) * 100)))
}
function remainText(p) {
  const ms = new Date(p.planted_at).getTime() + farm.value.period_hours * 3600 * 1000 - tick.value
  if (ms <= 0) return '成熟'
  const m = Math.ceil(ms / 60000)
  return m >= 60 ? `${Math.floor(m / 60)}h${m % 60}m` : `${m}m`
}
function plotClass(p) {
  if (!p.unlocked) return p.plot_index === farm.value.next_plot_index ? 'locked buyable' : 'locked'
  if (!p.planted_at) return 'empty'
  if (!matureAt(p)) return 'growing'
  return p.daily_left > 0 ? 'mature' : 'done-today'
}

async function load() {
  farm.value = await api.get('/farm')
}

async function onPlot(p) {
  primeAudio()
  if (!p.unlocked) {
    if (p.plot_index !== farm.value.next_plot_index) return showToast('按顺序开垦')
    try {
      await showConfirmDialog({
        title: '开垦田地',
        message: `开垦第 ${p.plot_index + 1} 块田，花费 ${farm.value.next_plot_price} 积分？`
      })
    } catch (e) {
      return
    }
    return act(() => api.post('/farm/buy-plot'), `已开垦（${unlockedCount.value + 1}/24）`)
  }
  if (!p.planted_at) return act(() => api.post('/farm/plant', { plot_index: p.plot_index }), '已种下')
  if (!matureAt(p)) return showToast(`还差 ${remainText(p)}`)
  if (p.daily_left <= 0) return showToast('今日 2 轮已满，明天再来')
  return act(() => api.post('/farm/harvest', { plot_index: p.plot_index }), null)
}

async function plantAll() {
  primeAudio()
  return act(() => api.post('/farm/plant', {}), `已种下 ${emptyCount.value} 块`)
}

async function harvestAll() {
  primeAudio()
  return act(() => api.post('/farm/harvest', { all: true }), null)
}

async function doWithdraw() {
  primeAudio()
  try {
    await showConfirmDialog({
      title: '农场提现',
      message: `提现 ${farm.value.withdrawable} 积分到主积分？（100 农场积分 = 1 积分，小数留存）`
    })
  } catch (e) {
    return
  }
  return act(() => api.post('/farm/withdraw'), null)
}

async function doUpgrade(line) {
  primeAudio()
  const f = farm.value
  let msg = ''
  if (line.key === 'A') msg = `单格收获 ${fmtNum(f.yield_per_round)} → ${fmtNum(f.yield_per_round / (1 + 0.02 * f.level_a) * (1 + 0.02 * (f.level_a + 1)))}`
  if (line.key === 'B') {
    const baseY = f.yield_per_round / (1 + 0.06 * f.level_b)
    // 周期是加法合成：每升 1 级 B 固定 +8h×8% = 0.64h
    msg = `单格收获 ${fmtNum(f.yield_per_round)} → ${fmtNum(baseY * (1 + 0.06 * (f.level_b + 1)))}，周期 ${fmtHours(f.period_hours)} → ${fmtHours(f.period_hours + 0.64)}`
  }
  if (line.key === 'C') msg = `周期 ${fmtHours(f.period_hours)} → ${fmtHours(Math.max(1, f.period_hours - 0.4))}`
  if (line.key === 'D') {
    msg = `昨日净收益加成 +10% → +${10 * (f.level_d + 1)}%（净赚发奖励，净亏同倍放大亏损）`
  }
  if (line.key === 'E') {
    const remain = 10 * Math.max(0, f.level_d - f.level_e - 1)
    msg = `对冲净亏损放大 10% → 剩余放大 ${remain}%（只对冲，不独立减罚）`
  }
  try {
    await showConfirmDialog({ title: `${line.name} Lv${line.level + 1}`, message: `${msg}\n\n花费 ${line.price} ${line.currency}？` })
  } catch (e) {
    return
  }
  return act(() => api.post('/farm/upgrade', { line: line.key }), `${line.name} 已升到 Lv${line.level + 1}`)
}

async function act(fn, okMsg) {
  busy.value = true
  try {
    const res = await fn()
    if (res && res.gained) {
      playDing()
      showToast(`+${fmtNum(res.gained)} 农场积分`)
    } else if (res && res.points) {
      playDing()
      showToast(`已到账 ${res.points} 积分`)
    } else if (okMsg) {
      showToast(okMsg)
    }
    await load()
  } catch (e) {
    showToast(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  load()
  timer = setInterval(() => { tick.value = Date.now() }, 1000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.farm-card {
  background: linear-gradient(135deg, #4d9dff, #1989fa);
  border-radius: 16px;
  color: #fff;
  padding: 20px 20px 14px;
}
.fc-top { display: flex; justify-content: space-between; align-items: center; }
.fc-coins { font-size: 38px; font-weight: 700; line-height: 1.2; }
.fc-label { font-size: 12px; opacity: 0.85; margin-top: 2px; }
.fc-stats { display: flex; margin-top: 14px; border-top: 1px solid rgba(255,255,255,0.25); padding-top: 10px; }
.fc-stat { flex: 1; display: flex; flex-direction: column; gap: 2px; text-align: center; }
.fc-stat b { font-size: 16px; }
.fc-stat span { font-size: 11px; opacity: 0.85; }
.quick-row { display: flex; gap: 10px; margin: 14px 0 12px; }
.quick-row .van-button { flex: 1; }
.plots-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}
.plot {
  aspect-ratio: 1;
  border-radius: 12px;
  background: #fff;
  border: 1.5px solid #eef2f7;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  cursor: pointer;
  user-select: none;
  overflow: hidden;
  padding: 4px;
  box-sizing: border-box;
}
.plot.empty { border: 1.5px dashed #c8ddf5; }
.plot.empty .pl-plus { font-size: 22px; color: #1989fa; font-weight: 300; }
.plot.locked { background: #f7f8fa; border-color: #e5e6eb; }
.plot.locked.buyable { border: 1.5px dashed #1989fa; background: #f0f7ff; }
.plot.locked.buyable .pl-sub { color: #1989fa; }
.pl-lock { font-size: 18px; opacity: 0.5; }
.pl-crop { font-size: 26px; line-height: 1.2; }
.pl-crop.mature { animation: swing 1.2s ease-in-out infinite; }
.pl-crop.dim { opacity: 0.4; }
@keyframes swing {
  0%, 100% { transform: rotate(-8deg); }
  50% { transform: rotate(8deg); }
}
.pl-bar { width: 80%; }
.pl-sub { font-size: 10px; color: #969799; white-space: nowrap; }
.pl-sub.gold { color: #ff976a; font-weight: 600; }
.plot.mature { border-color: #ffd21e; background: #fffbe8; }
.section-title { margin: 18px 4px 10px; font-size: 14px; font-weight: 600; color: #323233; }
.up-card {
  background: #fff;
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.up-main { flex: 1; min-width: 0; }
.up-card .van-button { min-width: 80px; justify-content: center; }
.up-name { font-size: 15px; font-weight: 500; color: #323233; }
.up-lv { font-size: 12px; color: #1989fa; font-weight: 400; margin-left: 4px; }
.up-desc { font-size: 12px; color: #969799; margin-top: 2px; }
.tip { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 12px; padding: 0 20px; }
</style>
