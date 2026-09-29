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
      ><i
          v-if="g.value !== '__all__' && g.value !== '__none__'"
          class="chip-dot" :style="{ background: tagColor(g.value) }"
        />{{ g.text }}</span>
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
            <span v-if="t.tag_name" class="meta-item group-tag" :style="tagChipStyle(t.tag_name)">{{ t.tag_name }}</span>
            <span v-if="t.box_id" class="meta-item box-tag">🎁 {{ boxName(t.box_id) }} · {{ t.box_drop_rate }}%</span>
            <span
              v-if="t.penalty && t.status !== 'done' && !(t.multi_round && t.rounds_today)"
              class="meta-item penalty-tag"
            >未完成 -{{ t.penalty }}</span>
            <span v-if="timeTag(t)" class="meta-item time-tag">{{ timeTag(t) }}</span>
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
          <template v-if="form.repeat === 'once'">
            <van-field label="预期开始" readonly :model-value="expAtLabel('expected_start_at')" placeholder="选填（上日程表）" :right-icon="form.expected_start_at ? 'clear' : 'arrow'" @click="openExpAt('expected_start_at')" @click-right-icon.stop="form.expected_start_at = ''" />
            <van-field label="预期结束" readonly :model-value="expAtLabel('expected_end_at')" :right-icon="form.expected_end_at ? 'clear' : 'arrow'" @click="openExpAt('expected_end_at')" @click-right-icon.stop="form.expected_end_at = ''" />
          </template>
          <template v-else>
            <van-field v-if="form.repeat === 'weekly'" label="周几" readonly :model-value="weekdayLabel" :right-icon="form.expected_weekday ? 'clear' : 'arrow'" @click="pickWeekday = true" @click-right-icon.stop="form.expected_weekday = 0" />
            <van-field label="计划开始" readonly :model-value="form.expected_start_time" placeholder="如 08:00（上日程表）" :right-icon="form.expected_start_time ? 'clear' : 'arrow'" @click="openExpTime('expected_start_time')" @click-right-icon.stop="form.expected_start_time = ''" />
            <van-field label="计划结束" readonly :model-value="form.expected_end_time" :right-icon="form.expected_end_time ? 'clear' : 'arrow'" @click="openExpTime('expected_end_time')" @click-right-icon.stop="form.expected_end_time = ''" />
          </template>
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

    <!-- 截止日期选择（支持滚轮） -->
    <van-popup v-model:show="pickDue" round position="bottom" @wheel.prevent="onDuePickerWheel">
      <van-date-picker
        v-model="duePickerValue"
        :min-date="dueMinDate"
        :max-date="dueMaxDate"
        title="选择截止日期"
        @confirm="onPickDue"
        @cancel="pickDue = false"
      />
    </van-popup>

    <!-- 日程计划时间选择（日期→时间 两步 / 纯时间，支持滚轮） -->
    <van-popup v-model:show="expPickShow" round position="bottom" @wheel.prevent="onExpPickerWheel">
      <van-date-picker
        v-if="expStep === 'date'"
        v-model="expDateValue"
        :min-date="dueMinDate"
        :max-date="dueMaxDate"
        title="选择日期"
        @confirm="onExpDateConfirm"
        @cancel="expPickShow = false"
      />
      <van-time-picker
        v-else
        v-model="expTimeValue"
        title="选择时间"
        @confirm="onExpTimeConfirm"
        @cancel="expPickShow = false"
      />
    </van-popup>

    <!-- 周几选择（每周任务） -->
    <van-popup v-model:show="pickWeekday" round position="bottom">
      <van-picker :columns="weekdayColumns" title="每周几" @confirm="onPickWeekday" @cancel="pickWeekday = false" />
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

const emptyForm = { id: null, title: '', points: '', tag_id: null, repeat: 'once', due_at: '', penalty: '', box_id: null, box_drop_rate: '', multi_round: false, expected_start_at: '', expected_end_at: '', expected_start_time: '', expected_end_time: '', expected_weekday: 0 }
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

// 分组颜色：卡片小标签 = 分组色文字 + 10% 同色底
function tagColor(name) {
  const tg = tags.value.find((x) => x.name === name)
  return (tg && tg.color) || '#1989fa'
}
function tagChipStyle(name) {
  const c = tagColor(name)
  return { color: c, background: c + '1a' }
}

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

// ---- 日程计划时间选择 ----
const expPickShow = ref(false)
const expStep = ref('date') // date | time
const expPickField = ref('expected_start_at')
const expDateValue = ref([])
const expTimeValue = ref([])
const pickWeekday = ref(false)
const weekdayColumns = ['周一', '周二', '周三', '周四', '周五', '周六', '周日'].map((t, i) => ({ text: t, value: i + 1 }))
const weekdayLabel = computed(() => (form.value.expected_weekday ? '周' + '一二三四五六日'[form.value.expected_weekday - 1] : ''))

function expAtLabel(field) {
  const v = form.value[field]
  if (!v) return ''
  const d = new Date(v)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function openExpAt(field) {
  expPickField.value = field
  // 结束时间缺省继承开始时间（少滑一大截）
  const fallback = field === 'expected_end_at' ? (form.value.expected_start_at || form.value.due_at || '') : ''
  const base = form.value[field] ? new Date(form.value[field]) : (fallback ? new Date(fallback) : new Date())
  expDateValue.value = [String(base.getFullYear()), String(base.getMonth() + 1).padStart(2, '0'), String(base.getDate()).padStart(2, '0')]
  expTimeValue.value = [String(base.getHours()).padStart(2, '0'), String(base.getMinutes()).padStart(2, '0')]
  expStep.value = 'date'
  expPickShow.value = true
}

function onExpDateConfirm({ selectedValues }) {
  expDateValue.value = selectedValues
  expStep.value = 'time' // 同一弹层切到时间步
}

function onExpTimeConfirm({ selectedValues }) {
  const [h, m] = selectedValues
  const field = expPickField.value
  if (field === 'expected_start_at' || field === 'expected_end_at') {
    // 一次性任务：日期 + 时间 组合
    const [y, mo, d] = expDateValue.value
    form.value[field] = `${y}-${String(mo).padStart(2, '0')}-${String(d).padStart(2, '0')}T${h}:${m}:00`
  } else {
    // 每日/每周：纯时间
    form.value[field] = `${h}:${m}`
  }
  expPickShow.value = false
}

function openExpTime(field) {
  expPickField.value = field
  // 结束时间缺省继承开始时间
  const fallback = field === 'expected_end_time' ? (form.value.expected_start_time || '08:00') : '08:00'
  expTimeValue.value = (form.value[field] || fallback).split(':')
  expStep.value = 'time'
  expPickShow.value = true
}

function onPickWeekday({ selectedOptions }) {
  form.value.expected_weekday = selectedOptions[0].value
  pickWeekday.value = false
}

// ---- 选择器滚轮支持：滚一下跳一格（Vant 原生不响应滚轮） ----

function columnUnderCursor(e) {
  const popup = e.currentTarget
  const cols = Array.from(popup.querySelectorAll('.van-picker-column'))
  if (!cols.length) return -1
  const colEl = e.target && e.target.closest ? e.target.closest('.van-picker-column') : null
  return colEl ? cols.indexOf(colEl) : -1
}

const p2 = (n) => String(n).padStart(2, '0')

function stepDateValue(arr, ci, dir) {
  let y = +arr.value[0]
  let m = +arr.value[1]
  let d = +arr.value[2]
  const minYear = dueMinDate.getFullYear()
  const maxYear = dueMaxDate.getFullYear()
  if (ci === 0) {
    y = Math.min(maxYear, Math.max(minYear, y + dir))
  } else if (ci === 1) {
    m += dir
    if (m < 1 || m > 12) return
  } else {
    const nd = new Date(y, m - 1, d + dir)
    y = nd.getFullYear()
    m = nd.getMonth() + 1
    d = nd.getDate()
    if (y < minYear || y > maxYear) return
  }
  arr.value = [String(y), p2(m), p2(d)]
}

function onExpPickerWheel(e) {
  e.preventDefault()
  const ci = columnUnderCursor(e)
  if (ci === -1) return
  const dir = e.deltaY > 0 ? 1 : -1
  if (expStep.value === 'time') {
    const max = ci === 0 ? 23 : 59
    const cur = parseInt(expTimeValue.value[ci] || '0', 10)
    const next = Math.min(max, Math.max(0, cur + dir))
    if (next === cur) return
    expTimeValue.value = expTimeValue.value.map((v, i) => (i === ci ? p2(next) : v))
  } else {
    stepDateValue(expDateValue, ci, dir)
  }
}

function onDuePickerWheel(e) {
  e.preventDefault()
  const ci = columnUnderCursor(e)
  if (ci === -1) return
  stepDateValue(duePickerValue, ci, e.deltaY > 0 ? 1 : -1)
}

// 任务卡时间标签（三种类型各自的展示形状）
function timeTag(t) {
  if (t.repeat === 'once' && t.expected_start_at && t.expected_end_at) {
    const s = new Date(t.expected_start_at)
    const e = new Date(t.expected_end_at)
    const p = (n) => String(n).padStart(2, '0')
    return `⏰ ${s.getMonth() + 1}/${s.getDate()} ${p(s.getHours())}:${p(s.getMinutes())}~${p(e.getHours())}:${p(e.getMinutes())}`
  }
  if (t.repeat === 'daily' && t.expected_start_time && t.expected_end_time) {
    return `⏰ 每天 ${t.expected_start_time}~${t.expected_end_time}`
  }
  if (t.repeat === 'weekly' && t.expected_weekday && t.expected_start_time && t.expected_end_time) {
    return `⏰ 周${'一二三四五六日'[t.expected_weekday - 1]} ${t.expected_start_time}~${t.expected_end_time}`
  }
  return ''
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
        multi_round: !!task.multi_round,
        expected_start_at: task.expected_start_at ? (() => {
          const d = new Date(task.expected_start_at)
          const p = (n) => String(n).padStart(2, '0')
          return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:00`
        })() : '',
        expected_end_at: task.expected_end_at ? (() => {
          const d = new Date(task.expected_end_at)
          const p = (n) => String(n).padStart(2, '0')
          return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:00`
        })() : '',
        expected_start_time: task.expected_start_time || '',
        expected_end_time: task.expected_end_time || '',
        expected_weekday: task.expected_weekday || 0
      }
    : { ...emptyForm }
  formShow.value = true
}

function onPickRepeat({ selectedOptions }) {
  form.value.repeat = selectedOptions[0].value
  // 类型切换：清空不适用的时间字段（三种类型字段形状不同）
  form.value.expected_start_at = ''
  form.value.expected_end_at = ''
  form.value.expected_start_time = ''
  form.value.expected_end_time = ''
  form.value.expected_weekday = 0
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
  // 日程计划字段按类型发送（后端会清空不适用类型）
  if (f.repeat === 'once') {
    body.expected_start_at = f.expected_start_at ? new Date(f.expected_start_at).toISOString() : ''
    body.expected_end_at = f.expected_end_at ? new Date(f.expected_end_at).toISOString() : ''
  } else if (f.repeat === 'daily') {
    body.expected_start_time = f.expected_start_time || ''
    body.expected_end_time = f.expected_end_time || ''
  } else {
    body.expected_start_time = f.expected_start_time || ''
    body.expected_end_time = f.expected_end_time || ''
    body.expected_weekday = f.expected_weekday || 0
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
.chip-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 4px; vertical-align: -1px; }
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
  border-radius: 4px;
  padding: 0 5px;
}
.penalty-tag { color: #ee0a24; background: #fcebeb; border-radius: 4px; padding: 0 5px; }
.time-tag { color: #1989fa; background: #e8f3ff; border-radius: 4px; padding: 0 5px; }
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
