package gatepoll

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tokfinity/infera/internal/flow"
)

// PgStore 是 Store 的 pgx 实现，落 T01 冻结的三张表：
// requirements（节点 + 游标列）/ gate_cards / audit_log。
// 需求行的创建不在此（reqservice 的职责），本实现只读消费 + 推进。
type PgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore 基于 pgx 连接池构造。
func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

var _ Store = (*PgStore)(nil)

const requirementCols = `id,title,description,acceptance_criteria,source,priority,acceptors,
	external_issue_id,external_issue_key,node,pr_url,
	poll_last_comment_at,poll_last_status,poll_seen_verdict,created_at,updated_at`

const gateCardCols = `id,requirement_id,kind,status,payload,comment_id,created_at,resolved_at`

// ListInFlight 返回在途需求：已建上游卡（有 issue 映射）且未到已交付。
// needs_decision 仍在途（等待用户决策后恢复）；intake 未建卡不轮询。
// 去重语义说明：单轮询器进程内 SELECT+INSERT，无需并发防护。
func (s *PgStore) ListInFlight(ctx context.Context) ([]InFlight, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+requirementCols+`
		FROM requirements
		WHERE external_issue_id <> '' AND node <> 'delivered'
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InFlight
	for rows.Next() {
		var (
			req      flow.Requirement
			lastAt   *time.Time
			lastStat string
			seen     bool
		)
		if err := rows.Scan(
			&req.ID, &req.Title, &req.Description, &req.AcceptanceCriteria,
			&req.Source, &req.Priority, &req.Acceptors,
			&req.ExternalIssueID, &req.ExternalIssueKey, &req.Node, &req.PRURL,
			&lastAt, &lastStat, &seen, &req.CreatedAt, &req.UpdatedAt,
		); err != nil {
			return nil, err
		}
		cur := flow.PollCursor{
			RequirementID:   req.ID,
			ExternalIssueID: req.ExternalIssueID,
			LastStatus:      lastStat,
			SeenVerdict:     seen,
			UpdatedAt:       req.UpdatedAt,
		}
		if lastAt != nil {
			cur.LastCommentAt = *lastAt
		}
		out = append(out, InFlight{Req: req, Cursor: cur})
	}
	return out, rows.Err()
}

// InsertCardIfNew 落一张闸门卡。有评论溯源的卡按 (requirement_id, comment_id)
// 幂等去重——tasksource.ListCommentsSince 的调用方契约（锚点秒截断重发时按评论
// id 去重，宁可重发不漏发）。状态类兜底卡（comment_id 空）不去重。
func (s *PgStore) InsertCardIfNew(ctx context.Context, card flow.GateCard) (bool, error) {
	if card.CommentID != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM gate_cards WHERE requirement_id=$1 AND comment_id=$2)`,
			card.RequirementID, card.CommentID).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO gate_cards (id,requirement_id,kind,status,payload,comment_id)
		 VALUES ($1,$2,$3,'pending',$4,$5)`,
		uuid.NewString(), card.RequirementID, string(card.Kind), card.Payload, card.CommentID); err != nil {
		return false, err
	}
	return true, nil
}

// InsertCardIfNewAdvanceNode 落一张闸门卡并在同一事务内把需求节点推进到
// node（决策闸门事件 T08 的原子接线：卡新建 ⇔ 节点推进同生同灭）。
// 去重语义与 InsertCardIfNew 一致——重复评论返回 false 且不动节点。
// 事务序：INSERT 卡在前、UPDATE 节点在后——节点写失败时卡一并回滚。
func (s *PgStore) InsertCardIfNewAdvanceNode(ctx context.Context, card flow.GateCard, node flow.Node) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if card.CommentID != "" {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM gate_cards WHERE requirement_id=$1 AND comment_id=$2)`,
			card.RequirementID, card.CommentID).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO gate_cards (id,requirement_id,kind,status,payload,comment_id)
		 VALUES ($1,$2,$3,'pending',$4,$5)`,
		uuid.NewString(), card.RequirementID, string(card.Kind), card.Payload, card.CommentID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE requirements SET node=$2, updated_at=now() WHERE id=$1`,
		card.RequirementID, string(node)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ListPendingMergeCards 列出需求的待处理合并卡（自动合并清扫的输入）。
func (s *PgStore) ListPendingMergeCards(ctx context.Context, requirementID string) ([]flow.GateCard, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+gateCardCols+` FROM gate_cards
		 WHERE requirement_id=$1 AND kind='merge' AND status='pending'
		 ORDER BY created_at`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []flow.GateCard
	for rows.Next() {
		var (
			card     flow.GateCard
			resolved *time.Time
		)
		if err := rows.Scan(&card.ID, &card.RequirementID, &card.Kind, &card.Status,
			&card.Payload, &card.CommentID, &card.CreatedAt, &resolved); err != nil {
			return nil, err
		}
		if resolved != nil {
			card.ResolvedAt = *resolved
		}
		out = append(out, card)
	}
	return out, rows.Err()
}

// CompleteAutoMerge 自动合并收口，单事务：合并卡置 resolved + 写审计
// （actor=system）+ 节点/PR 引用/游标落库。原子性收缩崩溃窗口：合并调用成功
// 后的任何进程崩溃，重启后由 pending 卡清扫 + closed PR 判定收敛。
func (s *PgStore) CompleteAutoMerge(ctx context.Context, cardID string, node flow.Node, prURL string, cur flow.PollCursor, audit flow.AuditEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE gate_cards SET status='resolved', resolved_at=now() WHERE id=$1`, cardID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_log (id,requirement_id,actor,action,detail) VALUES ($1,$2,$3,$4,$5)`,
		uuid.NewString(), audit.RequirementID, audit.Actor, audit.Action, audit.Detail); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE requirements SET node=$2, pr_url=$3, poll_last_comment_at=$4, poll_last_status=$5,
		 poll_seen_verdict=$6, updated_at=now() WHERE id=$1`,
		cur.RequirementID, string(node), prURL,
		nullTime(cur.LastCommentAt), cur.LastStatus, cur.SeenVerdict); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SavePollState 落库节点、PR 引用与轮询游标（重启续读不重放的持久化根基）。
func (s *PgStore) SavePollState(ctx context.Context, requirementID string, node flow.Node, prURL string, cur flow.PollCursor) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE requirements SET node=$2, pr_url=$3, poll_last_comment_at=$4, poll_last_status=$5,
		 poll_seen_verdict=$6, updated_at=now() WHERE id=$1`,
		requirementID, string(node), prURL,
		nullTime(cur.LastCommentAt), cur.LastStatus, cur.SeenVerdict)
	return err
}

// nullTime：游标时间零值（尚未轮询）落 NULL，非零原样传参。
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
