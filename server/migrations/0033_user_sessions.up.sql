-- 0033: user_sessions 登录会话表
--
-- 多端接入（Web / Tauri 桌面 / Flutter 移动）需要会话列表与单设备撤销。
-- JWT 无状态且只有 uid/tv，此前只能全局 logout-others。每个 JWT 的 sid
-- 对应一行；只存 sid 不存 token 本身，数据库泄露也无法伪造 JWT。
-- 存量 token 没有 sid：首次经 Auth 时按 uid 归一化出稳定 sid（见 middleware），
-- 保证升级后老登录态仍可用且能出现在会话列表里。
CREATE TABLE IF NOT EXISTS user_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    sid VARCHAR(64) NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT 'web',
    client_type VARCHAR(32) NOT NULL DEFAULT '',
    app_version VARCHAR(32) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL,
    last_seen_at DATETIME(3) NOT NULL,
    revoked_at DATETIME(3) NULL DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_sessions_sid (sid),
    KEY idx_user_sessions_user (user_id, last_seen_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
