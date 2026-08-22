// 需求流转装配（INFERA-11 T07）：tasksource / github 薄 client、reqservice、
// gatepoll（SettingsPolicy）的 main 侧接线。全部构造器与配置以已提交代码
// 为准，本文件只做装配，不含新行为。
package main

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/gatepoll"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/reqservice"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
	"github.com/tokfinity/infera/internal/tasksource"
)

// assembleTaskSync 装配任务同步（INFERA-169）：凭据三键齐 → client + 同步
// 服务 + 自动同步调度器（启动即同步一轮，之后按 cfg.TaskSyncInterval 周期
// 轮询；0 = 仅启动轮）+ （配置了 Tech Lead 时的）需求创建编排器。三键
// 全空 = 未接入，返回全 nil 无错；半配交给 tasksource.New 显式报错（与
// assembleFlow 同一组键，错误更早暴露）。
func assembleTaskSync(cfg config.Config, st store.Store) (*syncsvc.Service, *syncsvc.Scheduler, *syncsvc.Creator, error) {
	if cfg.TaskSyncServerURL == "" && cfg.TaskSyncToken == "" && cfg.TaskSyncWorkspaceID == "" {
		return nil, nil, nil, nil
	}
	client, err := tasksource.New(cfg.TaskSyncServerURL, cfg.TaskSyncToken, cfg.TaskSyncWorkspaceID)
	if err != nil {
		return nil, nil, nil, err
	}
	svc := syncsvc.New(client, st)
	var creator *syncsvc.Creator
	if cfg.TaskSyncTechLeadAgentID != "" {
		creator, err = syncsvc.NewCreator(client, svc, st, syncsvc.CreatorOptions{
			TechLeadAgentID: cfg.TaskSyncTechLeadAgentID,
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return svc, syncsvc.NewScheduler(svc, cfg.TaskSyncInterval), creator, nil
}

// flowConfigured 报告是否尝试接入需求流转：全部流转键为空 = 未接入
// （不装配、需求路由 503、轮询器不启动——裸开发启动不受影响）；任一键
// 出现即视为尝试接入，缺项交给各构造器显式报错，不做静默降级。
func flowConfigured(cfg config.Config) bool {
	return cfg.TaskSyncServerURL != "" || cfg.TaskSyncToken != "" || cfg.TaskSyncWorkspaceID != "" ||
		cfg.TaskSyncProjectID != "" || cfg.TaskSyncTechLeadAgentID != "" || cfg.TaskSyncWorkspaceSlug != ""
}

// assembleFlow 装配需求流转三件套。返回 (nil, nil, nil) 表示未接入。
// 构造顺序刻意 tasksource → github → poller → reqservice：上游 client 的
// 误配（缺 token / 云端地址 / 非法间隔）先于 reqservice 的 Options 校验
// 报出，错误信息最贴近根因。
func assembleFlow(pool *pgxpool.Pool, cfg config.Config) (*reqservice.Service, *gatepoll.Poller, error) {
	if !flowConfigured(cfg) {
		return nil, nil, nil
	}
	mc, err := tasksource.New(cfg.TaskSyncServerURL, cfg.TaskSyncToken, cfg.TaskSyncWorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	var ghOpts []github.Option
	if cfg.GitHubAPIURL != "" {
		ghOpts = append(ghOpts, github.WithBaseURL(cfg.GitHubAPIURL))
	}
	gh, err := github.New(cfg.GitHubToken, ghOpts...)
	if err != nil {
		return nil, nil, err
	}
	// 合并策略解析器用 SettingsPolicy（读 project_settings 表，缺省/损坏回落
	// manual）——部署级策略档位，不是 StaticPolicy。
	poller, err := gatepoll.New(
		gatepoll.NewPgStore(pool), mc, gh,
		gatepoll.NewSettingsPolicy(pool), cfg.GatePollInterval)
	if err != nil {
		return nil, nil, err
	}
	reqSvc, err := reqservice.New(pool, mc, gh, reqservice.Options{
		TaskSyncProjectID:     cfg.TaskSyncProjectID,
		TechLeadAgentID:       cfg.TaskSyncTechLeadAgentID,
		TaskSyncServerURL:     cfg.TaskSyncServerURL,
		TaskSyncWorkspaceSlug: cfg.TaskSyncWorkspaceSlug,
	})
	if err != nil {
		return nil, nil, err
	}
	return reqSvc, poller, nil
}
