-- 回滚 0032：删列。存量缩略图数据不会恢复（前端会回退到 avatar 完整图）。
ALTER TABLE users DROP COLUMN avatar_thumb;
