package main

import "net/http"

const (
	// E24: on a 6 GB GPU two parallel requests cost almost nothing on the
	// GQA models, while four already pushed phi3 onto the CPU. A stock
	// Ollama queues same-model requests anyway, so 2 is free there too.
	defaultMaxAgents = 2
	cloudMaxAgents   = 10
	maxMaxAgents     = 64
)

func (s *Server) maxAgentsFor(user *User, c Conversation) int {
	if s.ollama.modelInfo(s.ollamaURLFor(user), c.Model).RemoteHost != "" {
		if user.CloudMaxAgents != nil {
			return *user.CloudMaxAgents
		}
		return cloudMaxAgents
	}
	if user.MaxAgents != nil {
		return *user.MaxAgents
	}
	return defaultMaxAgents
}

func (s *Server) handleGetAgentRun(w http.ResponseWriter, r *http.Request) {
	run, err := getAgentRun(s.db, r.PathValue("id"), userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetAgentTranscript(w http.ResponseWriter, r *http.Request) {
	msgs, found, err := getAgentMessages(s.db, r.PathValue("id"), r.PathValue("agentId"), userFromContext(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}
