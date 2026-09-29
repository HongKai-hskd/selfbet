<template>
  <div class="page">
    <van-nav-bar title="日程" left-arrow @click-left="$router.back()" />

    <div class="toolbar">
      <van-button size="mini" round plain type="primary" @click="goToday">今天</van-button>
      <van-icon name="arrow-left" class="nav-ic" @click="shift(-1)" />
      <van-icon name="arrow" class="nav-ic" @click="shift(1)" />
      <span class="range-title">{{ rangeTitle }}</span>
      <div class="view-tabs">
        <span class="chip" :class="{ active: view === 'week' }" @click="setView('week')">周</span>
        <span class="chip" :class="{ active: view === 'day' }" @click="setView('day')">日</span>
      </div>
    </div>

    <div class="tip" v-if="!scheduledTasks.length">在任务的「计划开始/结束」里填好时间，日程会显示在这里</div>

    <div class="grid-wrap">
      <div class="grid-head">
        <div class="gutter-head"></div>
        <div v-for="d in days" :key="d.getTime()" class="day-head" :class="{ today: isToday(d) }">
          <div class="dow">{{ '周' + '一二三四五六日'[(d.getDay() + 6) % 7] }}</div>
          <div class="dom">{{ d.getMonth() + 1 }}/{{ d.getDate() }}</div>
        </div>
      </div>
      <div class="grid-body" ref="scrollEl">
        <div class="grid-inner">
          <div class="gutter">
            <div v-for="h in 24" :key="h" class="hour-label">{{ h - 1 }}:00</div>
          </div>
          <div class="grid-main">
            <div class="hour-lines">
              <div v-for="h in 25" :key="h" class="hour-line" :style="{ top: (h - 1) * 48 + 'px' }" />
            </div>
            <div class="day-cols">
              <div v-for="(d, di) in days" :key="d.getTime()" class="day-col" :class="{ today: isToday(d) }">
                <div
                  v-for="b in blocksByCol[di] || []"
                  :key="b.key"
                  class="block"
                  :style="blockStyle(b)"
                >
                  <div class="b-name">{{ b.title }}</div>
                  <div class="b-time">{{ b.timeText }}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import api from '../api'

const HOUR_H = 48 // 每小时像素高
const tasks = ref([])
const tags = ref([])
const anchor = ref(startOfDay(new Date()))
const view = ref('week')
const scrollEl = ref(null)

function startOfDay(d) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

// 可见范围：周 = 锚点所在周一开始的 7 天；日 = 锚点当天
const days = computed(() => {
  if (view.value === 'day') return [startOfDay(anchor.value)]
  const monday = startOfDay(anchor.value)
  monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7))
  return Array.from({ length: 7 }, (_, i) => {
    const d = new Date(monday)
    d.setDate(d.getDate() + i)
    return d
  })
})

const rangeTitle = computed(() => {
  const f = (d) => `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`
  const ds = days.value
  if (view.value === 'day') {
    return `${f(ds[0])} 周${'一二三四五六日'[(ds[0].getDay() + 6) % 7]}`
  }
  return `${f(ds[0])} - ${f(ds[6])}`
})

function shift(n) {
  const d = new Date(anchor.value)
  d.setDate(d.getDate() + n * (view.value === 'week' ? 7 : 1))
  anchor.value = startOfDay(d)
}

function goToday() {
  anchor.value = startOfDay(new Date())
}

function setView(v) {
  view.value = v
}

function isToday(d) {
  return d.getTime() === startOfDay(new Date()).getTime()
}

// ---- 数据 ----

async function load() {
  const [ts, tg] = await Promise.all([api.get('/tasks'), api.get('/tags')])
  tasks.value = ts.items || []
  tags.value = tg.items || []
}

const scheduledTasks = computed(() =>
  (tasks.value || []).filter((t) => {
    if (t.status === 'done') return false // 已完成隐藏
    if (t.repeat === 'once') return t.expected_start_at && t.expected_end_at
    return t.expected_start_time && t.expected_end_time && (t.repeat === 'daily' || t.expected_weekday)
  })
)

function tagColor(t) {
  if (!t.tag_name) return '#1989fa'
  const tg = tags.value.find((x) => x.name === t.tag_name)
  return (tg && tg.color) || '#1989fa'
}

const hm = (s) => {
  const [h, m] = s.split(':').map(Number)
  return h * 60 + m
}
const minutesOfDay = (d) => d.getHours() * 60 + d.getMinutes()
const fmtMin = (v) => `${String(Math.floor(v / 60)).padStart(2, '0')}:${String(v % 60).padStart(2, '0')}`

// 按可见范围生成色块：once 按日期落列；daily 每天一列；weekly 按 weekday 匹配
const blocks = computed(() => {
  const out = []
  days.value.forEach((d, di) => {
    const dayStart = new Date(d.getFullYear(), d.getMonth(), d.getDate())
    const dayEnd = new Date(dayStart.getTime() + 86400000)
    for (const t of scheduledTasks.value) {
      const color = tagColor(t)
      if (t.repeat === 'once') {
        const s = new Date(t.expected_start_at)
        const e = new Date(t.expected_end_at)
        if (s >= dayStart && s < dayEnd) {
          out.push({ key: `${t.id}-o-${di}`, di, title: t.title, color, startMin: minutesOfDay(s), endMin: minutesOfDay(e), timeText: `${fmtMin(minutesOfDay(s))}~${fmtMin(minutesOfDay(e))}` })
        }
      } else if (t.repeat === 'daily') {
        out.push({ key: `${t.id}-d-${di}`, di, title: t.title, color, startMin: hm(t.expected_start_time), endMin: hm(t.expected_end_time), timeText: `${t.expected_start_time}~${t.expected_end_time}` })
      } else {
        const iso = ((d.getDay() + 6) % 7) + 1 // 1=周一..7=周日
        if (iso === t.expected_weekday) {
          out.push({ key: `${t.id}-w-${di}`, di, title: t.title, color, startMin: hm(t.expected_start_time), endMin: hm(t.expected_end_time), timeText: `${t.expected_start_time}~${t.expected_end_time}` })
        }
      }
    }
  })
  // 同一天内的重叠分栏（贪心车道分配）
  const byDay = {}
  out.forEach((b) => (byDay[b.di] = byDay[b.di] || []).push(b))
  Object.values(byDay).forEach((arr) => {
    arr.sort((a, b) => a.startMin - b.startMin || b.endMin - a.endMin)
    const laneEnds = []
    arr.forEach((b) => {
      let lane = laneEnds.findIndex((end) => end <= b.startMin)
      if (lane === -1) {
        lane = laneEnds.length
        laneEnds.push(0)
      }
      laneEnds[lane] = b.endMin
      b.lane = lane
    })
    const lanes = Math.max(1, laneEnds.length)
    arr.forEach((b) => (b.lanes = lanes))
  })
  return out
})

const blocksByCol = computed(() => {
  const cols = days.value.map(() => [])
  blocks.value.forEach((b) => cols[b.di].push(b))
  return cols
})

function blockStyle(b) {
  const c = b.color
  return {
    background: c + '2b',
    color: c,
    borderLeft: '3px solid ' + c,
    top: `calc(${(b.startMin / 1440) * 100}% + 1px)`,
    height: `calc(${((b.endMin - b.startMin) / 1440) * 100}% - 2px)`,
    left: `calc(${(b.lane * 100) / b.lanes}% + 1px)`,
    width: `calc(${100 / b.lanes}% - 2px)`
  }
}

onMounted(async () => {
  await load()
  nextTick(() => {
    // 打开时滚到 7:00 附近（清晨时段少，直接看白天）
    if (scrollEl.value) scrollEl.value.scrollTop = 7 * HOUR_H
  })
})
</script>

<style scoped>
.page { min-height: 100vh; background: #f7f8fa; }
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: #fff;
}
.nav-ic { font-size: 16px; color: #646566; cursor: pointer; padding: 4px; }
.range-title { flex: 1; font-size: 13px; font-weight: 600; color: #323233; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.view-tabs { display: flex; gap: 0; background: #f2f3f5; border-radius: 8px; padding: 2px; }
.view-tabs .chip { padding: 3px 12px; border-radius: 6px; font-size: 12px; color: #646566; cursor: pointer; }
.view-tabs .chip.active { background: #1989fa; color: #fff; }
.tip { text-align: center; font-size: 12px; color: #c8c9cc; padding: 10px 20px 0; }
.grid-wrap { margin: 10px 12px; background: #fff; border-radius: 12px; overflow: hidden; }
.grid-head { display: flex; border-bottom: 1px solid #f2f3f5; }
.gutter-head { width: 44px; flex-shrink: 0; }
.day-head { flex: 1; text-align: center; padding: 6px 0; border-left: 1px solid #f7f8fa; }
.day-head .dow { font-size: 11px; color: #969799; }
.day-head .dom { font-size: 15px; font-weight: 600; color: #323233; }
.day-head.today .dow { color: #1989fa; }
.day-head.today .dom { color: #1989fa; }
.grid-body { height: 480px; overflow-y: auto; }
.grid-inner { display: flex; height: calc(24 * 48px); } /* 24h 完整内容层，色块百分比相对它定位 */
.gutter { width: 44px; flex-shrink: 0; }
.hour-label {
  height: 48px;
  font-size: 10px;
  color: #c8c9cc;
  text-align: right;
  padding-right: 6px;
  transform: translateY(-6px);
}
.grid-main { flex: 1; position: relative; }
.hour-lines { position: absolute; inset: 0; pointer-events: none; }.hour-line { position: absolute; left: 0; right: 0; border-top: 1px solid #f2f3f5; }
.day-cols { display: flex; position: absolute; inset: 0; }
.day-col { flex: 1; position: relative; border-left: 1px solid #f7f8fa; }
.day-col.today { background: #fafcff; }
.block {
  position: absolute;
  box-sizing: border-box; /* 宽高含边框内边距，绝不溢出列宽 */
  border-radius: 6px;
  padding: 3px 5px;
  overflow: hidden;
  cursor: default;
}
.b-name { font-size: 11px; font-weight: 600; line-height: 1.35; word-break: break-all; }
.b-time { font-size: 9px; opacity: 0.85; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
