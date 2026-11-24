-- 回滚：列名与索引恢复为迁移前状态。
ALTER TABLE `password_resets` CHANGE COLUMN `token_hash` `token` VARCHAR(64) NOT NULL;
ALTER TABLE `password_resets` RENAME INDEX `idx_password_resets_token_hash` TO `idx_password_resets_token`;
