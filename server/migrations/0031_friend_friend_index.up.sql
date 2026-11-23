-- 0031: friends(friend_id, status) 索引
--
-- 好友关系的唯一索引 (user_id, friend_id) 以 user_id 为最左前缀，无法服务
-- 「查我被谁加了好友」的反向查询。FriendIDs() / PendingToUser() 都按
-- friend_id = ? 过滤，而这两个查询在每次 WS 上下线（presence 广播）与
-- 好友列表刷新时触发——大站每次重连风暴都会对 friends 全表扫描。
-- 加 (friend_id, status) 同时覆盖状态过滤，把全表扫描变成索引查找。
CREATE INDEX idx_friends_friend_status ON friends (friend_id, status);
