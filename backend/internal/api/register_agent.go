package api

// registerAgent registers the routes of the KI-Agent (PLAN 44). Settings
// belong to owners and administrators; chats, projects, memory, skills
// and recurring tasks to every account that may use the agent, each only
// its own.
func (s *Server) registerAgent(mux *routeMux) {
	// Assist: the Ollama routes Home Assistant's integration calls (agent_assist.go).
	mux.HandleFunc(assistPrefix+"/", s.handleAgentOllama)
	mux.HandleFunc("GET /api/v1/agent/status", s.requireUser(s.handleAgentStatus))
	mux.HandleFunc("GET /api/v1/agent/settings", s.requireAdmin(s.handleAgentSettings))
	mux.HandleFunc("PUT /api/v1/agent/settings", s.requireAdmin(s.handleAgentSettingsSet))
	// The agent across the cluster (PLAN 47).
	mux.HandleFunc("GET /api/v1/agent/cluster", s.requireAdmin(s.handleAgentClusterInfo))
	mux.HandleFunc("POST /api/v1/agent/cluster/request", s.requireAdmin(s.handleAgentShareRequest))
	mux.HandleFunc("PUT /api/v1/agent/cluster/nodes/{node}", s.requireAdmin(s.handleAgentShareSet))
	mux.HandleFunc("POST /api/v1/agent/check", s.requireAdmin(s.handleAgentCheck))
	mux.HandleFunc("POST /api/v1/agent/voice/check", s.requireAdmin(s.handleAgentVoiceCheck))
	mux.HandleFunc("GET /api/v1/agent/tools", s.requireAdmin(s.handleAgentTools))

	mux.HandleFunc("GET /api/v1/agent/chats", s.requireAgent(s.handleAgentChats))
	mux.HandleFunc("POST /api/v1/agent/chats", s.requireAgent(s.handleAgentChatNew))
	mux.HandleFunc("GET /api/v1/agent/chats/{id}", s.requireAgent(s.handleAgentChat))
	mux.HandleFunc("PATCH /api/v1/agent/chats/{id}", s.requireAgent(s.handleAgentChatPatch))
	mux.HandleFunc("DELETE /api/v1/agent/chats/{id}", s.requireAgent(s.handleAgentChatDelete))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/messages", s.requireAgent(s.handleAgentMessage))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/decide", s.requireAgent(s.handleAgentDecide))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/stop", s.requireAgent(s.handleAgentStop))
	mux.HandleFunc("GET /api/v1/agent/chats/{id}/events", s.requireAgent(s.handleAgentEvents))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/attachments", s.requireAgent(s.handleAgentAttach))
	mux.HandleFunc("GET /api/v1/agent/chats/{id}/export", s.requireAgent(s.handleAgentExport))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/undo", s.requireAgent(s.handleAgentUndo))
	mux.HandleFunc("POST /api/v1/agent/chats/{id}/seen", s.requireAgent(s.handleAgentSeen))
	mux.HandleFunc("POST /api/v1/agent/transcribe", s.requireAgent(s.handleAgentTranscribe))
	mux.HandleFunc("GET /api/v1/agent/projects/{id}/files", s.requireAgent(s.handleAgentProjectFiles))
	mux.HandleFunc("POST /api/v1/agent/preload", s.requireAgent(s.handleAgentPreload))
	mux.HandleFunc("GET /api/v1/agent/models/pull", s.requireAdmin(s.handleAgentPullState))
	mux.HandleFunc("POST /api/v1/agent/models/pull", s.requireAdmin(s.handleAgentPull))

	mux.HandleFunc("GET /api/v1/agent/projects", s.requireAgent(s.handleAgentProjects))
	mux.HandleFunc("GET /api/v1/agent/accounts", s.requireAgent(s.handleAgentAccounts))
	mux.HandleFunc("POST /api/v1/agent/projects", s.requireAgent(s.handleAgentProjectNew))
	mux.HandleFunc("PUT /api/v1/agent/projects/{id}", s.requireAgent(s.handleAgentProjectSet))
	mux.HandleFunc("DELETE /api/v1/agent/projects/{id}", s.requireAgent(s.handleAgentProjectDelete))

	mux.HandleFunc("GET /api/v1/agent/memory", s.requireAgent(s.handleAgentMemory))
	mux.HandleFunc("PUT /api/v1/agent/memory/{id}", s.requireAgent(s.handleAgentMemorySet))
	mux.HandleFunc("DELETE /api/v1/agent/memory/{id}", s.requireAgent(s.handleAgentMemoryDelete))

	mux.HandleFunc("GET /api/v1/agent/skills", s.requireAgent(s.handleAgentSkills))
	mux.HandleFunc("PUT /api/v1/agent/skills/{id}", s.requireAgent(s.handleAgentSkillSet))
	mux.HandleFunc("DELETE /api/v1/agent/skills/{id}", s.requireAgent(s.handleAgentSkillDelete))

	mux.HandleFunc("GET /api/v1/agent/map", s.requireAgent(s.handleAgentMap))
	mux.HandleFunc("POST /api/v1/agent/map/refresh", s.requireAdmin(s.handleAgentMapRefresh))
	mux.HandleFunc("PUT /api/v1/agent/descriptions", s.requireAdmin(s.handleAgentDescription))
	mux.HandleFunc("GET /api/v1/agent/prefs", s.requireAgent(s.handleAgentPrefs))
	mux.HandleFunc("PUT /api/v1/agent/prefs", s.requireAgent(s.handleAgentPrefsSet))
	mux.HandleFunc("GET /api/v1/agent/suggestions", s.requireAgent(s.handleAgentSuggestions))
	mux.HandleFunc("GET /api/v1/agent/profile", s.requireAgent(s.handleAgentProfile))
	mux.HandleFunc("PUT /api/v1/agent/profile", s.requireAgent(s.handleAgentProfileSet))
	mux.HandleFunc("GET /api/v1/agent/schedules", s.requireAgent(s.handleAgentSchedules))
	mux.HandleFunc("PUT /api/v1/agent/schedules/{id}", s.requireAgent(s.handleAgentScheduleSet))
	mux.HandleFunc("DELETE /api/v1/agent/schedules/{id}", s.requireAgent(s.handleAgentScheduleDelete))
	mux.HandleFunc("POST /api/v1/agent/schedules/{id}/run", s.requireAgent(s.handleAgentScheduleRun))
}
