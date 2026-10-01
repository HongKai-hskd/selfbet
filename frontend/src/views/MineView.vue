<template>
  <div class="page">
    <div class="me-card">
      <div class="me-balance">{{ me.balance }}</div>
      <div class="me-label">当前积分</div>
      <div class="me-grid">
        <div class="me-cell"><b>{{ me.total_earned }}</b><span>累计获得</span></div>
        <div class="me-cell"><b>{{ me.total_spent }}</b><span>累计消费</span></div>
        <div class="me-cell"><b>{{ me.tasks_done }}</b><span>完成任务</span></div>
      </div>
    </div>

    <div class="backpack-entry" @click="$router.push('/backpack')">
      <div class="be-icon">🎒</div>
      <div class="be-name">背包</div>
      <div class="be-sub" v-if="me.backpack_count">{{ me.backpack_count }} 件待处理</div>
      <div class="be-sub" v-else>暂无待处理物品</div>
      <van-icon name="arrow" class="be-arrow" />
    </div>
    <div class="backpack-entry" @click="$router.push('/schedule')">
      <div class="be-icon">📅</div>
      <div class="be-name">日程</div>
      <div class="be-sub">任务计划时间一览</div>
      <van-icon name="arrow" class="be-arrow" />
    </div>
    <div class="backpack-entry" @click="$router.push('/farm')">
      <div class="be-icon">🌾</div>
      <div class="be-name">农场</div>
      <div class="be-sub">种田赚农场积分，100 = 1 积分</div>
      <van-icon name="arrow" class="be-arrow" />
    </div>

    <div class="section-title">宝箱管理</div>
    <div class="box-list">
      <div v-for="b in boxes" :key="b.id" class="box-card">
        <div class="box-emoji">🎁</div>
        <div class="box-main">
          <div class="box-name">{{ b.name }}</div>
          <div class="box-range">开出 {{ b.min_points }} ~ {{ b.max_points }} 积分</div>
        </div>
        <div class="box-actions">
          <van-button size="small" plain round @click="openBoxForm(b)">编辑</van-button>
          <van-icon name="delete-o" class="del-icon" @click="removeBox(b)" />
        </div>
      </div>
      <van-empty v-if="!boxes.length" description="还没有宝箱类型" />
    </div>
    <van-button block round plain type="primary" icon="plus" class="add-box-btn" @click="openBoxForm()">
      新建宝箱类型
    </van-button>
    <div class="tip">在任务里关联宝箱并设置掉率；掉落的宝箱进背包，开启时才入账。道具掉落为附加掉率，开启时积分照得（数量 = 开到时一次进背包几张），各道具概率之和 ≤ 100</div>

    <van-popup v-model:show="boxFormShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">{{ boxForm.id ? '编辑宝箱' : '新建宝箱' }}</div>
      <van-form @submit="saveBox">
        <van-cell-group inset>
          <van-field v-model="boxForm.name" label="名称" placeholder="小奖励 / 大奖池" :rules="[{ required: true, message: '请填名称' }]" />
          <van-field v-model="boxForm.min_points" type="digit" label="最少积分" placeholder="5" :rules="[{ required: true, message: '必填' }]" />
          <van-field v-model="boxForm.max_points" type="digit" label="最多积分" placeholder="50" :rules="[{ required: true, message: '必填' }]" />
          <div class="drops-head">
            <span>道具掉落（附加掉率，积分必得 {{ boxForm.min_points || 0 }}~{{ boxForm.max_points || 0 }}）</span>
            <van-button size="mini" round plain type="primary" :disabled="boxForm.item_drops.length >= itemPool.length" @click="addDropRow">+ 添加</van-button>
          </div>
          <div v-for="(row, idx) in boxForm.item_drops" :key="idx" class="drop-row">
            <van-field readonly is-link :model-value="dropTypeName(row.item_type)" @click="openDropTypePicker(idx)" />
            <van-field v-model="row.rate" type="digit" label="概率%" placeholder="0-100" />
            <van-field v-model="row.qty" type="digit" label="数量" placeholder="1" />
            <van-icon name="delete-o" class="drop-del" @click="boxForm.item_drops.splice(idx, 1)" />
          </div>
        </van-cell-group>
        <div style="margin: 16px 16px 0">
          <van-button round block type="primary" native-type="submit">保存</van-button>
        </div>
      </van-form>
    </van-popup>
    <van-popup v-model:show="dropPickShow" round position="bottom">
      <van-picker :columns="dropPickColumns" title="选择道具" @confirm="onDropTypePick" @cancel="dropPickShow = false" />
    </van-popup>
    <div class="section-title">余额（现金）</div>
    <div class="cash-card">
      <div class="cash-top">
        <div>
          <div class="cash-balance">¥{{ (cashBalance / 100).toFixed(2) }}</div>
          <div class="cash-label">10 积分 = 1 元</div>
        </div>
        <div class="cash-actions">
          <van-button size="small" round type="primary" @click="openExchange">兑换</van-button>
          <van-button size="small" round plain type="primary" @click="spendShow = true">记一笔</van-button>
        </div>
      </div>
      <div class="cash-flows" v-if="cashFlows.length">
        <div v-for="f in cashFlows.slice(0, 5)" :key="f.id" class="cash-flow-item">
          <span class="cash-note">{{ f.note }}</span>
          <span :class="f.amount_cents > 0 ? 'plus' : 'minus'">
            {{ f.amount_cents > 0 ? '+' : '-' }}¥{{ (Math.abs(f.amount_cents) / 100).toFixed(2) }}
          </span>
        </div>
      </div>
      <div class="cash-empty" v-else>兑换和消费记录会显示在这里</div>
      <div class="cash-more" v-if="cashFlows.length" @click="$router.push('/cash')">查看全部 ›</div>
    </div>

    <!-- 积分兑换余额 -->
    <van-popup v-model:show="exchangeShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">积分兑换余额</div>
      <van-cell-group inset>
        <van-field v-model="exchangePoints" type="digit" label="积分" placeholder="1000（10 的倍数）" required>
          <template #button><span class="pts-hint">分</span></template>
        </van-field>
      </van-cell-group>
      <div class="ex-summary">
        <div class="ex-row">
          <span class="ex-label">可得余额</span>
          <span class="ex-val money">¥{{ exchangeYuan }}</span>
        </div>
        <div class="ex-row">
          <span class="ex-label">兑换后积分剩余</span>
          <span class="ex-val" :class="{ over: remaining < 0 }">{{ remaining }} 分</span>
        </div>
      </div>
      <div style="margin: 16px 16px 0">
        <van-button round block type="primary" :disabled="!exchangePoints || (parseInt(exchangePoints) || 0) % 10 !== 0 || remaining < 0" :loading="exchanging" @click="doExchange">
          确认兑换
        </van-button>
      </div>
    </van-popup>

    <!-- 记一笔消费 -->
    <van-popup v-model:show="spendShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">记一笔消费</div>
      <van-cell-group inset>
        <van-field v-model="spendYuanStr" type="number" label="金额" placeholder="20.00" required>
          <template #button><span class="pts-hint">元</span></template>
        </van-field>
        <van-field v-model="spendNote" label="买了什么" placeholder="零食 / 玩具 / …" required />
      </van-cell-group>
      <div class="ex-summary">
        <div class="ex-row">
          <span class="ex-label">当前余额</span>
          <span class="ex-val money">¥{{ (cashBalance / 100).toFixed(2) }}</span>
        </div>
        <div class="ex-row">
          <span class="ex-label">消费后余额</span>
          <span class="ex-val" :class="{ over: spendAfter < 0 }">¥{{ spendAfter.toFixed(2) }}</span>
        </div>
      </div>
      <div style="margin: 16px 16px 0">
        <van-button round block type="primary" :disabled="!(parseFloat(spendYuanStr) > 0) || !spendNote.trim() || spendAfter < 0" :loading="spending" @click="doSpend">
          记录消费
        </van-button>
      </div>
    </van-popup>

    <div class="section-title">分组管理</div>
    <div class="tag-add">
      <van-field v-model="newTagName" placeholder="新分组名称，如：英语 / 数学" class="tag-add-field" />
      <van-button size="small" round type="primary" :disabled="!newTagName.trim()" @click="addTag">添加</van-button>
    </div>
    <div ref="tagListEl" class="tag-list">
      <div v-for="tg in tags" :key="tg.id" class="box-card">
        <div class="drag-handle">☰</div>
        <div class="box-emoji"><span class="tag-dot" :style="{ background: tg.color || '#1989fa' }"></span></div>
        <div class="box-main">
          <div class="box-name">{{ tg.name }}</div>
          <div class="box-range">{{ tg.task_count }} 任务</div>
        </div>
        <div class="box-actions">
          <van-button size="small" plain round @click="openTagEdit(tg)">编辑</van-button>
          <van-icon name="delete-o" class="del-icon" @click="removeTag(tg)" />
        </div>
      </div>
      <van-empty v-if="!tags.length" description="还没有分组，上方输入框创建" />
    </div>
    <div class="tip" v-if="tags.length > 1">按住 ☰ 拖动调整分组顺序，任务页的筛选和下拉都会跟随</div>
    <div class="tip">分组用于任务归类与筛选；改名后所有任务自动跟随，删除后组内任务变为未分组</div>

    <van-popup v-model:show="tagEditShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">编辑分组</div>
      <van-cell-group inset>
        <van-field v-model="tagEditName" label="名称" placeholder="新名称" :rules="[{ required: true, message: '必填' }]" />
      </van-cell-group>
      <div class="color-label">颜色</div>
      <div class="color-grid">
        <span
          v-for="c in TAG_COLORS" :key="c"
          class="color-dot" :class="{ active: tagEditColor === c }"
          :style="{ background: c }"
          @click="tagEditColor = c"
        />
      </div>
      <div style="margin: 16px 16px 0">
        <van-button round block type="primary" @click="saveTagEdit">保存</van-button>
      </div>
    </van-popup>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import Sortable from 'sortablejs'
import api from '../api'

const me = ref({ balance: 0, total_earned: 0, total_spent: 0, tasks_done: 0 })
const boxes = ref([])
const tags = ref([])
const newTagName = ref('')
const tagEditShow = ref(false)
const tagEdit = ref(null)
const tagEditName = ref('')
const tagEditColor = ref('#1989fa')
const TAG_COLORS = ['#1989fa', '#07c160', '#00b8d4', '#7232dd', '#ff6699', '#ff976a', '#ee0a24', '#969797']
const tagListEl = ref(null)
let tagSortable = null
const boxFormShow = ref(false)
const boxForm = ref({ id: null, name: '', min_points: '', max_points: '', item_drops: [] })
// 道具池来自 /api/items/guide（后端注册表驱动），新增道具自动可选
const itemPool = ref([])
const dropTypeName = (t) => (itemPool.value.find((d) => d.id === t) || {}).name || '选择道具'
const dropPickShow = ref(false)
const dropPickIdx = ref(-1)
const dropPickColumns = computed(() => itemPool.value.map((d) => ({ text: d.name, value: d.id })))

function addDropRow() {
  const used = boxForm.value.item_drops.map((r) => r.item_type)
  const free = dropPickColumns.value.find((c) => !used.includes(c.value))
  if (!free) return
  boxForm.value.item_drops.push({ item_type: free.value, rate: '', qty: '1' })
}

function openDropTypePicker(idx) {
  dropPickIdx.value = idx
  dropPickShow.value = true
}

function onDropTypePick({ selectedOptions }) {
  boxForm.value.item_drops[dropPickIdx.value].item_type = selectedOptions[0].value
  dropPickShow.value = false
}
const cashBalance = ref(0)
const cashFlows = ref([])
const exchangeShow = ref(false)
const exchangePoints = ref('')
const exchanging = ref(false)
const spendShow = ref(false)
const spendYuanStr = ref('')
const spendNote = ref('')
const spending = ref(false)

const exchangeYuan = computed(() => ((parseInt(exchangePoints.value) || 0) / 10).toFixed(2))
const remaining = computed(() => me.value.balance - (parseInt(exchangePoints.value) || 0))
const spendAfter = computed(() => cashBalance.value / 100 - (parseFloat(spendYuanStr.value) || 0))

async function load() {
  const [m, b, t, c, guide] = await Promise.all([api.get('/me'), api.get('/boxes'), api.get('/tags'), api.get('/cash'), api.get('/items/guide')])
  me.value = m
  boxes.value = b.items || []
  tags.value = t.items || []
  cashBalance.value = c.balance_cents
  cashFlows.value = c.items || []
  itemPool.value = guide.items || []
  initTagSortable()
}

function openExchange() {
  exchangePoints.value = ''
  exchangeShow.value = true
}

async function doExchange() {
  exchanging.value = true
  try {
    const res = await api.post('/cash/exchange', { points: parseInt(exchangePoints.value) })
    cashBalance.value = res.balance_cents
    exchangeShow.value = false
    showToast(`已到账 ¥${(res.cents / 100).toFixed(2)}`)
    load()
  } catch (e) {
    showToast(e)
  } finally {
    exchanging.value = false
  }
}

async function doSpend() {
  spending.value = true
  try {
    const res = await api.post('/cash/spend', {
      cents: Math.round(parseFloat(spendYuanStr.value) * 100),
      note: spendNote.value.trim()
    })
    cashBalance.value = res.balance_cents
    spendShow.value = false
    spendYuanStr.value = ''
    spendNote.value = ''
    showToast('已记录')
    load()
  } catch (e) {
    showToast(e)
  } finally {
    spending.value = false
  }
}

function initTagSortable() {
  if (tagSortable || !tagListEl.value) return
  tagSortable = Sortable.create(tagListEl.value, {
    handle: '.drag-handle',
    animation: 150,
    onEnd: async ({ oldIndex, newIndex }) => {
      if (oldIndex === newIndex) return
      const list = tags.value
      const moved = list.splice(oldIndex, 1)[0]
      list.splice(newIndex, 0, moved)
      try {
        await api.post('/tags/reorder', { ids: list.map((x) => x.id) })
        showToast('顺序已保存')
      } catch (e) {
        showToast(e)
        load()
      }
    }
  })
}
onMounted(load)

async function addTag() {
  try {
    await api.post('/tags', { name: newTagName.value.trim() })
    newTagName.value = ''
    showToast('已创建')
    load()
  } catch (e) {
    showToast(e)
  }
}

function openTagEdit(tg) {
  tagEdit.value = tg
  tagEditName.value = tg.name
  tagEditColor.value = tg.color || '#1989fa'
  tagEditShow.value = true
}

async function saveTagEdit() {
  if (!tagEditName.value.trim()) return showToast('名称不能为空')
  try {
    await api.put(`/tags/${tagEdit.value.id}`, { name: tagEditName.value.trim(), color: tagEditColor.value })
    tagEditShow.value = false
    showToast('已保存，任务自动跟随')
    load()
  } catch (e) {
    showToast(e)
  }
}

async function removeTag(tg) {
  try {
    await showConfirmDialog({
      title: '删除分组',
      message: `删除「${tg.name}」？关联的 ${tg.task_count} 个任务将变为未分组（本身保留）`
    })
  } catch (e) {
    return
  }
  try {
    await api.delete(`/tags/${tg.id}`)
    showToast('已删除')
    load()
  } catch (e) {
    showToast(e)
  }
}

function openBoxForm(b) {
  boxForm.value = b
    ? {
        id: b.id, name: b.name,
        min_points: String(b.min_points), max_points: String(b.max_points),
        item_drops: (b.item_drops || []).map((d) => ({
          item_type: d.item_type, rate: String(d.rate), qty: String(d.qty)
        }))
      }
    : { id: null, name: '', min_points: '', max_points: '', item_drops: [] }
  boxFormShow.value = true
}

async function saveBox() {
  const f = boxForm.value
  const body = {
    name: f.name,
    min_points: parseInt(f.min_points) || 0,
    max_points: parseInt(f.max_points) || 0,
    item_drops: f.item_drops.map((r) => ({
      item_type: r.item_type,
      rate: parseInt(r.rate) || 0,
      qty: parseInt(r.qty) || 1
    }))
  }
  try {
    if (f.id) await api.put(`/boxes/${f.id}`, body)
    else await api.post('/boxes', body)
    boxFormShow.value = false
    showToast('已保存')
    load()
  } catch (e) {
    showToast(e)
  }
}

async function removeBox(b) {
  try {
    await showConfirmDialog({ title: '删除宝箱', message: `删除「${b.name}」后，已关联它的任务会自动解除关联` })
  } catch (e) {
    return
  }
  try {
    await api.delete(`/boxes/${b.id}`)
    showToast('已删除')
    load()
  } catch (e) {
    showToast(e)
  }
}
</script>

<style scoped>
.me-card {
  background: linear-gradient(135deg, #4d9dff, #1989fa);
  border-radius: 16px;
  color: #fff;
  padding: 24px 20px 18px;
  text-align: center;
}
.me-balance { font-size: 46px; font-weight: 700; line-height: 1.2; }
.me-label { font-size: 13px; opacity: 0.85; }
.me-grid { display: flex; margin-top: 16px; border-top: 1px solid rgba(255,255,255,0.25); padding-top: 12px; }
.me-cell { flex: 1; display: flex; flex-direction: column; gap: 2px; }
.me-cell b { font-size: 17px; }
.me-cell span { font-size: 11px; opacity: 0.85; }
.section-title { margin: 18px 4px 10px; font-size: 14px; font-weight: 600; color: #323233; }
.box-card {
  background: #fff;
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.box-emoji { font-size: 26px; }
.tag-dot { display: block; width: 16px; height: 16px; border-radius: 5px; }
.color-label { font-size: 14px; color: #646566; margin: 14px 20px 0; }
.color-grid { display: flex; flex-wrap: wrap; gap: 14px; padding: 12px 22px 0; }
.color-dot { width: 30px; height: 30px; border-radius: 50%; cursor: pointer; box-sizing: border-box; }
.color-dot.active { outline: 2px solid #323233; outline-offset: 2px; }
.box-main { flex: 1; min-width: 0; }
.box-name { font-size: 15px; font-weight: 500; color: #323233; }
.box-range { font-size: 12px; color: #969799; margin-top: 2px; }
.box-actions { display: flex; align-items: center; gap: 8px; }
.del-icon { color: #c8c9cc; font-size: 17px; padding: 4px; }
.add-box-btn { margin-top: 4px; }
.tip { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 14px; padding: 0 20px; }
.drops-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 16px 4px;
  font-size: 12px;
  color: #969799;
}
.drop-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 8px;
}
.drop-row .van-field { flex: 1; padding: 6px 8px; background: #f7f8fa; border-radius: 8px; }
.drop-row .drop-del { font-size: 16px; color: #ee0a24; padding: 0 4px; flex-shrink: 0; }
.drag-handle {
  color: #c8c9cc;
  font-size: 18px;
  padding: 8px 2px;
  cursor: grab;
  touch-action: none;
  user-select: none;
  flex-shrink: 0;
}
.tag-add { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; }
.tag-add-field { flex: 1; background: #fff; border-radius: 10px; padding: 6px 12px; }
.form-title { text-align: center; font-size: 16px; font-weight: 600; margin-bottom: 12px; color: #323233; }
.cash-card { background: #fff; border-radius: 12px; padding: 16px; }
.cash-top { display: flex; justify-content: space-between; align-items: center; }
.cash-balance { font-size: 30px; font-weight: 700; color: #1989fa; line-height: 1.2; }
.cash-label { font-size: 12px; color: #c8c9cc; margin-top: 2px; }
.cash-actions { display: flex; gap: 8px; }
.cash-flows { margin-top: 12px; border-top: 1px solid #f2f3f5; padding-top: 8px; }
.cash-flow-item { display: flex; justify-content: space-between; font-size: 13px; padding: 5px 0; }
.cash-note { color: #646566; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 70%; }
.cash-flow-item .plus { color: #07c160; }
.cash-flow-item .minus { color: #ee0a24; }
.cash-empty { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 12px; }
.cash-more { text-align: center; font-size: 12px; color: #1989fa; padding-top: 8px; cursor: pointer; }
.backpack-entry {
  display: flex;
  align-items: center;
  gap: 10px;
  background: linear-gradient(135deg, #e8f3ff, #f0f7ff);
  border-radius: 12px;
  padding: 12px 16px;
  margin-top: 14px;
  margin-bottom: 16px;
  cursor: pointer;
}
.be-icon { font-size: 22px; }
.be-name { font-size: 15px; font-weight: 600; color: #323233; }
.be-sub { font-size: 12px; color: #1989fa; margin-left: 2px; }
.be-arrow { margin-left: auto; color: #c8c9cc; }
.form-title { text-align: center; font-size: 16px; font-weight: 600; margin-bottom: 12px; color: #323233; }
.ex-summary {
  margin: 14px 16px 0;
  padding: 10px 16px;
  background: #f7f8fa;
  border-radius: 10px;
}
.ex-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 5px 0;
  font-size: 14px;
  color: #646566;
}
.ex-val { font-weight: 600; color: #323233; }
.ex-val.money { color: #07c160; font-size: 17px; }
.ex-val.over { color: #ee0a24; }
</style>
