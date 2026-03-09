package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// UploadFiles handles POST /api/tasks/{taskID}/files.
// Accepts multipart/form-data with one or more files.
func (h *Handlers) UploadFiles(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	// Verify task exists
	task, err := h.svc.DB.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Parse multipart form — max 100MB
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	files := r.MultipartForm.File["files[]"]
	if len(files) == 0 {
		// Also try "files" key without brackets
		files = r.MultipartForm.File["files"]
	}
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no files provided")
		return
	}

	type uploadedFile struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Path string `json:"path"`
	}

	var uploaded []uploadedFile

	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			slog.Error("failed to open uploaded file", "filename", fh.Filename, "error", err)
			continue
		}

		info, err := h.svc.FileManager.Upload(r.Context(), taskID, fh.Filename, f)
		f.Close()
		if err != nil {
			slog.Error("failed to upload file", "filename", fh.Filename, "error", err)
			continue
		}

		uploaded = append(uploaded, uploadedFile{
			Name: info.Name,
			Size: info.Size,
			Path: info.Path,
		})
	}

	// Record event
	h.recordEventBackground(r.Context(), taskID, "files.uploaded", nil)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"uploaded": uploaded,
	})
}

// ListFiles handles GET /api/tasks/{taskID}/files.
// Returns input and output files for the task.
func (h *Handlers) ListFiles(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	inputs, outputs, workspace, err := h.svc.FileManager.ListFiles(r.Context(), taskID)
	if err != nil {
		slog.Error("failed to list files", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}

	// Convert to response type
	inputResp := make([]fileInfoResponse, 0, len(inputs))
	for _, f := range inputs {
		inputResp = append(inputResp, fileInfoResponse{
			Name: f.Name,
			Size: f.Size,
		})
	}
	outputResp := make([]fileInfoResponse, 0, len(outputs))
	for _, f := range outputs {
		outputResp = append(outputResp, fileInfoResponse{
			Name: f.Name,
			Size: f.Size,
		})
	}
	workspaceResp := make([]fileInfoResponse, 0, len(workspace))
	for _, f := range workspace {
		workspaceResp = append(workspaceResp, fileInfoResponse{
			Name: f.Name,
			Size: f.Size,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"input":     inputResp,
		"output":    outputResp,
		"workspace": workspaceResp,
	})
}

type fileInfoResponse struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// DownloadFile handles GET /api/tasks/{taskID}/files/{filename}.
// Downloads a specific file from the task.
// For workspace files with paths, use ?path=subdir/file.ext&type=workspace
func (h *Handlers) DownloadFile(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	filename := chi.URLParam(r, "filename")

	if taskID == "" || filename == "" {
		writeError(w, http.StatusBadRequest, "taskID and filename are required")
		return
	}

	direction := r.URL.Query().Get("type")
	if direction == "" {
		direction = "output"
	}

	// For workspace files, the full relative path is passed via ?path= query param
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		filePath = filename
	}

	path, err := h.svc.FileManager.DownloadPath(taskID, filePath, direction)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(filePath)+"\"")
	http.ServeFile(w, r, path)
}

// GetFileContent handles GET /api/tasks/{taskID}/files/content?path=...&type=...
// Returns the file content as JSON for viewing in the UI.
func (h *Handlers) GetFileContent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	filePath := r.URL.Query().Get("path")

	if taskID == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "taskID and path are required")
		return
	}

	direction := r.URL.Query().Get("type")
	if direction == "" {
		direction = "output"
	}

	absPath, err := h.svc.FileManager.DownloadPath(taskID, filePath, direction)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Read file content (limit to 2MB for safety)
	info, err := os.Stat(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if info.Size() > 2*1024*1024 {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large to view (max 2MB)")
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    filepath.Base(filePath),
		"path":    filePath,
		"size":    info.Size(),
		"content": string(data),
	})
}

// UpdateFileContent handles PUT /api/tasks/{taskID}/files/content?path=...&type=...
// Updates the file content from a JSON body { "content": "..." }.
func (h *Handlers) UpdateFileContent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	filePath := r.URL.Query().Get("path")

	if taskID == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "taskID and path are required")
		return
	}

	direction := r.URL.Query().Get("type")
	if direction == "" {
		direction = "output"
	}

	absPath, err := h.svc.FileManager.DownloadPath(taskID, filePath, direction)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := os.WriteFile(absPath, []byte(body.Content), 0o644); err != nil {
		slog.Error("failed to write file", "path", absPath, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	info, _ := os.Stat(absPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name": filepath.Base(filePath),
		"path": filePath,
		"size": size,
	})
}

// DeleteFile handles DELETE /api/tasks/{taskID}/files/delete?path=...&type=...
// Deletes a file from the task workspace/input/output.
func (h *Handlers) DeleteFile(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	filePath := r.URL.Query().Get("path")

	if taskID == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "taskID and path are required")
		return
	}

	direction := r.URL.Query().Get("type")
	if direction == "" {
		direction = "output"
	}

	absPath, err := h.svc.FileManager.DownloadPath(taskID, filePath, direction)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := os.Remove(absPath); err != nil {
		slog.Error("failed to delete file", "path", absPath, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
