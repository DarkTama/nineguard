package providerengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	sdkapi "github.com/router-for-me/CLIProxyAPI/v8/sdk/api"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/sirupsen/logrus"
	"nineguard/internal/db"
)

// CLIProxyAPI has process-wide registries. Only one Engine may run in this process.
var runtimeLock sync.Mutex
var sdkLogFilterOnce sync.Once

type Engine struct {
	db                                     *db.DB
	store                                  *credentialStore
	manager                                *coreauth.Manager
	service                                *cliproxy.Service
	router                                 *gin.Engine
	address, secret, managementSecret, dir string
	cancel                                 context.CancelFunc
	done                                   chan error
	sessionMu                              sync.Mutex
	sessions                               map[string]*loginState
}

func New(d *db.DB, dataDir string) (*Engine, error) {
	if d == nil {
		return nil, errors.New("OAuth engine requires a database")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	return &Engine{db: d, store: newCredentialStore(d), sessions: map[string]*loginState{}}, nil
}
func (e *Engine) Start(ctx context.Context) error {
	if !runtimeLock.TryLock() {
		return errors.New("OAuth engine already running")
	}
	sdkLogFilterOnce.Do(func() { logrus.AddHook(privateLogFilter{}) })
	locked := true
	defer func() {
		if locked {
			runtimeLock.Unlock()
		}
	}()
	dir, err := os.MkdirTemp("", "nineguard-oauth-")
	if err != nil {
		return err
	}
	e.dir = dir
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(dir)
		}
	}()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	e.secret = randomID()
	e.managementSecret = randomID()
	e.address = fmt.Sprintf("http://127.0.0.1:%d", port)
	path := filepath.Join(dir, "config.yaml")
	if err = os.WriteFile(path, []byte("host: 127.0.0.1\n"), 0600); err != nil {
		return err
	}
	cfg := &config.Config{Host: "127.0.0.1", Port: port, AuthDir: filepath.Join(dir, "callbacks"), CommercialMode: true}
	cfg.APIKeys = []string{e.secret}
	cfg.RemoteManagement.SecretKey = e.managementSecret
	cfg.RemoteManagement.DisableControlPanel = true
	cfg.RemoteManagement.DisableAutoUpdatePanel = true
	cfg.ForceModelPrefix = true
	sdkauth.RegisterTokenStore(e.store)
	e.manager = coreauth.NewManager(e.store, nil, accountRuntimeHook{engine: e})
	e.service, err = cliproxy.NewBuilder().WithConfig(cfg).WithConfigPath(path).WithCoreAuthManager(e.manager).
		WithWatcherFactory(func(string, string, func(*config.Config)) (*cliproxy.WatcherWrapper, error) {
			return &cliproxy.WatcherWrapper{}, nil
		}).
		WithPostAuthHook(e.prepareLoginRecord).
		WithServerOptions(sdkapi.WithLocalManagementPassword(e.managementSecret), sdkapi.WithRouterConfigurator(func(r *gin.Engine, _ *handlers.BaseAPIHandler, _ *config.Config) { e.router = r })).Build()
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.done = make(chan error, 1)
	go func() { e.done <- e.service.Run(runCtx) }()
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err = <-e.done:
			cancel()
			if err == nil {
				err = errors.New("OAuth engine stopped during startup")
			}
			return err
		case <-ctx.Done():
			cancel()
			<-e.done
			return ctx.Err()
		case <-deadline.C:
			cancel()
			<-e.done
			return errors.New("OAuth engine readiness timeout")
		case <-tick.C:
			req, _ := http.NewRequestWithContext(ctx, "GET", e.address+"/v1/models", nil)
			req.Header.Set("Authorization", "Bearer "+e.secret)
			resp, requestErr := client.Do(req)
			if requestErr != nil {
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				continue
			}
			for _, a := range e.manager.List() {
				if err = e.publish(ctx, a); err != nil {
					cancel()
					<-e.done
					return err
				}
			}
			locked = false
			cleanup = false
			return nil
		}
	}
}
func (e *Engine) Close(ctx context.Context) error {
	if e.cancel == nil {
		return nil
	}
	e.sessionMu.Lock()
	for _, s := range e.sessions {
		e.cancelSession(s)
	}
	e.sessionMu.Unlock()
	e.cancel()
	select {
	case err := <-e.done:
		for _, a := range e.manager.List() {
			cliproxy.GlobalModelRegistry().UnregisterClient(a.ID)
		}
		e.cancel = nil
		os.RemoveAll(e.dir)
		runtimeLock.Unlock()
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (e *Engine) manage(ctx context.Context, method, path string, body any) ([]byte, error) {
	var input []byte
	var err error
	if body != nil {
		input, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1/v0/management/"+path, bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+e.managementSecret)
	req.Header.Set("Content-Type", "application/json")
	return e.manageRequest(req)
}
func (e *Engine) manageRequest(req *http.Request) ([]byte, error) {
	if e.router == nil {
		return nil, errors.New("OAuth engine is not ready")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		return nil, fmt.Errorf("OAuth engine operation failed (HTTP %d)", rec.Code)
	}
	return rec.Body.Bytes(), nil
}
func (e *Engine) publish(ctx context.Context, a *coreauth.Auth) error {
	_, err := e.manage(ctx, "PATCH", "auth-files/fields", map[string]any{"name": a.ID, "prefix": "ng-" + a.ID})
	return err
}
func (e *Engine) Models(ctx context.Context, providerID string) ([]map[string]interface{}, error) {
	accounts, err := e.store.accounts(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	seen := map[string]bool{}
	for _, account := range accounts {
		a, ok := e.manager.GetByID(account.ID)
		if !ok || a.Disabled {
			continue
		}
		for _, m := range cliproxy.GlobalModelRegistry().GetModelsForClient(a.ID) {
			id := m.ID
			prefix := a.Prefix + "/"
			if len(id) > len(prefix) && id[:len(prefix)] == prefix {
				id = id[len(prefix):]
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, map[string]interface{}{"id": id, "object": "model", "owned_by": account.Family})
			}
		}
	}
	return out, nil
}
func (e *Engine) Accounts(ctx context.Context, providerID string) ([]Account, error) {
	accounts, err := e.store.accounts(ctx, providerID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		a, ok := e.manager.GetByID(accounts[i].ID)
		if !ok {
			continue
		}
		for _, key := range []string{"expired", "expires_at"} {
			if v, ok := a.Metadata[key].(string); ok {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					accounts[i].ExpiresAt = &t
					break
				}
			}
		}
		if !a.LastRefreshedAt.IsZero() {
			t := a.LastRefreshedAt
			accounts[i].LastRefreshedAt = &t
		}
		if a.LastError != nil && (a.LastError.HTTPStatus == 401 || strings.Contains(strings.ToLower(a.LastError.Code+" "+a.LastError.Message), "invalid_grant")) {
			accounts[i].Status = "needs_login"
			accounts[i].LastError = "Provider authorization expired or was revoked. Login again."
		} else if a.Disabled {
			accounts[i].Status = "disabled"
		}
	}
	return accounts, nil
}
