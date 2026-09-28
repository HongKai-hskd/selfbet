<template>
  <div class="page">
    <div class="shop-head">
      <span class="shop-balance">积分 <b>{{ balance }}</b> · 余额 <b class="cash">¥{{ (cashBalance / 100).toFixed(2) }}</b></span>
      <div class="head-btns">
        <van-button size="small" round plain type="default" icon="records" @click="openPurchases">购买记录</van-button>
        <van-button size="small" round plain type="primary" icon="plus" @click="openForm()">上新</van-button>
      </div>
    </div>

    <div ref="listEl" class="shop-list">
      <div v-for="item in items" :key="item.id" class="shop-card">
        <div class="drag-handle">☰</div>
        <div class="shop-main">
          <div class="shop-name">
            {{ item.name }}
            <span v-if="item.cooldown_days > 0" class="cd-chip">🔥 {{ item.cooldown_days }} 天冷却</span>
          </div>
          <div v-if="item.description" class="shop-desc">{{ item.description }}</div>
          <div class="shop-meta">
            <span class="shop-price">{{ item.price }} 分</span>
            <span v-if="item.redeemed_count" class="shop-count">已兑 {{ item.redeemed_count }} 次</span>
          </div>
        </div>
        <div class="shop-actions">
          <van-button
            v-if="cooldownLeft(item) > 0" size="small" round plain disabled
          >
            冷却 {{ cooldownLeft(item) }} 天
          </van-button>
          <van-button
            v-else size="small" round :type="balance >= item.price ? 'primary' : 'default'" :disabled="balance < item.price"
            :loading="redeemingId === item.id" @click="openRedeem(item)"
          >
            {{ balance >= item.price ? '兑换' : `还差 ${item.price - balance}` }}
          </van-button>
          <van-icon name="edit" class="del-icon" @click="openForm(item)" />
          <van-icon name="delete-o" class="del-icon" @click="remove(item)" />
        </div>
      </div>
      <van-empty v-if="!items.length" description="商城还是空的，点右上角上新" />
    </div>
    <div class="drag-tip" v-if="items.length > 1">按住 ☰ 上下拖动可调整顺序</div>

    <!-- 购买记录 -->
    <van-popup v-model:show="purchaseShow" round position="bottom" style="height: 75%; display: flex; flex-direction: column; padding: 20px 16px 16px">
      <div class="form-title">购买记录</div>
      <div class="purchase-filter">
        <span
          class="chip" :class="{ active: purchaseFilter === 0 }" @click="purchaseFilter = 0; loadPurchases()"
        >全部</span>
        <span
          v-for="it in items" :key="it.id"
          class="chip" :class="{ active: purchaseFilter === it.id }" @click="purchaseFilter = it.id; loadPurchases()"
        >{{ it.name }}</span>
      </div>
      <div class="purchase-list">
        <div v-for="p in purchaseList" :key="p.id" class="purchase-item">
          <div class="ledger-main">
            <div class="p-note">{{ p.note }}</div>
            <div class="ledger-time">{{ fmtTime(p.created_at) }}</div>
          </div>
          <div class="ledger-amount minus">{{ p.amount }}</div>
        </div>
        <van-empty v-if="!purchaseList.length" description="还没有购买记录" />
      </div>
    </van-popup>

    <!-- 上新/编辑 -->
    <van-popup v-model:show="formShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">{{ form.id ? '编辑商品' : '上新商品' }}</div>
      <van-form @submit="save">
        <van-cell-group inset>
          <van-field v-model="form.name" label="名称" placeholder="奶茶一杯 / 半天游戏时间" :rules="[{ required: true, message: '请填名称' }]" />
          <van-field v-model="form.price" type="digit" label="所需积分" placeholder="500" :rules="[{ required: true, message: '请填价格' }]" />
          <van-field v-model="form.description" label="说明" placeholder="备注（可空）" />
          <van-field v-model="form.cooldown_days" type="digit" label="冷却天数" placeholder="0（买后 N 天内不能再兑）" />
        </van-cell-group>
        <div style="margin: 16px 16px 0">
          <van-button round block type="primary" native-type="submit">保存</van-button>
        </div>
      </van-form>
    </van-popup>

    <!-- 兑换弹层 -->
    <van-popup v-model:show="redeemShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">兑换确认</div>
      <template v-if="redeemItem">
        <div class="rd-name">{{ redeemItem.name }}</div>
        <div class="rd-price">单价 {{ redeemItem.price }} 分</div>
        <div class="rd-qty">
          <span class="rd-label">兑换数量</span>
          <van-stepper v-model="redeemQty" :min="1" :max="maxQty" integer />
        </div>
        <div class="rd-total">
          <span>合计</span>
          <b class="rd-total-num">{{ redeemItem.price * redeemQty }}</b>
          <span>分</span>
        </div>
        <div class="rd-balance">兑换后剩余 {{ balance - redeemItem.price * redeemQty }} 分</div>
        <van-button round block type="primary" style="margin-top: 18px" :loading="redeemingId != null" @click="confirmRedeem">
          确认兑换
        </van-button>
      </template>
    </van-popup>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import Sortable from 'sortablejs'
import api from '../api'

const items = ref([])
const balance = ref(0)
const cashBalance = ref(0)
const formShow = ref(false)
const redeemingId = ref(null)
const redeemShow = ref(false)
const redeemItem = ref(null)
const redeemQty = ref(1)
const listEl = ref(null)
let sortable = null

const emptyForm = { id: null, name: '', price: '', description: '', cooldown_days: '' }
const form = ref({ ...emptyForm })

const maxQty = computed(() =>
  redeemItem.value ? Math.max(1, Math.floor(balance.value / redeemItem.value.price)) : 1
)

const purchaseShow = ref(false)
const purchaseFilter = ref(0)
const purchaseList = ref([])

function openPurchases() {
  purchaseFilter.value = 0
  purchaseShow.value = true
  loadPurchases()
}

async function loadPurchases() {
  const params = { type: 'shop', limit: 200 }
  if (purchaseFilter.value) params.ref_id = purchaseFilter.value
  try {
    const res = await api.get('/ledger', { params })
    purchaseList.value = res.items || []
  } catch (e) {
    showToast(e)
  }
}

function fmtTime(s) {
  const d = new Date(s)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function cooldownLeft(item) {
  if (!item.cooldown_days || !item.last_redeemed_at) return 0
  const end = new Date(item.last_redeemed_at).getTime() + item.cooldown_days * 86400000
  const left = Math.ceil((end - Date.now()) / 86400000)
  return left > 0 ? left : 0
}

async function load() {
  const [s, l, c] = await Promise.all([api.get('/shop'), api.get('/ledger', { params: { limit: 1 } }), api.get('/cash')])
  items.value = s.items || []
  balance.value = l.balance
  cashBalance.value = c.balance_cents
  initSortable()
}

function initSortable() {
  if (sortable || !listEl.value || items.value.length < 2) return
  sortable = Sortable.create(listEl.value, {
    handle: '.drag-handle',
    animation: 150,
    onEnd: async ({ oldIndex, newIndex }) => {
      if (oldIndex === newIndex) return
      const list = items.value
      const moved = list.splice(oldIndex, 1)[0]
      list.splice(newIndex, 0, moved)
      try {
        await api.post('/shop/reorder', { ids: list.map((x) => x.id) })
        showToast('顺序已保存')
      } catch (e) {
        showToast(e)
        load()
      }
    }
  })
}

onMounted(load)

function openForm(item) {
  form.value = item
    ? { id: item.id, name: item.name, price: String(item.price), description: item.description, cooldown_days: item.cooldown_days ? String(item.cooldown_days) : '' }
    : { ...emptyForm }
  formShow.value = true
}

async function save() {
  const f = form.value
  const body = { name: f.name, price: parseInt(f.price) || 0, description: f.description, cooldown_days: parseInt(f.cooldown_days) || 0 }
  try {
    if (f.id) await api.put(`/shop/${f.id}`, body)
    else await api.post('/shop', body)
    formShow.value = false
    showToast('已保存')
    load()
  } catch (e) {
    showToast(e)
  }
}

function openRedeem(item) {
  redeemItem.value = item
  redeemQty.value = 1
  redeemShow.value = true
}

async function confirmRedeem() {
  const item = redeemItem.value
  redeemingId.value = item.id
  try {
    const res = await api.post(`/shop/${item.id}/redeem`, { quantity: redeemQty.value })
    balance.value = res.balance
    redeemShow.value = false
    showToast(res.quantity > 1 ? `已兑换 ${res.quantity} 份，剩余 ${res.balance} 分` : `兑换成功，剩余 ${res.balance} 分`)
    load()
  } catch (e) {
    showToast(e)
  } finally {
    redeemingId.value = null
  }
}

async function remove(item) {
  try {
    await showConfirmDialog({ title: '删除商品', message: `确定删除「${item.name}」？` })
  } catch (e) {
    return
  }
  try {
    await api.delete(`/shop/${item.id}`)
    load()
  } catch (e) {
    showToast(e)
  }
}
</script>

<style scoped>
.shop-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.shop-balance { font-size: 14px; color: #646566; }
.shop-balance b { color: #1989fa; font-size: 18px; }
.shop-balance .cash { color: #ff9900; }
.head-btns { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.purchase-filter {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 8px;
  -webkit-overflow-scrolling: touch;
}
.purchase-filter::-webkit-scrollbar { display: none; }
.purchase-filter .chip { flex-shrink: 0; padding: 4px 12px; border-radius: 14px; background: #f7f8fa; color: #646566; font-size: 12px; border: 1px solid #ebedf0; }
.purchase-filter .chip.active { background: #1989fa; color: #fff; border-color: #1989fa; }
.purchase-list { flex: 1; overflow-y: auto; }
.purchase-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 4px;
  border-bottom: 1px solid #f7f8fa;
}
.purchase-item .p-note { font-size: 14px; color: #323233; }
.shop-balance .cash { color: #ff9900; }
.cd-chip {
  font-size: 11px;
  color: #ff9900;
  background: #fff7ec;
  border-radius: 4px;
  padding: 1px 5px;
  margin-left: 4px;
  font-weight: 400;
}
.shop-card {
  background: #fff;
  border-radius: 12px;
  padding: 14px;
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.drag-handle {
  color: #c8c9cc;
  font-size: 18px;
  padding: 8px 4px;
  cursor: grab;
  touch-action: none;
  user-select: none;
}
.shop-main { flex: 1; min-width: 0; }
.shop-name { font-size: 15px; font-weight: 500; color: #323233; }
.shop-desc { font-size: 12px; color: #969799; margin-top: 2px; }
.shop-meta { margin-top: 6px; display: flex; gap: 10px; font-size: 13px; }
.shop-price { color: #1989fa; font-weight: 600; }
.shop-count { color: #c8c9cc; font-size: 12px; }
.shop-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.del-icon { color: #c8c9cc; font-size: 17px; padding: 4px; }
.drag-tip { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 6px; }
.form-title { text-align: center; font-size: 16px; font-weight: 600; margin-bottom: 12px; color: #323233; }
.rd-name { text-align: center; font-size: 17px; font-weight: 600; color: #323233; }
.rd-price { text-align: center; font-size: 13px; color: #969799; margin-top: 4px; }
.rd-qty {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #f7f8fa;
  border-radius: 10px;
  padding: 14px;
  margin-top: 18px;
}
.rd-label { font-size: 14px; color: #323233; }
.rd-total {
  display: flex;
  align-items: baseline;
  justify-content: center;
  gap: 6px;
  margin-top: 18px;
  color: #969799;
  font-size: 13px;
}
.rd-total-num { font-size: 34px; font-weight: 700; color: #1989fa; }
.rd-balance { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 6px; }
</style>
