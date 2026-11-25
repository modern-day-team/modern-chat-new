-- 回滚 0035：删除插件扩展表（用户插件私有数据随之删除）。
DROP TABLE IF EXISTS plugin_user_data;
DROP TABLE IF EXISTS server_plugins;
