-- 0036: 服务端插件 WASM 运行时与插件专属 KV
ALTER TABLE server_plugins
    ADD COLUMN wasm_data LONGBLOB NULL,
    ADD COLUMN wasm_hash VARCHAR(64) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS server_plugin_kv (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    plugin_id BIGINT UNSIGNED NOT NULL,
    `key` VARCHAR(120) NOT NULL,
    `value` MEDIUMTEXT NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_server_plugin_kv (plugin_id, `key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
