DROP TABLE IF EXISTS server_plugin_kv;
ALTER TABLE server_plugins
    DROP COLUMN wasm_hash,
    DROP COLUMN wasm_data;
