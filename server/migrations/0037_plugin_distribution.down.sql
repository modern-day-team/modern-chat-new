-- 0037 回滚：去掉插件分发契约字段。
ALTER TABLE `plugins`
    DROP COLUMN engines,
    DROP COLUMN dependencies;