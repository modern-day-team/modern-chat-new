/**
 * 沙箱引导文档的回归测试：CSP 收敛、插件代码转义、postMessage 目标 origin。
 * bootHtml 生成的 HTML 会被浏览器直接执行，这里的每条断言都是安全边界。
 */
import { describe, expect, it } from 'vitest'

import { bootHtml } from './sandbox'

describe('bootHtml CSP', () => {
  it('connect-src / media-src 只收敛到 allowedHosts', () => {
    const html = bootHtml('', ['example.com', '*.cdn.example'], 'tk', 'https://app.example')
    expect(html).toContain('connect-src example.com *.cdn.example')
    expect(html).toContain('media-src example.com *.cdn.example blob:')
    expect(html).toContain("default-src 'none'")
    expect(html).toContain('script-src \'unsafe-inline\'')
  })

  it('未声明 allowedHosts 时完全禁止出网与媒体', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    expect(html).toContain("connect-src 'none'")
    expect(html).toContain("media-src 'none'")
  })

  it('非法 host 形态（带协议/路径）被过滤，不进 CSP', () => {
    const html = bootHtml('', ['https://evil.com/path', 'ok.example'], 'tk', 'https://app.example')
    expect(html).toContain('connect-src ok.example')
    expect(html).not.toContain('evil.com')
  })
})

describe('bootHtml 转义与注入', () => {
  it('插件代码里的 </script> 被转义，无法提前闭合引导脚本', () => {
    const html = bootHtml('var s = "</script><script>alert(1)</script>";', [], 'tk', 'https://app.example')
    // 逃逸后不会出现裸的 </script>（文档末尾的那个除外）
    expect(html).not.toContain('</script><script>alert(1)')
    expect(html).toContain('<\\/script>')
  })

  it('身份令牌与宿主 origin 被注入', () => {
    const html = bootHtml('', ['ok.example'], 'tok-abc', 'https://app.example')
    expect(html).toContain('var TK = "tok-abc";')
    expect(html).toContain('var HOST = "https://app.example";')
    expect(html).toContain('parent.postMessage(m, HOST)')
    // 沙箱 → 宿主不再使用 "*"
    expect(html).not.toContain('postMessage(m, "*")')
  })

  it('origin 形态非法时回退 "*"（不产生静默失败）', () => {
    const html = bootHtml('', [], 'tk', 'not-a-origin')
    expect(html).toContain('var HOST = "*";')
  })

  it('SDK v5 暴露会话/消息写操作、账号关系与文件上传，且保留 v2-v4 能力', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    expect(html).toContain('version: "5"')
    expect(html).toContain('removeSidePanel')
    expect(html).toContain('call("users.search", [kw])')
    expect(html).toContain('call("conv.list", [])')
    expect(html).toContain('call("messages.search", [kw, opts || {}])')
    expect(html).toContain('call("draft.get", [])')
    expect(html).toContain('call("draft.set", [text])')
    expect(html).toContain('call("clipboard.writeText", [text])')
    expect(html).toContain('call("storage.remove", [k])')
    expect(html).toContain('call("storage.keys", [])')
    // v3 新增
    expect(html).toContain('call("addPage", [desc])')
    expect(html).toContain('call("removePage", [id])')
    expect(html).toContain('call("setPage", [desc])')
    expect(html).toContain('call("data.get", [key])')
    expect(html).toContain('call("data.set", [key, value])')
    expect(html).toContain('call("data.remove", [key])')
    expect(html).toContain('call("data.keys", [])')
    expect(html).toContain('call("nav.openPluginPage", [pluginId, pageId])')
    // v4 新增：服务端统一 AI 代理
    expect(html).toContain('call("ai.status", [])')
    expect(html).toContain('call("ai.chat", [messages || []])')
    // v5 新增：会话写操作
    expect(html).toContain('call("conv.createSingle", [userId])')
    expect(html).toContain('call("conv.createGroup", [name])')
    expect(html).toContain('call("conv.rename", [id, name])')
    expect(html).toContain('call("conv.invite", [id, userIds])')
    expect(html).toContain('call("conv.leave", [id])')
    expect(html).toContain('call("conv.join", [id])')
    expect(html).toContain('call("conv.setPrefs", [id, prefs || {}])')
    expect(html).toContain('call("conv.remove", [id])')
    expect(html).toContain('call("conv.clear", [id, upToId])')
    expect(html).toContain('call("conv.files", [id])')
    expect(html).toContain('call("conv.addFile", [id, file])')
    expect(html).toContain('call("conv.removeFile", [fileId])')
    expect(html).toContain('call("conv.setAnnouncement", [id, content])')
    expect(html).toContain('call("conv.kick", [id, userId])')
    expect(html).toContain('call("conv.setAdmin", [id, userId, admin === true])')
    expect(html).toContain('call("conv.mute", [id, userId, minutes])')
    expect(html).toContain('call("conv.transfer", [id, userId])')
    expect(html).toContain('call("conv.createInviteLink", [id, expiresDays])')
    // v5 新增：消息读写与账号关系、宿主文件上传
    expect(html).toContain('call("message.history", [convId, opts || {}])')
    expect(html).toContain('call("message.delete", [convId, messageId])')
    expect(html).toContain('call("message.recall", [messageId])')
    expect(html).toContain('call("message.edit", [messageId, content])')
    expect(html).toContain('call("message.react", [messageId, emoji, remove === true])')
    expect(html).toContain('call("message.favorite", [messageId, favorite !== false])')
    expect(html).toContain('call("message.report", [messageId, reason])')
    expect(html).toContain('call("users.blockStatus", [userId])')
    expect(html).toContain('call("users.block", [userId])')
    expect(html).toContain('call("users.unblock", [userId])')
    expect(html).toContain('call("file.upload", [kind, dataUrl, filename])')
  })
})
