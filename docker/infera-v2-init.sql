-- 新库 infera_v2：后端默认 DATABASE_URL 指向它。
-- docker-entrypoint-initdb.d 只在数据卷为空（首次启动）时执行，
-- 因此老环境（已手动建库/已有数据卷）不受影响。
CREATE DATABASE infera_v2;
