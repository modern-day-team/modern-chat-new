-- 0035: 插件扩展表
--
-- plugin_user_data 存用户插件的服务端私有 KV（value 由接口限制 64KB）。
-- server_plugins   存管理员安装的服务端声明式插件（manifest 原文 + 启停状态）。
-- 开发模式由 store.AutoMigrate 建同名同构表。
CREATE TABLE IF NOT EXISTS plugin_user_data (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    plugin_id BIGINT UNSIGNED NOT NULL,
    `key` VARCHAR(120) NOT NULL,
    `value` MEDIUMTEXT NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_plugin_user_data (user_id, plugin_id, `key`),
    KEY idx_plugin_user_data_plugin (plugin_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS server_plugins (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(30) NOT NULL,
    version VARCHAR(16) NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    manifest_json TEXT NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    installed_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_server_plugins_name (name),
    KEY idx_server_plugins_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
