-- 回滚 0031：只删除为反向好友查询新增的索引。
DROP INDEX idx_friends_friend_status ON friends;
