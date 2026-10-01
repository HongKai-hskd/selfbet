<template>
  <div class="page">
    <van-nav-bar title="背包" left-arrow @click-left="$router.back()" @click-right="$router.push('/items-guide')">
      <template #right>
        <van-icon name="question-o" size="18" />
      </template>
    </van-nav-bar>

    <div class="section-head">宝箱 <span class="hint">点按开启</span></div>
    <div class="grid" v-if="boxes.length">
      <div v-for="b in boxes" :key="'b' + b.type_id" class="cell" @click="tapBox(b)">
        <span class="count">×{{ b.count }}</span>
        <div class="cell-icon">🎁</div>
        <div class="cell-name">{{ b.name }}</div>
        <div class="cell-sub">{{ b.min_points }}~{{ b.max_points }}</div>
      </div>
    </div>
    <van-empty v-else description="背包里没有宝箱，完成任务有概率获得" image-size="64" />

    <div class="section-head">道具 <span class="hint">点按使用</span></div>
    <div class="grid" v-if="items.length">
      <div v-for="it in items" :key="'i' + it.type_id" class="cell" @click="tapItem(it)">
        <span class="count">×{{ it.count }}</span>
        <div class="cell-icon">{{ it.icon || '🎴' }}</div>
        <div class="cell-name">{{ it.name }}</div>
      </div>
    </div>
    <van-empty v-else description="还没有道具，开宝箱有概率获得" image-size="64" />

    <div class="tip">完成任务有概率掉落宝箱或道具；宝箱要在这里手动开启，开启时积分才入账</div>

    <!-- 开箱方式选择 -->
    <van-action-sheet
      v-model:show="boxActionShow"
      :actions="boxActions"
      cancel-text="取消"
      @select="onBoxAction"
    />

    <!-- 开箱结果 -->
    <van-popup v-model:show="resultShow" round position="center" class="result-popup" :style="{ width: '78%' }">
      <div class="result-title">开箱结果</div>
      <div class="result-list">
        <div v-for="(r, i) in openResults" :key="i" class="result-row">
          <span>宝箱 #{{ i + 1 }}</span>
          <b class="gold">
            <template v-if="r.item">{{ r.icon || '🎴' }} {{ r.item }} ×{{ r.qty }} </template>
            +{{ r.points }} 分<span v-if="r.bonus" class="bonus-tag">⚡{{ r.bonus }}</span>
          </b>
        </div>
      </div>
      <div class="result-total" v-if="openTotal > 0">共 +{{ openTotal }} 积分</div>
      <van-button round block type="primary" class="result-btn" @click="resultShow = false">收下</van-button>
    </van-popup>

    <!-- 冷却重置卡：选择目标商品 -->
    <van-popup v-model:show="resetPickShow" round position="bottom">
      <van-picker
        :columns="resetColumns"
        @confirm="onPickReset"
        @cancel="resetPickShow = false"
        title="选择要重置冷却的商品"
      />
    </van-popup>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import api from '../api'

const boxes = ref([])
const items = ref([])
const cashBalanceCents = ref(0)
const pointBalance = ref(0)
const openResults = ref([])
const openTotal = ref(0)
const resultShow = ref(false)
const resetPickShow = ref(false)
const resetTargetType = ref(3)

// 道具元数据（icon/名称/交互模式/确认文案）全部来自 /backpack 接口，
// 后端 itemdef.go 注册表驱动——新增 confirm 型道具这里零改动
async function load() {
  const [bp, me, cash] = await Promise.all([api.get('/backpack'), api.get('/me'), api.get('/cash', { params: { limit: 1 } })])
  boxes.value = bp.boxes || []
  items.value = bp.items || []
  pointBalance.value = me.balance
  cashBalanceCents.value = cash.balance_cents
}

function tapBox(b) {
  boxActionTarget.value = b
  boxActionShow.value = true
}

const boxActionShow = ref(false)
const boxActionTarget = ref(null)
const boxActions = computed(() => [
  { name: '开一个', value: 1 },
  { name: `全部开启（${boxActionTarget.value?.count || 0} 个）`, value: boxActionTarget.value?.count || 0 }
])

async function onBoxAction(act) {
  boxActionShow.value = false
  const b = boxActionTarget.value
  if (!b || !act.value) return
  const res = await api.post('/backpack/open', { type_id: b.type_id, count: act.value })
  openResults.value = res.results || []
  openTotal.value = res.total || 0
  resultShow.value = true
  load()
}

async function tapItem(it) {
  // 即时结算型（弹窗带动态金额）：利息卡两张保留前端计算
  if (it.type_id === 1) {
    const pts = Math.max(0, Math.floor(pointBalance.value * 0.03))
    try {
      await showConfirmDialog({
        title: it.name,
        message: pts > 0
          ? `使用后立刻获得当前积分的 3%：\n+${pts} 分（当前积分 ${pointBalance.value}）`
          : '当前积分利息为 0 分，确认使用？'
      })
    } catch { return }
    const res = await api.post('/backpack/use', { type_id: it.type_id })
    showToast(`利息 +${res.points} 分已到账`)
    load()
  } else if (it.type_id === 2) {
    const yuan = (Math.floor(cashBalanceCents.value * 0.03) / 100).toFixed(2)
    try {
      await showConfirmDialog({
        title: it.name,
        message: parseFloat(yuan) > 0
          ? `使用后立刻获得当前余额的 3%：\n+¥${yuan}（当前余额 ¥${(cashBalanceCents.value / 100).toFixed(2)}）`
          : '当前余额利息为 ¥0.00，确认使用？'
      })
    } catch { return }
    const res = await api.post('/backpack/use', { type_id: it.type_id })
    showToast(`利息 +¥${(res.cents / 100).toFixed(2)} 已到账`)
    load()
  } else if (it.use_mode === 'shop_cooldown') {
    // 选目标商品的道具（如冷却重置卡）
    const shop = await api.get('/shop')
    const cooling = (shop.items || []).filter((s) => {
      if (!s.last_redeemed_at || !s.cooldown_days) return false
      return Date.now() < new Date(s.last_redeemed_at).getTime() + s.cooldown_days * 86400000
    })
    if (!cooling.length) return showToast('没有冷却中的商品')
    resetColumns.value = cooling.map((s) => ({ text: s.name, value: s.id }))
    resetTargetType.value = it.type_id
    resetPickShow.value = true
  } else {
    // 通用确认型：文案与激活提示全部来自后端注册表
    confirmUse(it.name, it.use_prompt || '确认使用？', it.type_id)
  }
}

// 统一确认弹窗 → 使用 → toast → 刷新（确认型道具共用）
async function confirmUse(title, message, typeId) {
  try {
    await showConfirmDialog({ title, message })
  } catch { return }
  try {
    const res = await api.post('/backpack/use', { type_id: typeId })
    showToast(res.message || '已使用')
    load()
  } catch (e) {
    showToast(e)
  }
}

const resetColumns = ref([])
async function onPickReset({ selectedOptions }) {
  resetPickShow.value = false
  try {
    const res = await api.post('/backpack/use', { type_id: 3, target_id: selectedOptions[0].value })
    showToast(res.message || '已重置冷却')
    load()
  } catch (e) {
    showToast(e)
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding-bottom: 30px; }
.section-head {
  font-size: 14px;
  font-weight: 600;
  color: #323233;
  margin: 18px 16px 10px;
}
.section-head .hint { font-size: 11px; color: #c8c9cc; font-weight: 400; margin-left: 6px; }
.grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 8px;
  padding: 0 12px;
}
.cell {
  position: relative;
  background: #fff;
  border-radius: 10px;
  padding: 10px 2px 8px;
  text-align: center;
  cursor: pointer;
}
.cell:active { background: #f2f3f5; }
.count {
  position: absolute;
  top: 3px;
  right: 3px;
  font-size: 10px;
  color: #fff;
  background: #ee0a24;
  border-radius: 8px;
  padding: 0 4px;
  line-height: 15px;
}
.cell-icon { font-size: 22px; line-height: 1.3; }
.cell-name {
  font-size: 10px;
  color: #323233;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cell-sub { font-size: 9px; color: #c8c9cc; }
.tip { text-align: center; font-size: 11px; color: #c8c9cc; margin-top: 18px; padding: 0 20px; }
.result-popup { padding: 24px 16px; text-align: center; }
.result-title { font-size: 16px; font-weight: 600; margin-bottom: 12px; }
.result-list { max-height: 220px; overflow-y: auto; }
.result-row {
  display: flex;
  justify-content: space-between;
  font-size: 14px;
  padding: 6px 4px;
  border-bottom: 1px solid #f2f3f5;
}
.gold { color: #ff9900; }
.bonus-tag { font-size: 11px; margin-left: 4px; }
.result-total { font-size: 15px; font-weight: 600; margin: 12px 0 4px; }
.result-btn { margin-top: 16px; }
</style>
