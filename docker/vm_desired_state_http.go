package main

import (
	"encoding/json"
	"net/http"
)

func (s *VMHTTPServer) handleDesiredState(w http.ResponseWriter, r *http.Request) {
	if s.reconciler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "自动收敛器未初始化")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, http.StatusOK, s.reconciler.Status())
	case http.MethodPut:
		var req DesiredNetworkState
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "无效的请求体: "+err.Error())
			return
		}
		if err := s.reconciler.UpdateDesiredState(req); err != nil {
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":      false,
				"message": err.Error(),
				"status":  s.reconciler.Status(),
			})
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "期望网络状态已更新并开始收敛",
			"status":  s.reconciler.Status(),
		})
		s.notifySSEClients()
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 GET/PUT 方法")
	}
}

func (s *VMHTTPServer) handleReconcileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	if s.reconciler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "自动收敛器未初始化")
		return
	}
	s.writeJSON(w, http.StatusOK, s.reconciler.Status())
}
