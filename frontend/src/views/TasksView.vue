<template>
  <div class="page">
    <van-tabs v-model:active="filter" sticky>
      <van-tab title="进行中" />
      <van-tab title="待办" />
      <van-tab title="已完成" />
    </van-tabs>

    <!-- 分组筛选 + 排序 -->
    <div class="group-chips">
      <span
        v-for="g in groupOptions" :key="g.value"
        class="chip" :class="{ active: filterGroup === g.value }"
        @click="filterGroup = g.value"
      >{{ g.text }}</span>
      <span class="chip sort-chip" :class="{ active: sortByDue }" @click="sortByDue = !sortByDue">
        ⇅ 截止期优先
      </span>
    </div>

    <div ref="listEl" class="task-list">
      <div v-for="t in filtered" :key="t.id" class="task-card" :class="{ done: t.status === 'done' }" :data-tag="t.tag_id || 0">
        <div v-if="t.status !== 'done'" class="drag-handle">☰</div>
        <div class="task-main">
          <div class="task-title">
            {{ t.title }}
            <span v-if="t.repeat !== 'once'" class="repeat-tag">{{ t.repeat === 'daily' ? '每天' : '每周' }}</span>
          </div>
          <div class="task-meta">
            <span class="pts">+{{ t.points }} 分</span>
            <span v-if="t.tag_name" class="meta-item group-tag">{{ t.tag_name }}</span>
            <span v-if="t.box_id" class="meta-item box-tag">🎁 {{ boxName(t.box_id) }} · {{ t.box_drop_rate }}%</span>
            <span v-if="t.penalty" class="meta-item penalty-tag">未完成 -{{ t.penalty }}</span>
            <span v-if="t.multi_round && t.rounds_today" class="meta-item round-tag">
              🔁 {{ t.repeat === 'weekly' ? '本周' : '今日' }} {{ t.rounds_today }} 轮
            </span>
          </div>
          <div class="task-bottom">
            <span v-if="dueInfo(t)" class="due-tag" :class="dueInfo(t).cls">📅 {{ dueInfo(t).text }}</span>
            <span v-if="t.status === 'done' && t.completed_at" class="task-time">
              {{ fmtTime(t.completed_at) }} 完成
            </span>
          </div>
        </div>
        <div class="task-actions">
          <template v-if="t.status !== 'done'">
            <van-button
              v-if="t.status !== 'doing' && t.repeat === 'once'" size="small" plain type="primary" round
              @click="start(t)"
            >开始</van-button>
            <van-button size="small" type="primary" round :loading="completingId === t.id" @click="complete(t)">
              完成
            </van-button>
          </template>
          <van-button v-else size="small" plain round disabled>
            {{ t.repeat !== 'once' ? '今日已领' : '已完成' }}
          </van-button>
          <van-icon name="edit" class="del-icon" @click="openForm(t)" />
          <van-icon name="delete-o" class="del-icon" @click="remove(t)" />
        </div>
      </div>
      <van-empty v-if="!filtered.length" description="还没有任务，点右下角 + 新建" />
    </div>
    <div class="drag-tip" v-if="filter !== 2 && filtered.length > 1">按住 ☰ 上下拖动可调整顺序</div>

    <div class="fab" @click="openForm()">+</div>

    <!-- 新建/编辑任务 -->
    <van-popup v-model:show="formShow" round position="bottom" style="padding: 20px 16px 28px">
      <div class="form-title">{{ form.id ? '编辑任务' : '新建任务' }}</div>
      <van-form @submit="saveTask">
        <van-cell-group inset>
          <van-field
            v-model="form.title" label="名称" placeholder="做完五道选择题"
            :rules="[{ required: true, message: '请填写任务名称' }]"
          />
          <van-field v-model="form.points" type="digit" label="奖励积分" placeholder="100" required>
            <template #button><span class="pts-hint">分</span></template>
          </van-field>
          <van-field label="分组" is-link readonly :model-value="tagLabel" @click="pickTag = true" />
          <van-field label="重复" is-link readonly :model-value="repeatLabel" @click="pickRepeat = true" />
          <van-field v-if="form.repeat !== 'once'" label="可多轮领取">
            <template #input>
              <van-switch v-model="form.multi_round" size="22" />
            </template>
          </van-field>
          <van-field
            label="截止日期" readonly :model-value="dueLabel"
            :right-icon="form.due_at ? 'clear' : 'arrow'" @click="onDueFieldClick" @click-right-icon.stop="clearDue"
          />
          <van-field v-model="form.penalty" type="digit" label="未完成罚分" placeholder="0（未完成自动扣分）" />
          <van-field label="关联宝箱" is-link readonly :model-value="boxLabel" @click="pickBox = true" />
          <van-field v-if="form.box_id" v-model="form.box_drop_rate" type="digit" label="掉率 %" placeholder="30（1-100）" />
        </van-cell-group>
        <div style="margin: 8px 16px 0">
          <van-button round block type="primary" native-type="submit">保存</van-button>
        </div>
      </van-form>
    </van-popup>

    <!-- 分组选择 -->
    <van-popup v-model:show="pickTag" round position="bottom">
      <van-picker
        :columns="tagColumns"
        @confirm="onPickTag"
        @cancel="pickTag = false"
        title="选择分组（管理入口在「我的」页）"
      />
    </van-popup>

    <!-- 重复选择 -->
    <van-popup v-model:show="pickRepeat" round position="bottom">
      <van-picker
        :columns="repeatColumns"
        @confirm="onPickRepeat"
        @cancel="pickRepeat = false"
        title="任务类型"
      />
    </van-popup>

    <!-- 截止日期选择 -->
    <van-popup v-model:show="pickDue" round position="bottom">
      <van-date-picker
        v-model="duePickerValue"
        :min-date="dueMinDate"
        :max-date="dueMaxDate"
        title="选择截止日期"
        @confirm="onPickDue"
        @cancel="pickDue = false"
      />
    </van-popup>

    <!-- 宝箱选择 -->
    <van-popup v-model:show="pickBox" round position="bottom">
      <van-picker
        :columns="boxColumns"
        @confirm="onPickBox"
        @cancel="pickBox = false"
        title="选择宝箱（可返回不关联）"
      />
    </van-popup>

    <RewardOverlay :result="rewardResult" @close="onRewardClose" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { showToast, showConfirmDialog } from 'vant'
import Sortable from 'sortablejs'
import api from '../api'
import RewardOverlay from '../components/RewardOverlay.vue'
import { primeAudio } from '../sound'

const tasks = ref([])
const boxes = ref([])
const tags = ref([])
const filter = ref(0) // 0=进行中(含doing) 1=待办 2=已完成
const filterGroup = ref('__all__')
const sortByDue = ref(false)
const formShow = ref(false)
const pickBox = ref(false)
const pickRepeat = ref(false)
const pickDue = ref(false)
const completingId = ref(null)
const rewardResult = ref(null)

const emptyForm = { id: null, title: '', points: '', tag_id: null, repeat: 'once', due_at: '', penalty: '', box_id: null, box_drop_rate: '', multi_round: false }
const form = ref({ ...emptyForm })

const statusMap = { doing: 0, pending: 1, done: 2 }
const filtered = computed(() => {
  const list = tasks.value.filter((t) => {
    if (filter.value === 0 ? t.status !== 'doing' : statusMap[t.status] !== filter.value) return false
    if (filterGroup.value === '__all__') return true
    if (filterGroup.value === '__none__') return !t.tag_name
    return t.tag_name === filterGroup.value
  })
  // 已完成 Tab：后端已按完成时间倒序；进行中/待办可按截止期排序
  if (filter.value === 2 || !sortByDue.value) return list
  return [...list].sort((a, b) => {
    if (!a.due_at && !b.due_at) return 0
    if (!a.due_at) return 1
    if (!b.due_at) return -1
    return new Date(a.due_at) - new Date(b.due_at)
  })
})

const listEl = ref(null)
let sortable = null

function initSortable() {
  if (sortable || !listEl.value) return
  sortable = Sortable.create(listEl.value, {
    handle: '.drag-handle',
    animation: 150,
    // 列表按分组显示：只允许组内拖动，跨组落点会被分组规则弹回，直接禁止
    onMove: (evt) => !evt.related || evt.dragged.dataset.tag === evt.related.dataset.tag,
    onEnd: async ({ oldIndex, newIndex }) => {
      if (oldIndex === newIndex) return
      const list = filtered.value
      const moved = list.splice(oldIndex, 1)[0]
      list.splice(newIndex, 0, moved)
      // sort_order 是全局字段：把当前 Tab 的新顺序合并回全局序列，
      // 全量非完成任务一起发后端编号，避免两个 Tab 各写一套 1..N 互相撞车
      const visible = new Set(list.map((x) => x.id))
      let k = 0
      const merged = tasks.value
        .filter((t) => t.status !== 'done')
        .map((t) => (visible.has(t.id) ? list[k++] : t))
      try {
        await api.post('/tasks/reorder', { ids: merged.map((x) => x.id) })
        showToast('顺序已保存')
      } catch (e) {
        showToast(e)
      }
      load()
    }
  })
}

const groupOptions = computed(() => {
  const opts = [{ text: '全部分组', value: '__all__' }]
  // 与列表显示顺序一致：未分组排最前，其后按分组管理顺序
  if (tasks.value.some((t) => !t.tag_name)) opts.push({ text: '未分组', value: '__none__' })
  tags.value.forEach((t) => opts.push({ text: t.name, value: t.name }))
  return opts
})

const pickTag = ref(false)
const tagColumns = computed(() => [
  { text: '不分组', value: null },
  ...tags.value.map((t) => ({ text: t.name, value: t.id }))
])
const tagLabel = computed(() => {
  const tg = tags.value.find((x) => x.id === form.value.tag_id)
  return tg ? tg.name : '不分组'
})
function onPickTag({ selectedOptions }) {
  form.value.tag_id = selectedOptions[0].value
  pickTag.value = false
}

const repeatColumns = [
  { text: '一次性任务', value: 'once' },
  { text: '每天重复', value: 'daily' },
  { text: '每周重复', value: 'weekly' }
]
const repeatLabel = computed(() => (repeatColumns.find((r) => r.value === form.value.repeat) || repeatColumns[0]).text)

const dueMinDate = new Date(2024, 0, 1)
const dueMaxDate = new Date(new Date().getFullYear() + 5, 11, 31)
const duePickerValue = ref([])
const dueLabel = computed(() => (form.value.due_at ? form.value.due_at.slice(0, 10).replaceAll('-', '/') : ''))

function onDueFieldClick() {
  const base = form.value.due_at ? new Date(form.value.due_at) : new Date()
  duePickerValue.value = [base.getFullYear(), base.getMonth() + 1, base.getDate()]
  pickDue.value = true
}

function onPickDue({ selectedValues }) {
  const [y, m, d] = selectedValues
  form.value.due_at = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}T23:59:59`
  pickDue.value = false
}

function clearDue() {
  form.value.due_at = ''
}

const boxColumns = computed(() => [
  { text: '不关联宝箱', value: null },
  ...boxes.value.map((b) => ({ text: `${b.name}（${b.min_points}~${b.max_points}分）`, value: b.id }))
])
const boxLabel = computed(() => {
  const b = boxes.value.find((x) => x.id === form.value.box_id)
  return b ? `${b.name}（${b.min_points}~${b.max_points}分）` : '不关联宝箱'
})
const boxName = (id) => (boxes.value.find((x) => x.id === id) || {}).name || '宝箱'

function dueInfo(t) {
  if (!t.due_at) return null
  const d = new Date(t.due_at)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const dd = new Date(d)
  dd.setHours(0, 0, 0, 0)
  const days = Math.round((dd - today) / 86400000)
  const dateStr = `${d.getMonth() + 1}/${d.getDate()}`
  if (t.status === 'done') return { text: dateStr, cls: 'dim' }
  if (days < 0) return { text: `逾期 ${-days} 天`, cls: 'overdue' }
  if (days === 0) return { text: '今天截止', cls: 'today' }
  return { text: `剩 ${days} 天`, cls: '' }
}

async function load() {
  const [t, b, tg] = await Promise.all([api.get('/tasks'), api.get('/boxes'), api.get('/tags')])
  tasks.value = t.items || []
  boxes.value = b.items || []
  tags.value = tg.items || []
}

onMounted(() => {
  load()
  initSortable()
})

function openForm(task) {
  form.value = task
    ? {
        id: task.id, title: task.title, points: String(task.points),
        tag_id: task.tag_id || null, repeat: task.repeat || 'once',
        // 后端给 UTC(Z) 格式，必须先转本地时间分量再回填，否则 slice 后
        // 被浏览器按本地解析，每保存一次截止时间漂移 8 小时
        due_at: (() => {
          if (!task.due_at) return ''
          const d = new Date(task.due_at)
          const p = (n) => String(n).padStart(2, '0')
          return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
        })(),
        penalty: task.penalty ? String(task.penalty) : '',
        box_id: task.box_id, box_drop_rate: task.box_drop_rate ? String(task.box_drop_rate) : '',
        multi_round: !!task.multi_round
      }
    : { ...emptyForm }
  formShow.value = true
}

function onPickRepeat({ selectedOptions }) {
  form.value.repeat = selectedOptions[0].value
  pickRepeat.value = false
}

function onPickBox({ selectedOptions }) {
  form.value.box_id = selectedOptions[0].value
  pickBox.value = false
}

async function saveTask() {
  const f = form.value
  const body = {
    title: f.title,
    points: parseInt(f.points) || 0,
    tag_id: f.tag_id || null,
    repeat: f.repeat,
    due_at: f.due_at ? new Date(f.due_at).toISOString() : null,
    penalty: parseInt(f.penalty) || 0,
    box_id: f.box_id || null,
    box_drop_rate: f.box_id ? parseInt(f.box_drop_rate) || 0 : 0,
    multi_round: !!f.multi_round
  }
  try {
    if (f.id) await api.put(`/tasks/${f.id}`, body)
    else await api.post('/tasks', body)
    formShow.value = false
    showToast('已保存')
    load()
  } catch (e) {
    showToast(e)
  }
}

async function start(t) {
  try {
    await api.post(`/tasks/${t.id}/start`)
    t.status = 'doing'
  } catch (e) {
    showToast(e)
  }
}

async function complete(t) {
  try {
    await showConfirmDialog({ title: '确认完成？', message: `「${t.title}」完成后 ${t.points} 分秒到账` })
  } catch (e) {
    return
  }
  primeAudio() // iOS 音效解锁要在用户手势链内
  completingId.value = t.id
  try {
    const res = await api.post(`/tasks/${t.id}/complete`)
    rewardResult.value = res
    load()
  } catch (e) {
    showToast(e)
  } finally {
    completingId.value = null
  }
}

function onRewardClose() {
  rewardResult.value = null
}

async function remove(t) {
  try {
    await showConfirmDialog({ title: '删除任务', message: `确定删除「${t.title}」？` })
  } catch (e) {
    return
  }
  try {
    await api.delete(`/tasks/${t.id}`)
    load()
  } catch (e) {
    showToast(e)
  }
}

function fmtTime(s) {
  const d = new Date(s)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
</script>

<style scoped>
/* 底部留出 FAB 高度，滚动到底时最后一张卡不被遮挡 */
.page {
  padding-bottom: 160px;
}
.group-chips {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding: 10px 12px 2px;
  -webkit-overflow-scrolling: touch;
}
.group-chips::-webkit-scrollbar { display: none; }
.chip {
  flex-shrink: 0;
  padding: 4px 12px;
  border-radius: 14px;
  background: #fff;
  color: #646566;
  font-size: 12px;
  border: 1px solid #ebedf0;
}
.chip.active {
  background: #1989fa;
  color: #fff;
  border-color: #1989fa;
}
.sort-chip { margin-left: auto; }
.task-list { margin-top: 10px; }
.drag-handle {
  color: #c8c9cc;
  font-size: 18px;
  padding: 8px 2px;
  cursor: grab;
  touch-action: none;
  user-select: none;
  flex-shrink: 0;
}
.drag-tip { text-align: center; font-size: 12px; color: #c8c9cc; margin-top: 6px; }
.task-card {
  background: #fff;
  border-radius: 12px;
  padding: 14px;
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.task-card.done { opacity: 0.55; }
.task-main { flex: 1; min-width: 0; }
.task-title { font-size: 15px; font-weight: 500; color: #323233; }
.repeat-tag {
  font-size: 11px;
  color: #1989fa;
  background: #e8f3ff;
  border-radius: 4px;
  padding: 1px 5px;
  margin-left: 4px;
  font-weight: 400;
}
.task-meta { margin-top: 4px; display: flex; flex-wrap: wrap; gap: 8px; font-size: 12px; color: #969799; }
.pts { color: #1989fa; font-weight: 600; }
.group-tag {
  color: #1989fa;
  background: #e8f3ff;
  border-radius: 4px;
  padding: 0 5px;
}
.penalty-tag { color: #ee0a24; background: #fcebeb; border-radius: 4px; padding: 0 5px; }
.round-tag { color: #07c160; }
.box-tag { color: #ff9900; }
.task-bottom { margin-top: 4px; display: flex; gap: 10px; align-items: center; }
.due-tag { font-size: 12px; color: #646566; }
.due-tag.overdue { color: #ee0a24; font-weight: 600; }
.due-tag.today { color: #ff976a; font-weight: 600; }
.due-tag.dim { color: #c8c9cc; }
.task-time { font-size: 12px; color: #c8c9cc; }
.task-actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.del-icon { color: #c8c9cc; font-size: 17px; padding: 4px; }
.fab {
  position: fixed;
  right: 20px;
  bottom: 76px;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: linear-gradient(135deg, #4d9dff, #1989fa);
  color: #fff;
  font-size: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 6px 16px rgba(25, 137, 250, 0.35);
  z-index: 100;
  cursor: pointer;
}
.form-title { text-align: center; font-size: 16px; font-weight: 600; margin-bottom: 12px; color: #323233; }
.pts-hint { color: #969799; font-size: 12px; }
</style>
