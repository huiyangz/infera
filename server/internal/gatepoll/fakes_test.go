package gatepoll

// 测试替身：multica / github client 与 Store 全部用 fake，不碰真服务、不碰真 DB
// （pg 例外，见 store_pg_test.go 的测试库引导模式）。

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/multica"
)

// ---------------------------------------------------------------------------
// fakeMultica：脚本化 multica client。GetIssue / ListCommentsSince 的语义
// 镜像真 client：评论升序、since 严格大于过滤；redeliverAnchor 模拟服务端
// 秒级截断导致的锚点评论重复下发（真实坑，见 multica.ListCommentsSince 注释）。
// ---------------------------------------------------------------------------

type fakeMultica struct {
	mu       sync.Mutex
	issues   map[string]multica.Issue // by issue id
	comments map[string][]multica.Comment
	getErr   map[string]error // by issue id
	listErr  map[string]error // by issue id

	redeliverAnchor bool
}

func newFakeMultica() *fakeMultica {
	return &fakeMultica{
		issues:   map[string]multica.Issue{},
		comments: map[string][]multica.Comment{},
		getErr:   map[string]error{},
		listErr:  map[string]error{},
	}
}

func (f *fakeMultica) addIssue(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues[id] = multica.Issue{ID: id, Identifier: "INFERA-1", Status: status}
}

func (f *fakeMultica) setStatus(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if iss, ok := f.issues[id]; ok {
		iss.Status = status
		f.issues[id] = iss
	}
}

func (f *fakeMultica) addComment(issueID, id, body string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[issueID] = append(f.comments[issueID], multica.Comment{
		ID: id, AuthorType: "agent", AuthorID: "agent-1", Content: body, CreatedAt: at,
	})
}

func (f *fakeMultica) GetIssue(_ context.Context, idOrKey string) (multica.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.getErr[idOrKey]; ok {
		return multica.Issue{}, err
	}
	iss, ok := f.issues[idOrKey]
	if !ok {
		return multica.Issue{}, fmt.Errorf("fakeMultica: issue %q 不存在", idOrKey)
	}
	return iss, nil
}

func (f *fakeMultica) ListCommentsSince(_ context.Context, issueID string, cur multica.CommentCursor) ([]multica.Comment, multica.CommentCursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.listErr[issueID]; ok {
		return nil, cur, err
	}
	all := f.comments[issueID]
	var out []multica.Comment
	for _, c := range all { // 升序（append 序）
		if c.CreatedAt.After(cur.Since) {
			out = append(out, c)
			continue
		}
		// 锚点秒截断模拟：与 since 相等的锚点评论重复下发
		if f.redeliverAnchor && c.CreatedAt.Equal(cur.Since) {
			out = append(out, c)
		}
	}
	next := cur
	if len(out) > 0 {
		last := out[len(out)-1]
		next = multica.CommentCursor{AfterID: last.ID, Since: last.CreatedAt}
	}
	return out, next, nil
}

// ---------------------------------------------------------------------------
// fakeGitHub：脚本化 github client。merge 成功即把 PR 置 closed（已合并）。
// ---------------------------------------------------------------------------

type fakeGitHub struct {
	mu        sync.Mutex
	prs       map[string]github.PullRequest // key: owner/repo/N
	diffStats map[string]github.DiffStats
	mergedPRs map[string]bool

	mergeCalls []string // key: owner/repo/N，每次 MergePullRequest 追加
	mergeErr   error    // 非 nil 时 MergePullRequest 返回该错误
	getPRErr   error
	diffErr    error
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		prs:       map[string]github.PullRequest{},
		diffStats: map[string]github.DiffStats{},
		mergedPRs: map[string]bool{},
	}
}

func prKey(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s/%d", owner, repo, number)
}

func (f *fakeGitHub) addPR(owner, repo string, number int, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	open := state == "open"
	f.prs[prKey(owner, repo, number)] = github.PullRequest{
		Number: number, State: state, Title: "pr", HTMLURL: fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, number),
		Mergeable: &open, MergeableState: state,
	}
}

func (f *fakeGitHub) setDiffStats(owner, repo string, number int, changes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.diffStats[prKey(owner, repo, number)] = github.DiffStats{Files: 2, Additions: changes, Deletions: 0, Changes: changes}
}

func (f *fakeGitHub) GetPullRequest(_ context.Context, owner, repo string, number int) (github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getPRErr != nil {
		return github.PullRequest{}, f.getPRErr
	}
	pr, ok := f.prs[prKey(owner, repo, number)]
	if !ok {
		return github.PullRequest{}, &github.APIError{Method: "GET", Path: "pulls", StatusCode: 404, Message: "Not Found"}
	}
	return pr, nil
}

func (f *fakeGitHub) GetDiffStats(_ context.Context, owner, repo string, number int) (github.DiffStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.diffErr != nil {
		return github.DiffStats{}, f.diffErr
	}
	s, ok := f.diffStats[prKey(owner, repo, number)]
	if !ok {
		return github.DiffStats{}, &github.APIError{Method: "GET", Path: "files", StatusCode: 404, Message: "Not Found"}
	}
	return s, nil
}

func (f *fakeGitHub) MergePullRequest(_ context.Context, owner, repo string, number int, in github.MergeInput) (github.MergeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := prKey(owner, repo, number)
	f.mergeCalls = append(f.mergeCalls, key)
	if f.mergeErr != nil {
		return github.MergeResult{}, f.mergeErr
	}
	if _, ok := f.prs[key]; !ok {
		return github.MergeResult{}, &github.APIError{Method: "PUT", Path: "merge", StatusCode: 404, Message: "Not Found"}
	}
	f.mergedPRs[key] = true
	pr := f.prs[key]
	pr.State = "closed"
	f.prs[key] = pr
	return github.MergeResult{Merged: true, SHA: "deadbeef", Message: "merged"}, nil
}

// ---------------------------------------------------------------------------
// memStore：内存 Store。语义镜像 PgStore（含 InsertCardIfNew 的评论去重）。
// ---------------------------------------------------------------------------

type memStore struct {
	mu      sync.Mutex
	reqs    map[string]flow.Requirement
	cursors map[string]flow.PollCursor
	cards   []flow.GateCard
	audits  []flow.AuditEntry

	seq         int
	listCalls   int
	cardErr     error
	listErr     error
	completeErr error
	saveErr     error
}

func newMemStore() *memStore {
	return &memStore{reqs: map[string]flow.Requirement{}, cursors: map[string]flow.PollCursor{}}
}

func (m *memStore) addReq(req flow.Requirement) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs[req.ID] = req
	m.cursors[req.ID] = flow.PollCursor{
		RequirementID:  req.ID,
		MulticaIssueID: req.MulticaIssueID,
		LastStatus:     "",
		SeenVerdict:    false,
	}
}

func (m *memStore) ListInFlight(_ context.Context) ([]InFlight, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []InFlight
	for _, r := range m.reqs {
		if r.MulticaIssueID == "" || r.Node == flow.NodeDelivered {
			continue
		}
		out = append(out, InFlight{Req: r, Cursor: m.cursors[r.ID]})
	}
	return out, nil
}

func (m *memStore) InsertCardIfNew(_ context.Context, card flow.GateCard) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cardErr != nil {
		return false, m.cardErr
	}
	if card.CommentID != "" {
		for _, c := range m.cards {
			if c.RequirementID == card.RequirementID && c.CommentID == card.CommentID {
				return false, nil
			}
		}
	}
	m.seq++
	card.ID = fmt.Sprintf("card-%d", m.seq)
	card.Status = flow.CardPending
	card.CreatedAt = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	m.cards = append(m.cards, card)
	return true, nil
}

func (m *memStore) ListPendingMergeCards(_ context.Context, requirementID string) ([]flow.GateCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []flow.GateCard
	for _, c := range m.cards {
		if c.RequirementID == requirementID && c.Kind == flow.GateMerge && c.Status == flow.CardPending {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memStore) CompleteAutoMerge(_ context.Context, cardID string, node flow.Node, prURL string, cur flow.PollCursor, audit flow.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completeErr != nil {
		return m.completeErr
	}
	for i := range m.cards {
		if m.cards[i].ID == cardID {
			m.cards[i].Status = flow.CardResolved
			now := time.Date(2026, 8, 21, 12, 1, 0, 0, time.UTC)
			m.cards[i].ResolvedAt = now
		}
	}
	m.seq++
	audit.ID = fmt.Sprintf("audit-%d", m.seq)
	audit.At = time.Date(2026, 8, 21, 12, 1, 0, 0, time.UTC)
	m.audits = append(m.audits, audit)
	if r, ok := m.reqs[cur.RequirementID]; ok {
		r.Node = node
		r.PRURL = prURL
		m.reqs[cur.RequirementID] = r
	}
	m.cursors[cur.RequirementID] = cur
	return nil
}

func (m *memStore) SavePollState(_ context.Context, requirementID string, node flow.Node, prURL string, cur flow.PollCursor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	if r, ok := m.reqs[requirementID]; ok {
		r.Node = node
		r.PRURL = prURL
		m.reqs[requirementID] = r
	}
	m.cursors[requirementID] = cur
	return nil
}

// -- 读辅助（断言用） --------------------------------------------------------

func (m *memStore) cardsOf(requirementID string) []flow.GateCard {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []flow.GateCard
	for _, c := range m.cards {
		if c.RequirementID == requirementID {
			out = append(out, c)
		}
	}
	return out
}

func (m *memStore) cardCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cards)
}

func (m *memStore) auditCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.audits)
}

// ---------------------------------------------------------------------------
// 组装辅助。
// ---------------------------------------------------------------------------

var testNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

// newTestReq 返回一个已派发的标准测试需求（issue 已建、大节点 dispatched）。
func newTestReq(id, issueID string) flow.Requirement {
	return flow.Requirement{
		ID: id, Title: "需求-" + id, MulticaIssueID: issueID, MulticaIssueKey: "INFERA-" + id,
		Node: flow.NodeDispatched,
	}
}

// newTestPoller 用 fake 三件套组装一个 30s 间隔的轮询器（测试里只手动 PollOnce，
// 不走 ticker；间隔取默认档）。
func newTestPoller(store Store, mc MulticaClient, gh GitHubClient, policy MergePolicyResolver) (*Poller, error) {
	return New(store, mc, gh, policy, 30*time.Second)
}
