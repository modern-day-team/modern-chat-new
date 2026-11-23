-- 回滚 0033：删除会话表。撤销中的会话随之失效语义消失，JWT 退化为仅靠 tv 校验。
DROP TABLE IF EXISTS user_sessions;
