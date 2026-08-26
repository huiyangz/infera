// 任务描述「上游优先」编辑编排（INFERA-298 冻结契约）：任务详情页描述编辑
// 的后端面。
//
// 背景约束：importIssue 每轮全量同步都用上游快照整行 upsert 描述，任何只写
// 本地的路径都会在下一轮被覆盖。因此这里不发明本地持久化——唯一写路径是经
// tasksource 改上游；本地值取自上游读回，同步自然一致。
package syncsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// IssueEditor 是描述编辑面对 tasksource 薄 client 的最小依赖
// （*tasksource.Client 天然满足）。Fetcher 刻意保持只读面，写入面独立成窄
// 接口，Go 鸭型——与创建面的 IssueCreator 同款。
type IssueEditor interface {
	UpdateIssueDescription(ctx context.Context, issueID, description string) error
	GetIssue(ctx context.Context, idOrKey string) (tasksource.Issue, error)
}

// MaxDescriptionBytes 描述长度上限（UTF-8 字节数）。Markdown 文档远小于此；
// 意义在于赶在 1MiB 请求体上限与上游落库上限之前先给出可读的校验错误，
// 而不是把超长文本打到上游换一个 4xx 回来。
const MaxDescriptionBytes = 64 << 10 // 64KiB

// ErrNotMirrored 交付无上游映射（external_issue_id 为空）：非同步来源的交付
// 没有可写的上游对象，上游优先路径无从生效。
var ErrNotMirrored = errors.New("syncsvc: 交付无上游映射")

// Editor 描述编辑编排器。一次构造多处复用；无自有并发状态——上游调用与
// 落库串行于单请求内，本地行级竞争由 store.UpdateDelivery 的乐观锁兜底。
type Editor struct {
	ed IssueEditor
	st store.Store
}

// NewEditor 构造编排器。依赖缺失在构造期报错（缺项漏到运行期只会变成
// 难排查的空指针）。
func NewEditor(ed IssueEditor, st store.Store) (*Editor, error) {
	if ed == nil {
		return nil, errors.New("syncsvc: tasksource client 缺失")
	}
	if st == nil {
		return nil, errors.New("syncsvc: store 缺失")
	}
	return &Editor{ed: ed, st: st}, nil
}

// UpdateDeliveryDescription 编辑任务描述：校验 → 读交付 → 上游写 → 读回 →
// 本地只落描述一列。返回落库后的整行。
//
// 本地不做独立持久化：落库的描述永远取自上游读回（经 MapIssue 归一，与
// 全量同步同一映射），所以下一轮同步导入的是同一个值，不会回滚。刻意不做
// 整行重导入（UpsertDeliveryByExternalID）——那会把非终态镜像打回 queued、
// 冲掉停在门禁的交付；描述编辑只该动描述。
//
// 读回失败时上游写已确认成功：按已发送的请求体降级落库、不转为错误——
// 重试只会对上游重复一次幂等的 PUT，而报错会让调用方误以为没保存成。
func (e *Editor) UpdateDeliveryDescription(ctx context.Context, deliveryID, description string) (store.Delivery, error) {
	if strings.TrimSpace(description) == "" {
		return store.Delivery{}, fmt.Errorf("%w: 描述不能为空", ErrInvalid)
	}
	if len(description) > MaxDescriptionBytes {
		return store.Delivery{}, fmt.Errorf("%w: 描述超过 %d 字节上限（got %d）", ErrInvalid, MaxDescriptionBytes, len(description))
	}

	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return store.Delivery{}, err
	}
	if d.ExternalIssueID == "" {
		return store.Delivery{}, fmt.Errorf("%w: 交付 %s 非同步来源，无上游对象可写", ErrNotMirrored, deliveryID)
	}

	if err := e.ed.UpdateIssueDescription(ctx, d.ExternalIssueID, description); err != nil {
		return store.Delivery{}, fmt.Errorf("上游更新描述失败: %w", err)
	}

	// 读回取上游权威值，不信任请求体——上游才是描述的 source of truth。
	applied := description
	if issue, err := e.ed.GetIssue(ctx, d.ExternalIssueID); err == nil {
		applied = tasksource.MapIssue(issue).Description
	}

	d.Description = applied
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return store.Delivery{}, fmt.Errorf("落库编辑后的描述失败: %w", err)
	}
	return *d, nil
}
