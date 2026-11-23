-- 0032: users.avatar_thumb 头像缩略图列
--
-- 自定义头像是 data URL（前端压到 256×256 webp，仍可达 256KB）。列表类接口
-- （会话列表 / 群成员 / 好友 / 搜索）此前每个用户都回传一次完整头像，200 人
-- 群 ≈ 50MB 响应。前端上传时同时生成 64×64 缩略图，列表只读这一列。
-- TEXT 足够（64×64 webp q0.7 通常 1–3KB，base64 后也在 16KB 内）。
-- 存量用户：缩略图列为空，前端回退到 avatar（完整图），行为不变；用户下次
-- 换头像后自动补上缩略图。
ALTER TABLE users ADD COLUMN avatar_thumb TEXT NULL AFTER avatar;
