-- 0037: 插件分发契约
--
-- engines      宿主能力要求（当前只有 sdk），安装时校验兼容性。
-- dependencies 依赖的同名插件及版本范围，启用时校验是否满足。
-- 两者都以 JSON 串存储：安装/升级时整体覆盖，查询时从响应体拿，不需要关系表。
ALTER TABLE `plugins`
    ADD COLUMN engines TEXT NULL,
    ADD COLUMN dependencies TEXT NULL;