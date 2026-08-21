// Package flow 是需求流转的核心契约：大节点状态机、闸门协议解析、领域类型。
//
// 契约冻结点（INFERA-11 T01）：本包的类型、解析器行为、连同
// db/migrations 中的 flow 表结构，以本包提交的代码为准。下游任务
// （gatepoll 轮询器、reqservice 需求服务、REST API）只读消费，禁止平行
// 另造入口。
//
// 本包是纯确定性代码：无网络、无 DB、无时间依赖（时间只作为透传数据），
// 不 import multica/github 等外部 client——增量评论由下游适配成
// CommentInput 喂进来。
package flow
