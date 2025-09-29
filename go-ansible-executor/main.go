package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"net/netip"
	"os/exec"

	"github.com/nats-io/nats.go"
)

const (
	subjectInstall       = "db.install"
	subjectInstallStatus = "db.install.status"
	defaultNatsURL       = "nats://127.0.0.1:4222"

	inventoryDir = "inventories"

	// Adjust if you want a different play timeout
	playTimeout = 30 * time.Minute

	// Limit published ansible output size
	maxOutputBytes = 10000
)

type InstallRequest struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	IPAddress  string `json:"ip_address"`
	VMUser     string `json:"vm_user"`
	VMPassword string `json:"vm_password"`
	DBType     string `json:"db_type"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`
	DBName     string `json:"db_name"`
}

type InstallStatus struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Status          string    `json:"status"` // "success" | "error"
	Inventory       string    `json:"inventory"`
	AnsibleExitCode int       `json:"ansible_exit_code"`
	AnsibleOutput   string    `json:"ansible_output,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
	Error           string    `json:"error,omitempty"`
}

func main() {
	natsURL := envOr("NATS_URL", defaultNatsURL)

	// Connect to NATS
	nc, err := nats.Connect(natsURL,
		nats.Name("db-install-worker"),
		nats.MaxReconnects(-1),
	)
	mustNoErr(err, "connect NATS")
	defer nc.Drain()

	log.Printf("[startup] connected to NATS at %s", natsURL)

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Queue group so multiple workers share the load (optional)
	sub, err := nc.QueueSubscribe(subjectInstall, "db-install-workers", func(msg *nats.Msg) {
		handleMessage(ctx, nc, msg)
	})
	mustNoErr(err, "subscribe to subject")
	defer sub.Unsubscribe()

	log.Printf("[ready] listening on subject %q; will publish status to %q", subjectInstall, subjectInstallStatus)

	<-ctx.Done()
	log.Println("[shutdown] stopping worker...")
}

// ------------ message handling ------------

func handleMessage(parent context.Context, nc *nats.Conn, msg *nats.Msg) {
	time.Sleep(3 * time.Second)
	var req InstallRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		log.Printf("[warn] invalid JSON: %v", err)
		publishStatus(nc, InstallStatus{
			ID:        0,
			Name:      "",
			Status:    "error",
			Error:     fmt.Sprintf("invalid JSON: %v", err),
			Timestamp: time.Now(),
		})
		return
	}

	// Basic validation
	if err := validateRequest(req); err != nil {
		log.Printf("[warn] invalid request (id=%d name=%q): %v", req.ID, req.Name, err)
		publishStatus(nc, InstallStatus{
			ID:        req.ID,
			Name:      req.Name,
			Status:    "error",
			Error:     err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// 1) Write an inventory file
	invPath, err := writeInventory(req)
	if err != nil {
		log.Printf("[error] write inventory failed (id=%d): %v", req.ID, err)
		publishStatus(nc, InstallStatus{
			ID:        req.ID,
			Name:      req.Name,
			Status:    "error",
			Error:     err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// ensure secrets don't linger on disk
	defer func(p string) {
		if p == "" {
			return
		}
		if rmErr := os.Remove(p); rmErr != nil {
			log.Printf("[warn] failed to remove inventory %s: %v", p, rmErr)
		} else {
			log.Printf("[ok] removed inventory %s", p)
		}
	}(invPath)

	// 2) Choose a playbook based on db_type
	playbookPath, err := selectPlaybook(req.DBType)
	if err != nil {
		publishStatus(nc, InstallStatus{
			ID: req.ID, Name: req.Name, Status: "error",
			Inventory: invPath, Error: err.Error(), Timestamp: time.Now(),
		})
		return
	}

	// 3) Run ansible playbook
	exitCode, output, runErr := runPlaybook(parent, invPath, playbookPath)

	// Prepare status
	status := "success"
	errMsg := ""
	if runErr != nil || exitCode != 0 {
		status = "error"
		if runErr != nil {
			errMsg = runErr.Error()
		}
	}

	publishStatus(nc, InstallStatus{
		ID:              req.ID,
		Name:            req.Name,
		Status:          status,
		Inventory:       invPath,
		AnsibleExitCode: exitCode,
		AnsibleOutput:   truncate(string(output), maxOutputBytes),
		Error:           errMsg,
		Timestamp:       time.Now(),
	})
}

// ------------ helpers ------------

func validateRequest(r InstallRequest) error {
	if r.ID == 0 {
		return errors.New("missing id")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("missing name")
	}
	if _, err := netip.ParseAddr(r.IPAddress); err != nil {
		return fmt.Errorf("invalid ip_address: %v", err)
	}
	if r.VMUser == "" || r.VMPassword == "" {
		return errors.New("missing vm_user or vm_password")
	}
	if r.DBName == "" || r.DBUser == "" || r.DBPassword == "" {
		return errors.New("missing db creds or db_name")
	}
	// Optional: enforce db_type == "postgresql"
	if !strings.EqualFold(r.DBType, "postgresql") {
		return fmt.Errorf("unsupported db_type %q (only 'postgresql' supported)", r.DBType)
	}
	return nil
}

func writeInventory(r InstallRequest) (string, error) {
	if err := os.MkdirAll(inventoryDir, 0o755); err != nil {
		return "", fmt.Errorf("create inventories dir: %w", err)
	}

	sanitized := sanitizeName(r.Name) // e.g., "db_postgresql_hiteman_prod"
	filename := fmt.Sprintf("vm_%d_%s.ini", r.ID, sanitized)
	path := filepath.Join(inventoryDir, filename)

	// Inventory entry (single host line)
	// Example:
	// 10.2.0.61 ansible_user=root ansible_password=P@ssw0rd123!! db_name=app_db db_user=appUser db_password=appPassword
	line := fmt.Sprintf("%s ansible_user=%s ansible_password=%s db_name=%s db_user=%s db_password=%s\n",
		r.IPAddress, r.VMUser, r.VMPassword, r.DBName, r.DBUser, r.DBPassword)

	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return path, fmt.Errorf("write inventory file: %w", err)
	}
	return path, nil
}

// sanitizeName converts "DB PostgreSQL HiTeman Prod" => "db_postgresql_hiteman_prod"
func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	// replace spaces and hyphens with underscores
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	// keep only a-z0-9_
	re := regexp.MustCompile(`[^a-z0-9_]+`)
	s = re.ReplaceAllString(s, "")
	// if it doesn't start with "db_", prepend it (to match your example)
	if !strings.HasPrefix(s, "db_") {
		s = "db_" + s
	}
	return s
}

func selectPlaybook(dbType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgresql", "postgres", "pg":
		return "playbooks/postgresql.yml", nil
	// Add other DBs here when ready:
	// case "mysql":
	//     return "playbooks/mysql.yml", nil
	// case "mariadb":
	//     return "playbooks/mariadb.yml", nil
	default:
		return "", fmt.Errorf("unsupported db_type %q", dbType)
	}
}

func runPlaybook(parent context.Context, inventoryPath, playbookPath string) (exitCode int, output []byte, err error) {
	if _, statErr := os.Stat(playbookPath); statErr != nil {
		return 127, nil, fmt.Errorf("playbook not found at %s: %w", playbookPath, statErr)
	}

	ctx, cancel := context.WithTimeout(parent, playTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ansible-playbook", "-i", inventoryPath, playbookPath)

	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, os.Stdout) // stream to journald + capture
	cmd.Stdout = mw
	cmd.Stderr = mw

	runErr := cmd.Run()

	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.Is(runErr, context.DeadlineExceeded) {
			return 124, buf.Bytes(), fmt.Errorf("ansible-playbook timed out after %s", playTimeout)
		}
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
		return code, buf.Bytes(), runErr
	}

	return 0, buf.Bytes(), nil
}

func publishStatus(nc *nats.Conn, st InstallStatus) {
	data, err := json.Marshal(st)
	if err != nil {
		log.Printf("[error] marshal status failed: %v", err)
		return
	}
	if err := nc.Publish(subjectInstallStatus, data); err != nil {
		log.Printf("[error] publish status failed: %v", err)
		return
	}
	log.Printf("[status] published: id=%d name=%q status=%s exit=%d", st.ID, st.Name, st.Status, st.AnsibleExitCode)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustNoErr(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]..."
}
