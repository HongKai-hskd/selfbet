<template>
  <div class="page">
    <van-nav-bar title="设置" left-arrow @click-left="$router.back()" />

    <div class="section-title">结算顺序（拖拽调整）</div>
    <div class="tip">罚分结算固定最前——它是昨日账的基础。排在前面的项，其产出会被后面的丰收祝福按倍率连锁放大。</div>
    <div ref="orderEl" class="order-list">
      <div v-for="item in orderItems" :key="item.key" class="order-card" :data-key="item.key">
        <div class="drag-handle">☰</div>
        <div class="order-main">
          <div class="order-name">{{ item.name }}</div>
          <div class="order-desc">{{ item.desc }}</div>
        </div>
      </div>
    </div>

    <div class="section-title">可预支金额</div>
    <div class="tip">余额最低可到 -X（罚分结算与商城透支共用此额度）</div>
    <van-cell-group inset>
      <van-field label="可负到" label-width="90">
        <template #input>
          <span class="neg-preview">-</span><van-stepper v-model="advanceLimit" :min="0" :max="100000" :step="100" integer />
        </template>
      </van-field>
    </van-cell-group>

    <div class="section-title">透支利息</div>
    <div class="tip">余额为负时，每天按「当日欠款 × 日利率」扣息（利息计入欠款，自然复利）；0 = 不计息。例：欠 1000、利率 10% → 每天扣 100</div>
    <van-cell-group inset>
      <van-field label="日利率 %" label-width="90">
        <template #input>
          <van-stepper v-model="overdraftRate" :min="0" :max="100" integer />
        </template>
      </van-field>
    </van-cell-group>

    <div class="section-title">冷却溢价</div>
    <div class="tip">商品冷却期内仍可购买，价格 × 溢价倍数（避免临时需要的意外情况）</div>
    <van-cell-group inset>
      <van-field label="溢价 %" label-width="90">
        <template #input>
          <van-stepper v-model="premiumPercent" :min="100" :max="1000" :step="10" integer />
        </template>
      </van-field>
    </van-cell-group>

    <div class="section-title">全勤奖</div>
    <div class="tip">昨日零罚分且有产出 → 次日结算时发放（免罚金牌豁免不算罚分）</div>
    <van-cell-group inset>
      <van-field label="奖励金额" label-width="90">
        <template #input>
          <van-stepper v-model="perfectDayAmount" :min="0" :max="500" integer />
        </template>
      </van-field>
    </van-cell-group>

    <div class="section-title">无冷却奖励（按可用天数梯度）</div>
    <div class="tip">特指「奖励自己一次」商品（商品编辑里开「奖励型」开关，暂只有一个）：冷却结束后每天发放，越久没兑换补得越多；兑换当天重新进冷却、停发</div>
    <van-cell-group inset>
      <van-field v-for="(t, i) in tiers" :key="i" :label="tierLabel(i)" label-width="90">
        <template #input>
          <van-stepper v-model="tiers[i].amount" :min="0" :max="1000" integer />
        </template>
      </van-field>
    </van-cell-group>

    <div style="margin: 16px">
      <van-button round block type="primary" :loading="saving" @click="save">保存设置</van-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { showToast } from 'vant'
import Sortable from 'sortablejs'
import api from '../api'

const KEY_NAMES = {
  overdraft_interest: '透支利息',
  perfect_day: '全勤奖',
  cooldown_reward: '无冷却奖励',
  farm_boost: '丰收祝福（昨日净值倍率）'
}
const KEY_DESCS = {
  overdraft_interest: '余额为负时按当日欠款每日计息（设置利率 >0 才生效）',
  perfect_day: '昨日零扣分且有产出 → 发固定奖励',
  cooldown_reward: '奖励型商品不在冷却中 → 按可用天数梯度发放',
  farm_boost: '昨日净收益 ×D 级倍率（净赚发奖/净亏放大），压轴连锁放大'
}

const order = ref(['overdraft_interest', 'perfect_day', 'cooldown_reward', 'farm_boost'])
const perfectDayAmount = ref(50)
const tiers = ref([
  { days: 3, amount: 50 },
  { days: 7, amount: 80 },
  { days: 30, amount: 120 },
  { days: 0, amount: 150 }
])
const advanceLimit = ref(500)
const overdraftRate = ref(0)
const premiumPercent = ref(200)
const saving = ref(false)

const orderItems = computed(() =>
  order.value.map((k) => ({ key: k, name: KEY_NAMES[k] || k, desc: KEY_DESCS[k] || '' }))
)
function tierLabel(i) {
  const prev = i === 0 ? 0 : tiers.value[i - 1].days
  const cur = tiers.value[i].days
  return cur === 0 ? `${prev} 天以上` : `${prev}~${cur} 天`
}

let sortable = null
const orderEl = ref(null)
function initSortable() {
  if (sortable || !orderEl.value) return
  sortable = Sortable.create(orderEl.value, {
    handle: '.drag-handle',
    animation: 150,
    onEnd: ({ oldIndex, newIndex }) => {
      if (oldIndex === newIndex) return
      const moved = order.value.splice(oldIndex, 1)[0]
      order.value.splice(newIndex, 0, moved)
    }
  })
}

onMounted(async () => {
  const d = await api.get('/settle-settings')
  if (Array.isArray(d.order) && d.order.length) order.value = d.order
  if (d.perfect_day_amount >= 0) perfectDayAmount.value = d.perfect_day_amount
  if (Array.isArray(d.cooldown_tiers) && d.cooldown_tiers.length) tiers.value = d.cooldown_tiers
  if (d.advance_limit >= 0) advanceLimit.value = d.advance_limit
  if (d.overdraft_daily_rate >= 0) overdraftRate.value = d.overdraft_daily_rate
  if (d.cooldown_premium_percent >= 100) premiumPercent.value = d.cooldown_premium_percent
  await nextTick()
  initSortable()
})
onUnmounted(() => {
  if (sortable) sortable.destroy()
  sortable = null
})

async function save() {
  saving.value = true
  try {
    await api.put('/settle-settings', {
      order: order.value,
      perfect_day_amount: perfectDayAmount.value,
      cooldown_tiers: tiers.value,
      advance_limit: advanceLimit.value,
      overdraft_daily_rate: overdraftRate.value,
      cooldown_premium_percent: premiumPercent.value
    })
    showToast('已保存')
  } catch (e) {
    showToast(e)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.page { padding-bottom: 40px; }
.section-title { font-size: 14px; font-weight: 600; color: #323233; margin: 18px 16px 8px; }
.tip { font-size: 12px; color: #969799; margin: 0 16px 8px; line-height: 1.5; }
.order-list { margin: 0 16px; }
.neg-preview { color: #ee0a24; font-weight: 600; margin-right: 2px; }
.order-card {
  display: flex; align-items: center; gap: 10px;
  background: #fff; border-radius: 8px; padding: 12px; margin-bottom: 8px;
}
.drag-handle { cursor: grab; color: #c8c9cc; font-size: 16px; padding: 0 4px; }
.order-name { font-size: 14px; font-weight: 500; color: #323233; }
.order-desc { font-size: 12px; color: #969799; margin-top: 2px; }
</style>
