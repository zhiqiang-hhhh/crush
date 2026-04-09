package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/skills"
)

//go:embed templates/memory_search.md
var memorySearchToolDescription []byte

//go:embed templates/memory_search_prompt.md.tpl
var memorySearchPromptTmpl []byte

func (c *coordinator) memorySearchTool(_ context.Context) (fantasy.AgentTool, error) {
	return fantasy.NewParallelAgentTool(
		tools.MemorySearchToolName,
		string(memorySearchToolDescription),
		func(ctx context.Context, params tools.MemorySearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Query == "" {
				return fantasy.NewTextErrorResponse("query is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.NewTextErrorResponse("session id missing from context"), nil
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.NewTextErrorResponse("agent message id missing from context"), nil
			}

			parentSession, err := c.sessions.Get(ctx, sessionID)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to get session: %s", err)), nil
			}

			if parentSession.SummaryMessageID == "" {
				return fantasy.NewTextErrorResponse("This session has not been summarized yet. The memory_search tool is only available after summarization."), nil
			}

			transcriptPath := TranscriptPath(c.cfg.Config().Options.DataDirectory, parentSession.ID)
			if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Transcript file not found at %s. The session may have been summarized before this feature was available.", transcriptPath)), nil
			}

			transcriptDir := filepath.Dir(transcriptPath)
			promptOpts := []prompt.Option{
				prompt.WithWorkingDir(transcriptDir),
			}

			promptTemplate, err := prompt.NewPrompt("memory_search", string(memorySearchPromptTmpl), promptOpts...)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error creating prompt: %s", err)
			}

			_, small, err := c.buildAgentModels(ctx, true)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building models: %s", err)
			}

			systemPrompt, err := promptTemplate.Build(ctx, small.Model.Provider(), small.Model.Model(), c.cfg)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building system prompt: %s", err)
			}

			smallProviderCfg, ok := c.cfg.Config().Providers.Get(small.ModelCfg.Provider)
			if !ok {
				return fantasy.ToolResponse{}, errors.New("small model provider not configured")
			}

			searchTools := []fantasy.AgentTool{
				tools.NewGlobTool(transcriptDir),
				tools.NewGrepTool(transcriptDir, config.ToolGrep{}),
				tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, (*skills.Tracker)(nil), transcriptDir),
			}

			agent := NewSessionAgent(SessionAgentOptions{
				LargeModel:           small,
				SmallModel:           small,
				SystemPromptPrefix:   smallProviderCfg.SystemPromptPrefix,
				SystemPrompt:         systemPrompt,
				DisableAutoSummarize: true,
				IsYolo:               c.permissions.SkipRequests(),
				Sessions:             c.sessions,
				Messages:             c.messages,
				Tools:                searchTools,
				IsSubAgent:           true,
			})

			fullPrompt := fmt.Sprintf("%s\n\nThe session transcript is located at: %s\n\nUse grep and view to search this file for the requested information.", params.Query, transcriptPath)

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      sessionID,
				AgentMessageID: agentMessageID,
				ToolCallID:     call.ID,
				Prompt:         fullPrompt,
				SessionTitle:   "Memory Search",
				SessionSetup: func(sid string) {
					c.permissions.AutoApproveSession(sid)
				},
			})
		}), nil
}
