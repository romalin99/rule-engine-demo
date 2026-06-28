-- ============================================================
-- 在现有 TCG_UCS.SERVICE_TERMS_VERSION 之上追加业务唯一约束
-- 作为应用层 MERGE INTO 幂等写入的「并发安全栅栏」
-- ============================================================
--
-- 背景表结构（已建）：
--   PARTITION BY RANGE (AGREE_TIME) INTERVAL (NUMTOYMINTERVAL(1,'MONTH'))
--   SUBPARTITION BY HASH (CUSTOMER_ID) SUBPARTITIONS 32
--   预建分区 P_BEFORE_2011 ~ P_2025，2026-01-01 起自动按月建分区
--
-- 业务键：(CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO)
--   语义：同一玩家在同一商户下，对同一条款节点的同一版本号只允许同意一次。
--
-- 应用层逻辑（Go repo `ServiceTermsVersionRepo.InsertBatch`）：
--   MERGE INTO ... USING (SELECT :bind FROM dual) s
--     ON (业务键四列等值)
--     WHEN NOT MATCHED THEN INSERT ...
--   MERGE 的 ON 子查询不持锁，并发场景下两条相同请求可能各自得到 NOT MATCHED。
--   此唯一约束是真正阻止双写的物理屏障：第二条 INSERT 触发 ORA-00001，
--   被 repo 层的 isOracleUniqueViolation() 静默吞掉，业务返回 success: true。
--
-- ⚠ 为什么必须是 GLOBAL UNIQUE 而不是 LOCAL UNIQUE：
--   Oracle 规定 LOCAL UNIQUE 索引必须包含全部分区列（AGREE_TIME + CUSTOMER_ID）。
--   若把 AGREE_TIME 纳入唯一键，跨月的同一业务键就不再视为重复，幂等失效
--   （同一玩家在不同月分区中可写入同一 NODE_ID/VERSION_NO 多次）。
--   因此本约束只能用 GLOBAL UNIQUE，代价是分区维护时需带 UPDATE GLOBAL INDEXES，
--   否则全局索引会被标记 UNUSABLE。
--
-- ⚠ 与 PK_STV 的关系：
--   PK_STV (ID) 同样是 GLOBAL 唯一索引（ID 不在任何分区列内，无法 LOCAL）。
--   本约束 UK_STV_CUST_MCH_NODE_VER 与 PK_STV 互不替代：
--     - PK_STV：服务于 FindOne(ID) / Update / Delete by ID 等单行精确查询
--     - 本约束：服务于业务键唯一性 + MERGE.ON 加速 + 并发竞态物理拦截

-- ============================================================
-- 0. 历史脏数据预检 + 清理（执行前必须人工评估）
-- ============================================================
-- 检查是否存在历史重复（添加唯一约束前必须为 0 行，否则 CREATE INDEX 会失败）
-- SELECT CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO, COUNT(*) AS DUP_CNT
--   FROM TCG_UCS.SERVICE_TERMS_VERSION
--  GROUP BY CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO
-- HAVING COUNT(*) > 1;
--
-- 若有重复，保留每组 ID 最大者（最新写入），清理旧行：
-- DELETE FROM TCG_UCS.SERVICE_TERMS_VERSION
--  WHERE ID NOT IN (
--    SELECT MAX(ID)
--      FROM TCG_UCS.SERVICE_TERMS_VERSION
--     GROUP BY CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO
--  );
-- COMMIT;

-- ============================================================
-- 1. 创建 GLOBAL UNIQUE 索引
-- ============================================================
-- 列顺序按选择性从高到低 (CUSTOMER_ID 选择性最高)：
--   - 同时充当 MERGE.ON (CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO) 的最优入口
--   - ONLINE 允许在线创建，不阻塞 DML
--   - 不指定 LOCAL 即默认 GLOBAL
CREATE UNIQUE INDEX TCG_UCS.UK_STV_CUST_MCH_NODE_VER
    ON TCG_UCS.SERVICE_TERMS_VERSION (CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO)
    ONLINE;

-- ============================================================
-- 2. 绑定 UNIQUE 约束到上述索引
--    USING INDEX 复用同一物理 B-tree，避免重复索引
-- ============================================================
ALTER TABLE TCG_UCS.SERVICE_TERMS_VERSION
    ADD CONSTRAINT UK_STV_CUST_MCH_NODE_VER
    UNIQUE (CUSTOMER_ID, MERCHANT_CODE, NODE_ID, VERSION_NO)
    USING INDEX TCG_UCS.UK_STV_CUST_MCH_NODE_VER;

COMMENT ON COLUMN TCG_UCS.SERVICE_TERMS_VERSION.CUSTOMER_ID IS
    '客户ID（HASH 子分区键 + 业务唯一键首列）';

-- ============================================================
-- 3. 收集统计信息（让优化器立刻识别新索引）
-- ============================================================
BEGIN
    DBMS_STATS.GATHER_INDEX_STATS(
        ownname => 'TCG_UCS',
        indname => 'UK_STV_CUST_MCH_NODE_VER',
        degree  => 16
    );
END;
/

-- ============================================================
-- 4. 分区维护规约（写入运维文档，DBA 必读）
-- ============================================================
-- 由于 UK_STV_CUST_MCH_NODE_VER 是 GLOBAL 索引，对分区表执行下列操作时必须
-- 加 UPDATE GLOBAL INDEXES，否则该索引被置为 UNUSABLE：
--
--   ALTER TABLE TCG_UCS.SERVICE_TERMS_VERSION
--     DROP PARTITION p_xxx
--     UPDATE GLOBAL INDEXES;
--
--   ALTER TABLE TCG_UCS.SERVICE_TERMS_VERSION
--     EXCHANGE PARTITION p_xxx WITH TABLE staging_xxx
--     UPDATE GLOBAL INDEXES;
--
--   ALTER TABLE TCG_UCS.SERVICE_TERMS_VERSION
--     TRUNCATE PARTITION p_xxx
--     UPDATE GLOBAL INDEXES;
--
-- 若意外漏写，索引被标记 UNUSABLE 后:
--   1) 应用 MERGE 的并发兜底失效 → 出现重复行风险
--   2) 业务侧不会立即报错，必须监控 USER_INDEXES.STATUS
-- 应急恢复：
--   ALTER INDEX TCG_UCS.UK_STV_CUST_MCH_NODE_VER REBUILD ONLINE;
