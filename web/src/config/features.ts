/**
 * 功能开关：只隐藏不删码。
 * 置 false 时对应 UI 不渲染（逻辑与样式保留），要重新启用改回 true 即可。
 */
export const FEATURES = {
  /**
   * 表情回应（消息气泡下的 emoji 表态）。
   * 2026-11-26 启用：链路早已实现（WS reaction 帧 + REST 历史聚合 + UI），
   * 此前按设计文档划在二期隐藏；如需临时下线改回 false 即可。
   */
  reactions: true,
} as const
