-- 找回密码令牌改为哈希存储：列 token 改名 token_hash，仅存 sha256 哈希不存明文。
-- 现有明文令牌在迁移后立即失效（哈希不匹配），用户需重新发起找回密码流程（令牌 15 分钟有效期，影响面可忽略）。
ALTER TABLE `password_resets` CHANGE COLUMN `token` `token_hash` VARCHAR(64) NOT NULL;
ALTER TABLE `password_resets` RENAME INDEX `idx_password_resets_token` TO `idx_password_resets_token_hash`;
