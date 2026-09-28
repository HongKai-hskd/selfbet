// 简短的到账音效：Web Audio 合成两声上行"叮咚"，无需音频文件。
// 必须在用户点击回调里调用（浏览器自动播放限制）。
let ctx = null

// primeAudio 在用户手势的同步上下文里提前创建/解锁 AudioContext，
// 否则 iOS 上经过 await 异步链后播放会被静默拦截。
export function primeAudio() {
  try {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)()
    if (ctx.state === 'suspended') ctx.resume()
  } catch (e) {
    // 忽略
  }
}

export function playDing() {
  try {
    primeAudio()
    const notes = [880, 1318.5] // A5 → E6
    notes.forEach((freq, i) => {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'sine'
      osc.frequency.value = freq
      const t0 = ctx.currentTime + i * 0.12
      gain.gain.setValueAtTime(0.0001, t0)
      gain.gain.exponentialRampToValueAtTime(0.18, t0 + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.3)
      osc.connect(gain).connect(ctx.destination)
      osc.start(t0)
      osc.stop(t0 + 0.35)
    })
  } catch (e) {
    // 音效失败不影响主流程
  }
}
