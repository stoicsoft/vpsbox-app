package app

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	manager *Manager
	mu      sync.Mutex
	jobs    map[string]jobStatus
}

type pageData struct {
	Instances any
	Shares    any
	Jobs      []jobStatus
	Error     string
}

type jobStatus struct {
	Name    string
	State   string
	Message string
}

func NewServer(manager *Manager) *Server {
	return &Server{
		manager: manager,
		jobs:    map[string]jobStatus{},
	}
}

func (s *Server) Run(ctx context.Context, port int, openBrowser bool) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex(ctx))
	mux.HandleFunc("POST /instances/create", s.handleCreate(ctx))
	mux.HandleFunc("POST /instances/{name}/down", s.handleDown(ctx))
	mux.HandleFunc("POST /instances/{name}/destroy", s.handleDestroy(ctx))
	mux.HandleFunc("POST /instances/{name}/snapshot", s.handleSnapshot(ctx))
	mux.HandleFunc("POST /instances/{name}/reset", s.handleReset(ctx))
	mux.HandleFunc("POST /instances/{name}/checkpoint", s.handleCheckpoint(ctx))
	mux.HandleFunc("POST /instances/{name}/undo", s.handleUndo(ctx))
	mux.HandleFunc("POST /instances/{name}/panic", s.handlePanic(ctx))
	mux.HandleFunc("POST /share", s.handleShare(ctx))
	mux.HandleFunc("POST /unshare/{name}", s.handleUnshare())

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "http://" + addr
	if openBrowser {
		go open(url)
	}

	fmt.Printf("Dashboard running at %s\n", url)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return server.ListenAndServe()
}

func (s *Server) handleIndex(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instances, _ := s.manager.List(ctx)
		shares, _ := s.manager.Shares()
		render(w, pageData{Instances: instances, Shares: shares, Jobs: s.listJobs()})
	}
}

func (s *Server) handleCreate(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		name := r.Form.Get("name")
		if name == "" {
			suggested, err := s.manager.SuggestName()
			if err != nil {
				s.renderError(ctx, w, err)
				return
			}
			name = suggested
		}

		opts := UpOptions{
			Name:     name,
			Image:    "24.04",
			User:     "root",
			CPUs:     parseInt(r.Form.Get("cpus"), 2),
			MemoryGB: parseInt(r.Form.Get("memory"), 2),
			DiskGB:   parseInt(r.Form.Get("disk"), 10),
		}
		s.setJob(jobStatus{Name: name, State: "starting", Message: "Provisioning sandbox with Multipass and cloud-init"})
		go s.runCreate(opts)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleDown(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.manager.Down(ctx, r.PathValue("name")); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleDestroy(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.manager.Destroy(ctx, r.PathValue("name"), true); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleSnapshot(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if err := s.manager.Snapshot(ctx, name, "baseline", "created from local dashboard"); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleReset(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.manager.Reset(ctx, r.PathValue("name"), "baseline"); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleCheckpoint(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		s.setJob(jobStatus{Name: name, State: "saving", Message: "Capturing checkpoint snapshot and baseline"})
		go func() {
			if _, err := s.manager.Checkpoint(context.Background(), name, ""); err != nil {
				s.setJob(jobStatus{Name: name, State: "error", Message: err.Error()})
				return
			}
			s.setJob(jobStatus{Name: name, State: "ready", Message: "Checkpoint saved"})
			time.AfterFunc(20*time.Second, func() { s.deleteJob(name) })
		}()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleUndo(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, _, err := s.manager.Undo(ctx, name); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handlePanic(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, _, err := s.manager.Panic(ctx, name); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleShare(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if _, err := s.manager.CreateShare(ctx, r.Form.Get("target"), 4*time.Hour, ""); err != nil {
			s.renderError(ctx, w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleUnshare() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.manager.Unshare(r.PathValue("name")); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) renderError(ctx context.Context, w http.ResponseWriter, err error) {
	instances, _ := s.manager.List(ctx)
	shares, _ := s.manager.Shares()
	render(w, pageData{
		Instances: instances,
		Shares:    shares,
		Jobs:      s.listJobs(),
		Error:     err.Error(),
	})
}

func (s *Server) runCreate(opts UpOptions) {
	_, err := s.manager.Up(context.Background(), opts)
	if err != nil {
		s.setJob(jobStatus{Name: opts.Name, State: "error", Message: err.Error()})
		return
	}
	s.setJob(jobStatus{Name: opts.Name, State: "ready", Message: "Sandbox is ready"})
	time.AfterFunc(20*time.Second, func() {
		s.deleteJob(opts.Name)
	})
}

func (s *Server) setJob(job jobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.Name] = job
}

func (s *Server) deleteJob(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, name)
}

func (s *Server) listJobs() []jobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]jobStatus, 0, len(s.jobs))
	for _, job := range s.jobs {
		items = append(items, job)
	}
	return items
}

func render(w http.ResponseWriter, data pageData) {
	tpl := template.Must(template.New("page").Parse(pageTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, data)
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func open(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>vpsbox</title>
  <style>
    :root { color-scheme: light; font-family: "IBM Plex Sans", "Avenir Next", sans-serif; }
    body { margin: 0; padding: 32px; background: radial-gradient(circle at top left, #f7f0df, #f3f5ff 40%, #eef8f4 100%); color: #1a1d21; }
    .wrap { max-width: 1100px; margin: 0 auto; }
    .hero { display: flex; justify-content: space-between; gap: 24px; align-items: start; margin-bottom: 22px; }
    h1 { margin: 0; font-size: 42px; line-height: 1; }
    h2 { margin: 0 0 12px; font-size: 20px; }
    h3 { margin: 0 0 6px; font-size: 14px; text-transform: uppercase; letter-spacing: 0.05em; color: #6b7280; }
    p { margin: 8px 0 0; color: #44515f; }
    .grid { display: grid; grid-template-columns: 1.1fr 0.9fr; gap: 18px; }
    .card { background: rgba(255,255,255,0.85); border: 1px solid rgba(17,24,39,0.08); border-radius: 18px; padding: 20px; backdrop-filter: blur(12px); box-shadow: 0 20px 60px rgba(24, 29, 38, 0.08); margin-bottom: 18px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-top: 8px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th, td { text-align: left; padding: 10px 6px; border-bottom: 1px solid rgba(17,24,39,0.08); vertical-align: top; }
    input { padding: 10px 12px; border-radius: 12px; border: 1px solid rgba(17,24,39,0.12); min-width: 0; }
    button { background: #1b4d3e; color: white; border: none; border-radius: 999px; padding: 8px 12px; cursor: pointer; font-size: 12px; }
    button.alt { background: #2f3542; }
    button.warn-btn { background: #c2410c; }
    .warn { background: #fff2dd; border: 1px solid #ebc98d; color: #714b00; border-radius: 12px; padding: 12px 14px; margin-bottom: 18px; }
    .pill { display:inline-block; padding: 4px 10px; border-radius: 999px; background: #eef2f7; font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; }
    .resources { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin-top: 8px; }
    .resource { background: #f8f9fb; border-radius: 10px; padding: 8px 10px; }
    .resource .num { font-size: 18px; font-weight: 600; color: #1b4d3e; }
    .resource .lbl { font-size: 11px; color: #6b7280; text-transform: uppercase; letter-spacing: 0.04em; }
    .cheat code { background: #f1f4f7; padding: 2px 6px; border-radius: 6px; font-size: 12px; color: #1b4d3e; }
    .cheat li { margin: 6px 0; color: #44515f; font-size: 13px; }
    .cheat ul { padding-left: 18px; margin: 6px 0 0; }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="hero">
      <div>
        <h1>vpsbox</h1>
        <p>The local Ubuntu VPS sandbox. Break stuff safely. Roll back any time.</p>
      </div>
      <div class="pill">http://localhost:7878</div>
    </div>
    {{ if .Error }}<div class="warn">{{ .Error }}</div>{{ end }}
    {{ range .Jobs }}<div class="warn"><strong>{{ .Name }}</strong>: {{ .State }}{{ if .Message }} · {{ .Message }}{{ end }}</div>{{ end }}

    <div class="card cheat">
      <h2>First time using a VPS?</h2>
      <p>Everything here is a sandbox — there's no real server, no bill, and you can't break anything you can't roll back.</p>
      <div class="grid" style="margin-top: 14px;">
        <div>
          <h3>Learn safely</h3>
          <ul>
            <li><code>vpsbox tour</code> — guided 10-step walkthrough of the VM</li>
            <li><code>vpsbox learn</code> — hands-on missions with verify checks</li>
            <li><code>vpsbox explain chmod</code> — plain-English command help</li>
            <li><code>vpsbox deploy --list</code> — one-shot starter apps</li>
          </ul>
        </div>
        <div>
          <h3>Save and roll back</h3>
          <ul>
            <li><code>vpsbox checkpoint</code> — save a "known good" point</li>
            <li><code>vpsbox diff</code> — see what changed since</li>
            <li><code>vpsbox undo</code> — roll back to last checkpoint</li>
            <li><code>vpsbox panic</code> — the "I broke it" button</li>
          </ul>
        </div>
      </div>
    </div>

    <div class="grid">
      <div class="card">
        <h2>Instances</h2>
        <form method="post" action="/instances/create" class="row">
          <input name="name" placeholder="name (optional)" />
          <input name="cpus" placeholder="cpus" value="2" />
          <input name="memory" placeholder="memory GB" value="2" />
          <input name="disk" placeholder="disk GB" value="10" />
          <button type="submit">Create</button>
        </form>
        <table>
          <thead>
            <tr><th>Sandbox</th><th>Resources</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {{ range .Instances }}
            <tr>
              <td>
                <strong>{{ .Name }}</strong><br />
                <small>{{ .Status }} · {{ .Host }}</small><br />
                <small>{{ .Hostname }}</small>
              </td>
              <td>
                <div class="resources">
                  <div class="resource"><div class="num">{{ .CPUs }}</div><div class="lbl">vCPU</div></div>
                  <div class="resource"><div class="num">{{ .MemoryGB }}G</div><div class="lbl">RAM</div></div>
                  <div class="resource"><div class="num">{{ .DiskGB }}G</div><div class="lbl">Disk</div></div>
                </div>
              </td>
              <td>
                <div class="row">
                  <form method="post" action="/instances/{{ .Name }}/checkpoint"><button>Checkpoint</button></form>
                  <form method="post" action="/instances/{{ .Name }}/undo"><button class="alt">Undo</button></form>
                  <form method="post" action="/instances/{{ .Name }}/panic"><button class="warn-btn">Panic</button></form>
                </div>
                <div class="row">
                  <form method="post" action="/instances/{{ .Name }}/down"><button class="alt">Stop</button></form>
                  <form method="post" action="/instances/{{ .Name }}/snapshot"><button class="alt">Snapshot</button></form>
                  <form method="post" action="/instances/{{ .Name }}/reset"><button class="alt">Reset</button></form>
                  <form method="post" action="/instances/{{ .Name }}/destroy"><button class="warn-btn">Destroy</button></form>
                </div>
              </td>
            </tr>
            {{ end }}
          </tbody>
        </table>
      </div>
      <div class="card">
        <h2>Shares</h2>
        <form method="post" action="/share" class="row">
          <input name="target" placeholder="https://myapp.dev-1.vpsbox.local" style="flex:1;" />
          <button type="submit">Create Share</button>
        </form>
        <table>
          <thead>
            <tr><th>Name</th><th>URL</th><th></th></tr>
          </thead>
          <tbody>
            {{ range .Shares }}
            <tr>
              <td>{{ .Name }}</td>
              <td><a href="{{ .URL }}">{{ .URL }}</a><br /><small>{{ .TargetURL }}</small></td>
              <td><form method="post" action="/unshare/{{ .Name }}"><button class="alt">Unshare</button></form></td>
            </tr>
            {{ end }}
          </tbody>
        </table>
      </div>
    </div>
  </div>
</body>
</html>`
